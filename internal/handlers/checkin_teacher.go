package handlers

import (
	"database/sql"
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
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if !validEventTypes[req.EventType] {
		writeError(w, http.StatusBadRequest, "invalid_event_type", "event_type harus check_in atau check_out")
		return
	}
	if !isValidCoordinate(req.Latitude, req.Longitude) {
		writeError(w, http.StatusBadRequest, "invalid_coordinates",
			"latitude harus -90..90 dan longitude harus -180..180")
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
	clientIP := middleware.ClientIP(r)
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

func joinReasons(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	return strings.Join(reasons, ",")
}

// isValidCoordinate mengecek latitude/longitude berada di rentang yang
// secara fisik mungkin ada di bumi. Ini validasi FORMAT, bukan validasi
// "GPS ini beneran akurat" — GPS palsu/spoofed yang angkanya tetap masuk
// rentang wajar tidak akan ketahuan di sini (itu di luar cakupan
// pengecekan sederhana ini).
func isValidCoordinate(lat, lng float64) bool {
	return lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}
