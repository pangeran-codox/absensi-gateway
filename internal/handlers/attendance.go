package handlers

import (
	"database/sql"
	"net/http"

	"absensi-gateway/internal/middleware"
)

type AttendanceHandler struct {
	DB *sql.DB
}

func NewAttendanceHandler(db *sql.DB) *AttendanceHandler {
	return &AttendanceHandler{DB: db}
}

// GetDaily membaca ringkasan absensi harian dari attendance_daily
// (tabel agregat — bukan attendance_events mentah).
//
// CATATAN: agregasi attendance_events -> attendance_daily belum dibuat di
// gateway ini (lihat README, direncanakan jadi worker/job terpisah lewat
// sync_log). Jadi endpoint ini baru akan mengembalikan data setelah proses
// agregasi itu berjalan; untuk sekarang kemungkinan besar hasilnya kosong.
func (h *AttendanceHandler) GetDaily(w http.ResponseWriter, r *http.Request) {
	personID := r.URL.Query().Get("person_id")
	date := r.URL.Query().Get("date")

	if personID == "" || date == "" {
		writeError(w, http.StatusBadRequest, "missing_params", "Query param person_id dan date wajib diisi")
		return
	}

	// Dibatasi ke school_id milik token JWT yang dipakai, supaya guru/admin
	// satu sekolah tidak bisa intip data absensi sekolah lain hanya dengan
	// menebak person_id.
	schoolID, _ := r.Context().Value(middleware.CtxSchoolID).(string)

	var (
		personType     string
		status         string
		firstCheckIn   sql.NullString
		lastCheckOut   sql.NullString
		primaryMethod  sql.NullString
		hasAnomaly     bool
	)

	err := h.DB.QueryRowContext(r.Context(), `
		SELECT person_type, status, first_check_in, last_check_out, primary_method, has_anomaly
		FROM attendance_daily
		WHERE school_id = $1 AND person_id = $2 AND date = $3
		LIMIT 1
	`, schoolID, personID, date).Scan(&personType, &status, &firstCheckIn, &lastCheckOut, &primaryMethod, &hasAnomaly)

	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found", "Belum ada data absensi untuk person_id dan tanggal ini")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Gagal mengambil data absensi")
		return
	}

	resp := map[string]interface{}{
		"person_id":    personID,
		"date":         date,
		"status":       status,
		"has_anomaly":  hasAnomaly,
	}
	if firstCheckIn.Valid {
		resp["first_check_in"] = firstCheckIn.String
	} else {
		resp["first_check_in"] = nil
	}
	if lastCheckOut.Valid {
		resp["last_check_out"] = lastCheckOut.String
	} else {
		resp["last_check_out"] = nil
	}
	if primaryMethod.Valid {
		resp["primary_method"] = primaryMethod.String
	}

	writeJSON(w, http.StatusOK, resp)
}
