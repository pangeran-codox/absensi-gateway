package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"absensi-gateway/internal/middleware"
)

type DeviceOpsHandler struct {
	DB *sql.DB
}

func NewDeviceOpsHandler(db *sql.DB) *DeviceOpsHandler {
	return &DeviceOpsHandler{DB: db}
}

// Heartbeat dipanggil device secara berkala supaya admin bisa tahu device
// mana yang masih hidup (via devices.last_seen_at), dan supaya device
// tahu jam server untuk sinkronisasi waktu di layar/kiosk-nya.
func (h *DeviceOpsHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	deviceID, _ := r.Context().Value(middleware.CtxDeviceID).(string)

	_, err := h.DB.ExecContext(r.Context(),
		`UPDATE devices SET last_seen_at = now() WHERE id = $1`, deviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Gagal update last_seen_at")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":      "ok",
		"server_time": time.Now().Format(time.RFC3339),
	})
}
