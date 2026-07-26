package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
)

// writeJSON menulis body JSON apa saja dengan status code tertentu.
// Dipakai untuk semua respons SUKSES (200/201/202).
func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Response header/status sudah terlanjur dikirim di titik ini,
		// jadi kita cuma bisa log — tidak bisa lagi ganti status code.
		log.Printf("writeJSON: gagal encode response: %v", err)
	}
}

// writeError adalah bentuk khusus writeJSON untuk respons GAGAL, format-nya
// konsisten dengan writeJSONError di internal/middleware/auth.go:
//
//	{ "status": "rejected", "reason": "...", "message": "..." }
//
// "reason" dipakai frontend/device untuk logic (switch-case per kode),
// "message" untuk ditampilkan ke manusia (guru/admin).
func writeError(w http.ResponseWriter, status int, reason, message string) {
	writeJSON(w, status, map[string]string{
		"status":  "rejected",
		"reason":  reason,
		"message": message,
	})
}

// decodeJSONBody men-decode body JSON ke dst. Kalau gagal, ia SUDAH menulis
// response error yang sesuai dan mengembalikan false — jadi pemanggil cukup
// return kalau hasilnya false.
//
// Ini pengganti json.NewDecoder(r.Body).Decode(&req) yang dipakai langsung
// di setiap handler sebelumnya. Bedanya: fungsi ini membedakan 2 jenis
// kegagalan yang perilakunya harus beda ke klien —
//   - body kelewat besar (melanggar batas dari middleware.MaxBodySize di
//     main.go) → 413 Request Entity Too Large, pesannya jelas soal ukuran
//   - body memang rusak/format salah → 400 Bad Request seperti biasa
//
// Tanpa pembeda ini, body raksasa akan kena "invalid_body" generik yang
// bikin klien (atau kita pas debug) salah kira itu masalah format data.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large",
				fmt.Sprintf("Body request melebihi batas maksimum %d bytes", maxBytesErr.Limit))
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_body", "Body request tidak valid")
		return false
	}
	return true
}

// mustJSON mengubah struct request jadi JSON string untuk disimpan ke
// kolom raw_payload (jsonb) — tujuannya audit/debug, supaya kalau ada
// event mencurigakan, kita masih punya body asli yang dikirim device/guru.
//
// "must" di nama fungsi ini berarti sengaja tidak mengembalikan error:
// checkinDeviceRequest & checkinTeacherRequest cuma berisi tipe data dasar
// (string/float64), yang SELALU valid di-marshal ke JSON. Kalau suatu saat
// field baru ditambahkan yang bisa gagal di-marshal (misal channel, func),
// ini akan panic — itu sengaja, supaya bug ketahuan pas testing, bukan
// diam-diam menyimpan raw_payload kosong ke production.
func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("mustJSON: gagal marshal, cek tipe data pada struct request: " + err.Error())
	}
	return b
}

// generateJobID membuat ID unik untuk job face-recognition async,
// format "job_<16 hex char>" mengikuti contoh di api_contract.md.
func generateJobID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand gagal baca praktis tidak pernah terjadi di Linux;
		// kalau sampai terjadi, lebih baik panic daripada job_id kosong/dobel.
		panic("generateJobID: gagal generate random bytes: " + err.Error())
	}
	return "job_" + hex.EncodeToString(buf)
}
