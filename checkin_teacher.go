package handlers

import (
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"absensi-gateway/internal/geofence"
	"absensi-gateway/internal/middleware"
)

type TeacherHandler struct {
	DB *sql.DB
}

func NewTeacherHandler(db *sql.DB) *TeacherHandler {
	return &TeacherHandler{DB: db}
}

type checkinTeacherRequest struct {
	EventType       string  `json:"event_type"`
	Latitude        float64 `json:"latitude"`
	Longitude       float64 `json:"longitude"`
	AccuracyMeters  float64 `json:"accuracy_meters"`
	ClientTimestamp string  `json:"client_timestamp"`
}

func (h *TeacherHandler) CheckinTeacher(w http.ResponseWriter, r *http.Request) {
	var req checkinTeacherRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Body request tidak valid")
		return
	}
	if !validEventTypes[req.EventType] {
		writeError(w, http.StatusBadRequest, "invalid_event_type", "event_type harus check_in atau check_out")
		return
	}

	userID, _ := r.Context().Value(middleware.CtxUserID).(string)
	schoolID, _ := r.Context().Value(middleware.CtxSchoolID).(string)

	// --- Validasi 1: GPS radius ---
	var schoolLat, schoolLng float64
	var radiusMeters int
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT latitude, longitude, geofence_radius_meters FROM schools_ref WHERE school_id = $1
	`, schoolID).Scan(&schoolLat, &schoolLng, &radiusMeters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Gagal memuat data sekolah")
		return
	}

	gpsInRadius := geofence.WithinRadius(req.Latitude, req.Longitude, schoolLat, schoolLng, float64(radiusMeters))

	// --- Validasi 2: jaringan sekolah, berdasarkan IP request (bukan dari body klien) ---
	clientIP := extractClientIP(r)
	networkRecognized, err := h.isRecognizedNetwork(r, schoolID, clientIP)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Gagal memvalidasi jaringan")
		return
	}

	var anomalyReasons []string
	if !gpsInRadius {
		anomalyReasons = append(anomalyReasons, "gps_out_of_radius")
	}
	if !networkRecognized {
		anomalyReasons = append(anomalyReasons, "ip_not_whitelisted")
	}

	isValid := len(anomalyReasons) == 0

	var eventID int64
	err = h.DB.QueryRowContext(r.Context(), `
		INSERT INTO attendance_events
			(school_id, person_id, person_type, method, event_type, is_valid, flagged_reason, raw_payload)
		VALUES ($1, $2, 'teacher', 'manual', $3, $4, $5, $6)
		RETURNING id
	`, schoolID, userID, req.EventType, isValid, joinReasons(anomalyReasons), mustJSON(req)).Scan(&eventID)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Gagal mencatat event absensi")
		return
	}

	status := "accepted"
	if !isValid {
		status = "accepted_with_flag"
	}

	resp := map[string]interface{}{
		"status":   status,
		"event_id": eventID,
		"validations": map[string]bool{
			"gps_in_radius":      gpsInRadius,
			"network_recognized": networkRecognized,
		},
	}
	if len(anomalyReasons) > 0 {
		resp["anomaly_reasons"] = anomalyReasons
	}

	writeJSON(w, http.StatusOK, resp)
}

// isRecognizedNetwork mencocokkan IP klien terhadap daftar school_networks.
// Untuk jaringan yang requires_local_verifier = true (mis. IndiHome CGNAT),
// pencocokan IP publik TIDAK diandalkan — jaringan itu akan selalu dianggap
// tidak cocok lewat jalur ini sampai lapisan Local Presence Verifier
// (presence_tickets) diaktifkan. Ini konsisten dengan keputusan bertahap:
// fokus dulu ke jaringan yang IP-nya reliable (mis. iForte statis).
func (h *TeacherHandler) isRecognizedNetwork(r *http.Request, schoolID, clientIP string) (bool, error) {
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT ip_or_hostname FROM school_networks
		WHERE school_id = $1 AND is_active = true AND requires_local_verifier = false
	`, schoolID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var ipOrHost string
		if err := rows.Scan(&ipOrHost); err != nil {
			return false, err
		}
		if ipOrHost == clientIP {
			return true, nil
		}
	}
	return false, rows.Err()
}

// extractClientIP mengambil IP asli klien. Karena service ini berjalan
// di belakang Nginx Proxy Manager, IP asli ada di header
// X-Forwarded-For (bukan RemoteAddr yang akan menunjukkan IP proxy).
// PENTING: header ini HANYA bisa dipercaya kalau reverse proxy di
// depan gateway sudah dikonfigurasi menimpa (bukan meneruskan mentah)
// nilai dari klien luar — kalau tidak, klien bisa memalsukan IP-nya
// sendiri lewat header ini.
func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func joinReasons(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	return strings.Join(reasons, ",")
}
