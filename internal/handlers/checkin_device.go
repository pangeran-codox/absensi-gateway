package handlers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"absensi-gateway/internal/middleware"
	"absensi-gateway/internal/scheduling"
)

type DeviceHandler struct {
	DB *sql.DB

	// jobStore adalah placeholder in-memory untuk job face-recognition
	// async. INI STUB SEMENTARA — di production harus diganti queue
	// sungguhan (mis. Redis Streams) yang dikonsumsi worker Python,
	// supaya job tidak hilang kalau service Go restart dan bisa
	// di-scale ke banyak instance.
	jobStore   map[string]FaceJobResult
	jobStoreMu sync.Mutex
}

func NewDeviceHandler(db *sql.DB) *DeviceHandler {
	return &DeviceHandler{DB: db, jobStore: make(map[string]FaceJobResult)}
}

type checkinDeviceRequest struct {
	Method          string `json:"method"`           // "rfid" | "qr" | "face"
	EventType       string `json:"event_type"`        // "check_in" | "check_out"
	CredentialValue string `json:"credential_value"`  // untuk rfid/qr
	ImageBase64     string `json:"image_base64"`      // untuk face
	ClientTimestamp string `json:"client_timestamp"`
}

type FaceJobResult struct {
	Status string `json:"status"` // "processing" | "done"
	Result string `json:"result,omitempty"`

	// deviceID & schoolID huruf kecil (unexported) supaya otomatis TIDAK
	// ikut ter-encode ke response JSON — dua field ini murni dipakai
	// internal untuk memastikan device yang polling job ini adalah device
	// yang sama yang membuatnya. Tanpa ini, device mana saja (asal device
	// key-nya valid) bisa menebak/mencoba job_id device lain dan mengintip
	// hasil pengenalan wajah orang yang bukan urusannya.
	deviceID string
	schoolID string
}

var validMethods = map[string]bool{"rfid": true, "qr": true, "face": true}
var validEventTypes = map[string]bool{"check_in": true, "check_out": true}

// duplicateScanWindow adalah jeda minimum antar-scan untuk kredensial
// yang sama sebelum dianggap anomali (mis. kartu ditap berkali-kali
// terlalu cepat, indikasi percobaan titip absen beruntun atau device error).
const duplicateScanWindow = 5 * time.Second

func (h *DeviceHandler) CheckinDevice(w http.ResponseWriter, r *http.Request) {
	var req checkinDeviceRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}

	if !validMethods[req.Method] {
		writeError(w, http.StatusBadRequest, "invalid_method", "method harus rfid, qr, atau face")
		return
	}
	if !validEventTypes[req.EventType] {
		writeError(w, http.StatusBadRequest, "invalid_event_type", "event_type harus check_in atau check_out")
		return
	}

	deviceID, _ := r.Context().Value(middleware.CtxDeviceID).(string)
	schoolID, _ := r.Context().Value(middleware.CtxSchoolID).(string)

	if req.Method == "face" {
		h.handleFaceCheckin(w, r, deviceID, schoolID, req)
		return
	}

	h.handleSyncCheckin(w, r, deviceID, schoolID, req)
}

// handleSyncCheckin menangani RFID & QR: pencocokan kredensial cepat
// (hash lookup), langsung insert event, langsung respons ke device.
func (h *DeviceHandler) handleSyncCheckin(w http.ResponseWriter, r *http.Request, deviceID, schoolID string, req checkinDeviceRequest) {
	if req.CredentialValue == "" {
		writeError(w, http.StatusBadRequest, "missing_credential", "credential_value wajib diisi")
		return
	}

	sum := sha256.Sum256([]byte(req.CredentialValue))
	credHash := hex.EncodeToString(sum[:])

	var personID, personType string
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT c.person_id, c.person_type
		FROM credentials c
		WHERE c.school_id = $1 AND c.method = $2 AND c.credential_hash = $3 AND c.is_active = true
	`, schoolID, req.Method, credHash).Scan(&personID, &personType)

	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "credential_not_found", "Kartu/token tidak terdaftar")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Gagal mencocokkan kredensial")
		return
	}

	// Semua langkah dari titik ini (cek anomali sampai insert event) harus
	// jadi satu paket yang tidak bisa diselak request lain untuk
	// person_id+method yang SAMA — makanya dibungkus transaksi + advisory
	// lock, bukan dua query terpisah seperti sebelumnya. Tanpa ini, dua
	// scan yang datang nyaris bersamaan bisa sama-sama "melihat" belum ada
	// event sebelumnya dan lolos berdua dari cek duplicate_scan_within_5s.
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Gagal memulai transaksi")
		return
	}
	defer tx.Rollback() // no-op kalau sudah di-Commit di bawah

	// pg_advisory_xact_lock mengunci berdasarkan angka, jadi person_id
	// (uuid/string) diubah dulu jadi angka pakai hashtext(). Lock ini
	// otomatis lepas saat transaksi commit/rollback (makanya versi "xact").
	if _, err := tx.ExecContext(r.Context(),
		`SELECT pg_advisory_xact_lock(hashtext($1 || $2))`, personID, req.Method); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Gagal mengambil lock")
		return
	}

	// Cek anomali: scan berulang terlalu cepat untuk orang yang sama.
	// Pakai tx (bukan h.DB) supaya baca data di dalam transaksi yang sama.
	anomalyReasons := h.detectAnomalies(r, tx, personID, personType, req.Method)

	// Auto-resolve jadwal aktif kalau device ini terpasang tetap di 1
	// ruang kelas/lab (default_class_id). Kosong (bukan error) kalau
	// device umum (gerbang) atau sedang bukan jam pelajaran.
	deviceClassID, _ := r.Context().Value(middleware.CtxDeviceClassID).(string)
	scheduleID, err := scheduling.ResolveActiveSchedule(h.DB, deviceClassID, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Gagal resolve jadwal aktif")
		return
	}

	var scheduleIDParam interface{}
	if scheduleID != "" {
		scheduleIDParam = scheduleID
	} else {
		scheduleIDParam = nil
	}

	var eventID int64
	var personName string
	err = tx.QueryRowContext(r.Context(), `
		INSERT INTO attendance_events
			(school_id, device_id, schedule_id, person_id, person_type, method, event_type, is_valid, flagged_reason, raw_payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`, schoolID, deviceID, scheduleIDParam, personID, personType, req.Method, req.EventType,
		len(anomalyReasons) == 0, joinReasons(anomalyReasons), mustJSON(req)).Scan(&eventID)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Gagal mencatat event absensi")
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Gagal menyimpan event absensi")
		return
	}

	// Ambil nama untuk response (dari cache people_ref).
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT full_name FROM people_ref WHERE person_id = $1 AND person_type = $2`,
		personID, personType).Scan(&personName)

	status := "accepted"
	if len(anomalyReasons) > 0 {
		status = "accepted_with_flag"
	}

	resp := map[string]interface{}{
		"status":   status,
		"event_id": eventID,
		"person": map[string]string{
			"id":   personID,
			"name": personName,
			"type": personType,
		},
	}
	if scheduleID != "" {
		resp["schedule_id"] = scheduleID
	}
	if len(anomalyReasons) > 0 {
		resp["anomaly_reasons"] = anomalyReasons
	}

	writeJSON(w, http.StatusOK, resp)
}

// queryRowContexter adalah kontrak minimal yang dipenuhi baik *sql.DB
// maupun *sql.Tx — supaya detectAnomalies bisa dipanggil dengan salah
// satunya (di sini dipanggil dengan tx, supaya baca datanya konsisten
// dalam transaksi yang sama dengan insert setelahnya).
type queryRowContexter interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// detectAnomalies mengecek pola mencurigakan sederhana. Ini fondasi
// awal — bisa dikembangkan lagi (mis. cek lokasi device vs jadwal
// siswa) tanpa mengubah kontrak API.
func (h *DeviceHandler) detectAnomalies(r *http.Request, q queryRowContexter, personID, personType, method string) []string {
	var lastRecordedAt time.Time
	err := q.QueryRowContext(r.Context(), `
		SELECT recorded_at FROM attendance_events
		WHERE person_id = $1 AND person_type = $2 AND method = $3
		ORDER BY recorded_at DESC LIMIT 1
	`, personID, personType, method).Scan(&lastRecordedAt)

	if err != nil {
		return nil // belum pernah ada event sebelumnya, wajar tidak ada anomali
	}

	if time.Since(lastRecordedAt) < duplicateScanWindow {
		return []string{"duplicate_scan_within_5s"}
	}
	return nil
}

// handleFaceCheckin adalah STUB. Alur sebenarnya: gambar dikirim ke
// queue (Redis Streams), worker Python melakukan liveness check +
// face matching, lalu menulis hasilnya ke tempat yang bisa dipoll
// lewat GET /checkin/device/jobs/{job_id}. Implementasi queue-nya
// belum dibuat — di sini job langsung "diselesaikan" secara dummy
// supaya kontrak API & endpoint polling bisa diuji lebih dulu.
func (h *DeviceHandler) handleFaceCheckin(w http.ResponseWriter, r *http.Request, deviceID, schoolID string, req checkinDeviceRequest) {
	if req.ImageBase64 == "" {
		writeError(w, http.StatusBadRequest, "missing_image", "image_base64 wajib diisi")
		return
	}

	jobID := generateJobID()

	h.jobStoreMu.Lock()
	h.jobStore[jobID] = FaceJobResult{Status: "processing", deviceID: deviceID, schoolID: schoolID}
	h.jobStoreMu.Unlock()

	// TODO: publish job ke queue nyata untuk dikonsumsi worker Python.
	// Placeholder ini sengaja TIDAK menyimpan gambar wajah ke database
	// atau memprosesnya — hanya menunjukkan bentuk kontrak API.

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "processing",
		"job_id": jobID,
	})
}

// GetFaceJobResult adalah endpoint polling GET /checkin/device/jobs/{job_id}.
func (h *DeviceHandler) GetFaceJobResult(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("job_id")
	requestingDeviceID, _ := r.Context().Value(middleware.CtxDeviceID).(string)

	h.jobStoreMu.Lock()
	result, ok := h.jobStore[jobID]
	h.jobStoreMu.Unlock()

	// Sengaja dipakai pesan & status code YANG SAMA ("job_not_found", 404)
	// baik untuk job yang memang tidak ada, MAUPUN job yang ada tapi
	// kepunyaan device lain. Kalau dibedakan (mis. 403 "bukan milik anda"),
	// itu jadi bocor informasi ke device yang coba menebak-nebak: dia jadi
	// tahu job_id itu valid, cuma bukan miliknya — cukup buat mempersempit
	// tebakan job_id device lain.
	if !ok || result.deviceID != requestingDeviceID {
		writeError(w, http.StatusNotFound, "job_not_found", "Job tidak ditemukan")
		return
	}

	writeJSON(w, http.StatusOK, result)
}
