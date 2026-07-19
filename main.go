package main

import (
	"log"
	"net/http"

	"absensi-gateway/internal/config"
	"absensi-gateway/internal/db"
	"absensi-gateway/internal/handlers"
	"absensi-gateway/internal/middleware"
)

// chain menyusun beberapa middleware menjadi satu, dieksekusi berurutan
// dari kiri ke kanan (chain(a, b)(handler) => a(b(handler))).
func chain(mws ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(final http.Handler) http.Handler {
		h := final
		for i := len(mws) - 1; i >= 0; i-- {
			h = mws[i](h)
		}
		return h
	}
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("konfigurasi tidak valid: %v", err)
	}

	dbConn, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("gagal konek database: %v", err)
	}
	defer dbConn.Close()

	deviceAuth := middleware.DeviceKeyAuth(dbConn)
	jwtAuth := middleware.JWTAuth(cfg.JWTSecret)
	adminOnly := middleware.RequireRole("admin")

	deviceHandler := handlers.NewDeviceHandler(dbConn)
	teacherHandler := handlers.NewTeacherHandler(dbConn)
	enrollmentHandler := handlers.NewEnrollmentHandler(dbConn)
	attendanceHandler := handlers.NewAttendanceHandler(dbConn)
	deviceOpsHandler := handlers.NewDeviceOpsHandler(dbConn)

	mux := http.NewServeMux()

	// --- Endpoint device tetap (RFID/QR/Face) ---
	mux.Handle("POST /api/v1/checkin/device",
		chain(deviceAuth)(http.HandlerFunc(deviceHandler.CheckinDevice)))

	mux.Handle("GET /api/v1/checkin/device/jobs/{job_id}",
		chain(deviceAuth)(http.HandlerFunc(deviceHandler.GetFaceJobResult)))

	mux.Handle("POST /api/v1/devices/heartbeat",
		chain(deviceAuth)(http.HandlerFunc(deviceOpsHandler.Heartbeat)))

	// --- Endpoint guru (web/PWA, JWT dari Eduzone) ---
	mux.Handle("POST /api/v1/checkin/teacher",
		chain(jwtAuth)(http.HandlerFunc(teacherHandler.CheckinTeacher)))

	mux.Handle("GET /api/v1/attendance/daily",
		chain(jwtAuth)(http.HandlerFunc(attendanceHandler.GetDaily)))

	// --- Endpoint admin ---
	mux.Handle("POST /api/v1/enrollment/credentials",
		chain(jwtAuth, adminOnly)(http.HandlerFunc(enrollmentHandler.EnrollCredential)))

	log.Printf("absensi-gateway listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		log.Fatalf("server berhenti: %v", err)
	}
}
