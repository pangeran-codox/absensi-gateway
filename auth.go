package middleware

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	CtxDeviceID      contextKey = "device_id"
	CtxSchoolID      contextKey = "school_id"
	CtxDeviceClassID contextKey = "device_class_id"
	CtxUserID        contextKey = "user_id"
	CtxUserRole      contextKey = "user_role"
)

// TeacherClaims merepresentasikan klaim JWT yang diterbitkan Eduzone
// (Laravel) saat guru login. Struktur ini HARUS sinkron dengan payload
// yang dibuat di sisi Laravel.
type TeacherClaims struct {
	UserID   string `json:"user_id"`
	SchoolID string `json:"school_id"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func writeJSONError(w http.ResponseWriter, status int, reason, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "rejected",
		"reason":  reason,
		"message": message,
	})
}

// DeviceKeyAuth memverifikasi header X-Device-Key terhadap kolom
// devices.api_key_hash (SHA-256). Device yang tidak aktif (is_active =
// false) juga ditolak, supaya device yang dicuri/dinonaktifkan admin
// langsung berhenti berfungsi tanpa perlu ganti key device lain.
func DeviceKeyAuth(dbConn *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawKey := r.Header.Get("X-Device-Key")
			if rawKey == "" {
				writeJSONError(w, http.StatusUnauthorized, "missing_device_key", "Header X-Device-Key wajib diisi")
				return
			}

			sum := sha256.Sum256([]byte(rawKey))
			keyHash := hex.EncodeToString(sum[:])

			var deviceID, schoolID string
			var defaultClassID sql.NullString
			err := dbConn.QueryRowContext(r.Context(),
				`SELECT id, school_id, default_class_id FROM devices WHERE api_key_hash = $1 AND is_active = true`,
				keyHash,
			).Scan(&deviceID, &schoolID, &defaultClassID)

			if err == sql.ErrNoRows {
				writeJSONError(w, http.StatusUnauthorized, "invalid_device_key", "Device key tidak valid atau device nonaktif")
				return
			}
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "internal_error", "Gagal verifikasi device")
				return
			}

			ctx := context.WithValue(r.Context(), CtxDeviceID, deviceID)
			ctx = context.WithValue(ctx, CtxSchoolID, schoolID)
			if defaultClassID.Valid {
				ctx = context.WithValue(ctx, CtxDeviceClassID, defaultClassID.String)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// JWTAuth memverifikasi token Bearer JWT yang diterbitkan Eduzone.
// Verifikasi dilakukan lokal (stateless) pakai shared secret HMAC,
// TIDAK memanggil balik ke Laravel — supaya cepat & tidak bergantung
// ketersediaan Eduzone utama saat guru check-in.
func JWTAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if len(authHeader) <= len(prefix) || authHeader[:len(prefix)] != prefix {
				writeJSONError(w, http.StatusUnauthorized, "missing_token", "Header Authorization Bearer wajib diisi")
				return
			}
			rawToken := authHeader[len(prefix):]

			claims := &TeacherClaims{}
			token, err := jwt.ParseWithClaims(rawToken, claims, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(secret), nil
			})

			if err != nil || !token.Valid {
				writeJSONError(w, http.StatusUnauthorized, "invalid_token", "Token tidak valid atau kedaluwarsa")
				return
			}

			ctx := context.WithValue(r.Context(), CtxUserID, claims.UserID)
			ctx = context.WithValue(ctx, CtxSchoolID, claims.SchoolID)
			ctx = context.WithValue(ctx, CtxUserRole, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole membatasi endpoint hanya untuk role tertentu (dipakai
// setelah JWTAuth), misal endpoint enrollment kredensial hanya untuk admin.
func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userRole, _ := r.Context().Value(CtxUserRole).(string)
			if userRole != role {
				writeJSONError(w, http.StatusForbidden, "forbidden", "Role tidak memiliki akses ke endpoint ini")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
