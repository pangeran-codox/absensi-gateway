package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

// HealthHandler mengecek apakah gateway hidup dan bisa menjangkau
// Postgres-nya sendiri. Dipakai untuk health check dari luar (mis. Laravel
// saat testing, atau monitoring lain) — TIDAK memeriksa konektivitas ke
// Laravel, karena gateway memang didesain tetap hidup walau sync ke
// Laravel sedang bermasalah (lihat catatan di internal/aggregation).
type HealthHandler struct {
	db *sql.DB
}

func NewHealthHandler(db *sql.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

type healthResponse struct {
	Status    string `json:"status"`
	Database  string `json:"database"`
	Timestamp string `json:"timestamp"`
}

// Handle membalas 200 kalau Postgres bisa dijangkau, 503 kalau tidak.
// Timeout ping sengaja pendek (3 detik) — health check harus cepat
// menjawab, bukan ikut nunggu lama kalau DB memang lagi bermasalah.
func (h *HealthHandler) Handle(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	resp := healthResponse{
		Status:    "ok",
		Database:  "ok",
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if err := h.db.PingContext(ctx); err != nil {
		resp.Status = "degraded"
		resp.Database = "unreachable"
		writeJSON(w, http.StatusServiceUnavailable, resp)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}