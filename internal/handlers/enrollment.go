package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"

	"absensi-gateway/internal/middleware"
)

type EnrollmentHandler struct {
	DB *sql.DB
}

func NewEnrollmentHandler(db *sql.DB) *EnrollmentHandler {
	return &EnrollmentHandler{DB: db}
}

type enrollCredentialRequest struct {
	PersonID        string `json:"person_id"`
	PersonType      string `json:"person_type"`
	Method          string `json:"method"`
	CredentialValue string `json:"credential_value"`  // wajib untuk rfid/qr
	ImagesBase64    []string `json:"images_base64"`    // wajib untuk face (belum diimplementasikan)
}

var validPersonTypes = map[string]bool{"student": true, "teacher": true, "staff": true}

// EnrollCredential mendaftarkan kredensial baru (RFID/QR) untuk seorang
// person. Hanya bisa diakses admin (dicek RequireRole("admin") di main.go).
func (h *EnrollmentHandler) EnrollCredential(w http.ResponseWriter, r *http.Request) {
	var req enrollCredentialRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}

	if !validPersonTypes[req.PersonType] {
		writeError(w, http.StatusBadRequest, "invalid_person_type", "person_type harus student, teacher, atau staff")
		return
	}
	if req.PersonID == "" {
		writeError(w, http.StatusBadRequest, "missing_person_id", "person_id wajib diisi")
		return
	}

	// Face enrollment butuh queue + worker Python (belum dibuat — lihat
	// catatan "Masih stub" di README). Ditolak eksplisit di sini supaya
	// admin tidak mengira kredensial face sudah tersimpan padahal belum.
	if req.Method == "face" {
		writeError(w, http.StatusNotImplemented, "not_implemented",
			"Enrollment face belum aktif — menunggu worker Python (InsightFace)")
		return
	}

	if !validMethods[req.Method] {
		writeError(w, http.StatusBadRequest, "invalid_method", "method harus rfid atau qr (face belum aktif)")
		return
	}
	if req.CredentialValue == "" {
		writeError(w, http.StatusBadRequest, "missing_credential", "credential_value wajib diisi")
		return
	}

	schoolID, _ := r.Context().Value(middleware.CtxSchoolID).(string)

	sum := sha256.Sum256([]byte(req.CredentialValue))
	credHash := hex.EncodeToString(sum[:])

	var credentialID string
	err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO credentials (school_id, person_id, person_type, method, credential_hash)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, schoolID, req.PersonID, req.PersonType, req.Method, credHash).Scan(&credentialID)

	if err != nil {
		// Kemungkinan besar penyebabnya constraint UNIQUE (school_id, method,
		// credential_hash) di skema — kartu/token itu sudah didaftarkan
		// sebelumnya (untuk orang ini atau orang lain di sekolah yang sama).
		writeError(w, http.StatusConflict, "credential_conflict",
			"Kredensial ini sudah terdaftar, atau person_id/person_type tidak ditemukan di people_ref")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"status":        "enrolled",
		"credential_id": credentialID,
	})
}
