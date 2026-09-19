package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // embed database zoneinfo IANA ke dalam binary — lihat komentar di bawah

	"absensi-gateway/internal/aggregation"
	"absensi-gateway/internal/config"
	"absensi-gateway/internal/db"
	"absensi-gateway/internal/handlers"
	"absensi-gateway/internal/media"
	"absensi-gateway/internal/middleware"
	"absensi-gateway/internal/sync"
)

// Import "time/tzdata" (side-effect only, tidak dipakai langsung) membuat
// binary ini membawa SENDIRI database zoneinfo IANA (termasuk
// "Asia/Jakarta"), tidak bergantung pada file /usr/share/zoneinfo di image
// Docker tempat ia berjalan. Tanpa ini, kalau base image runtime suatu
// saat diganti ke image yang lebih minimal (mis. scratch/distroless tanpa
// paket tzdata), env var TZ=Asia/Jakarta akan DIAM-DIAM gagal di-resolve
// dan scheduling.ResolveActiveSchedule balik memakai UTC — bug yang susah
// ketahuan karena tidak ada error, cuma jadwal jadi salah beberapa jam.

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

	// rootCtx dibatalkan otomatis begitu proses menerima sinyal SIGTERM
	// (dikirim Docker saat `docker stop`/redeploy) atau SIGINT (Ctrl+C).
	// Dipakai sebagai context induk untuk HTTP server maupun goroutine
	// background (sync, agregasi) — supaya semuanya berhenti dengan rapi
	// bersamaan, bukan sebagian masih jalan sementara koneksi database
	// sudah ditutup duluan.
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	// Cache foto profil (lihat internal/media dan
	// spesifikasi-proxy-foto-absensi-gateway.md dari tim Laravel) —
	// gagal menyiapkan folder cache ini TIDAK menghentikan gateway,
	// cuma bikin endpoint foto selalu fallback ke avatar inisial
	// (masih lebih baik daripada gateway sama sekali tidak bisa start
	// gara-gara masalah folder cache yang sifatnya non-esensial).
	photoCache, err := media.NewDiskCache(cfg.PhotoCacheDir)
	if err != nil {
		log.Printf("PERINGATAN: gagal menyiapkan cache foto (%v) — endpoint foto akan selalu fallback ke avatar inisial", err)
	}
	mediaHandler := handlers.NewMediaHandler(dbConn, photoCache)

	// Sinkronisasi data schools_ref/people_ref/schedules_ref dari Laravel,
	// jalan di goroutine terpisah — TIDAK memblokir HTTP server, dan
	// kegagalannya (Laravel down, dll) tidak pernah membuat check-in
	// berhenti berfungsi. Lihat internal/sync dan
	// docs/laravel-sync-contract.md untuk kontrak endpoint yang harus
	// disediakan Laravel.
	if cfg.SyncEnabled {
		client := sync.NewLaravelClient(cfg.LaravelSyncURL, cfg.LaravelSyncToken)
		puller := sync.NewPuller(dbConn, client, cfg.SyncInterval)
		go puller.Run(rootCtx)
		log.Printf("sinkronisasi data AKTIF, interval %s, sumber %s", cfg.SyncInterval, cfg.LaravelSyncURL)
	} else {
		log.Print("sinkronisasi data NONAKTIF (SYNC_ENABLED bukan \"true\") — people_ref/schools_ref/schedules_ref harus diisi manual")
	}

	// Agregasi attendance_events -> attendance_daily, juga di goroutine
	// terpisah. Ini murni internal (tidak bergantung Laravel), jadi tetap
	// jalan normal walau sinkronisasi di atas sedang bermasalah.
	if cfg.AggregationEnabled {
		aggregator := aggregation.NewAggregator(dbConn, cfg.AggregationInterval, cfg.AggregationLookbackDays)
		go aggregator.Run(rootCtx)
		log.Printf("agregasi absen harian AKTIF, interval %s, lookback %d hari", cfg.AggregationInterval, cfg.AggregationLookbackDays)
	} else {
		log.Print("agregasi absen harian NONAKTIF (AGGREGATION_ENABLED=false) — attendance_daily tidak akan terisi otomatis")
	}

	mux := http.NewServeMux()

	// Semua route dipasangi middleware.MaxBodySize PALING LUAR (dieksekusi
	// paling awal, sebelum auth/handler apapun sempat baca body). Ini
	// pengaman terhadap body request raksasa yang bisa bikin server
	// kehabisan memori (DoS) — lihat komentar di internal/middleware/bodylimit.go.
	// Ukurannya beda-beda tergantung payload wajar tiap endpoint.

	// --- Endpoint device tetap (RFID/QR/Face) ---
	// SizeImage dipakai karena endpoint ini juga menerima foto (face check-in).
	mux.Handle("POST /api/v1/checkin/device",
		chain(middleware.MaxBodySize(middleware.SizeImage), deviceAuth)(http.HandlerFunc(deviceHandler.CheckinDevice)))

	mux.Handle("GET /api/v1/checkin/device/jobs/{job_id}",
		chain(middleware.MaxBodySize(middleware.SizeSmall), deviceAuth)(http.HandlerFunc(deviceHandler.GetFaceJobResult)))

	mux.Handle("POST /api/v1/devices/heartbeat",
		chain(middleware.MaxBodySize(middleware.SizeSmall), deviceAuth)(http.HandlerFunc(deviceOpsHandler.Heartbeat)))

	// --- Endpoint guru (web/PWA, JWT dari Eduzone) ---
	mux.Handle("POST /api/v1/checkin/teacher",
		chain(middleware.MaxBodySize(middleware.SizeSmall), jwtAuth)(http.HandlerFunc(teacherHandler.CheckinTeacher)))

	mux.Handle("GET /api/v1/attendance/daily",
		chain(middleware.MaxBodySize(middleware.SizeSmall), jwtAuth)(http.HandlerFunc(attendanceHandler.GetDaily)))

	// --- Endpoint admin ---
	// SizeEnrollment dipakai karena enrollment face bisa membawa beberapa foto sekaligus.
	mux.Handle("POST /api/v1/enrollment/credentials",
		chain(middleware.MaxBodySize(middleware.SizeEnrollment), jwtAuth, adminOnly)(http.HandlerFunc(enrollmentHandler.EnrollCredential)))

	// --- Foto profil (proxy + cache dari Laravel) ---
	// SENGAJA TANPA middleware auth apapun — lihat komentar lengkap di
	// internal/handlers/media.go soal kenapa (keterbatasan <img src>,
	// keputusan sadar mengandalkan UUID + NPM).
	mux.Handle("GET /api/v1/media/photo/{person_id}",
		chain(middleware.MaxBodySize(middleware.SizeSmall))(http.HandlerFunc(mediaHandler.GetPhoto)))

	// http.Server custom (bukan http.ListenAndServe langsung) supaya bisa
	// diset timeout eksplisit dan dimatikan dengan rapi (graceful shutdown).
	//
	// ReadTimeout/WriteTimeout/IdleTimeout mencegah 1 koneksi yang lambat
	// atau macet (disengaja maupun tidak — mis. device dengan jaringan
	// buruk, atau serangan "slowloris") menahan resource server tanpa
	// batas waktu. Tanpa ini, nilai defaultnya adalah TIDAK ADA timeout
	// sama sekali.
	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second, // sedikit lebih longgar dari ReadTimeout, karena beberapa handler perlu waktu query database
		IdleTimeout:  60 * time.Second,
	}

	// Server dijalankan di goroutine terpisah supaya main() bisa lanjut
	// menunggu sinyal shutdown (rootCtx.Done()) di bawah.
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("absensi-gateway listening on %s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		log.Fatalf("server berhenti tidak wajar: %v", err)
	case <-rootCtx.Done():
		// Sinyal SIGTERM/SIGINT diterima (mis. `docker stop` atau redeploy).
		log.Print("menerima sinyal shutdown, menyelesaikan request yang sedang berjalan...")

		// Request yang SUDAH masuk diberi waktu 15 detik untuk selesai
		// secara normal sebelum dipaksa berhenti — bukan langsung diputus
		// paksa di tengah jalan (mis. saat device sedang tap kartu).
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown paksa setelah timeout: %v", err)
		} else {
			log.Print("server berhenti dengan rapi")
		}
	}
}
