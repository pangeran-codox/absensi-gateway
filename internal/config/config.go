// Package config membaca konfigurasi service dari environment variable.
// Semua nilainya sesuai yang sudah didefinisikan di docker-compose.yml.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"
)

type Config struct {
	ListenAddr  string
	DatabaseURL string
	JWTSecret   string

	// Pengaturan sinkronisasi data dari Laravel (schools_ref/people_ref/
	// schedules_ref) — lihat internal/sync. NONAKTIF secara default supaya
	// deployment yang sudah ada (belum punya endpoint Laravel-nya) tidak
	// tiba-tiba error saat start hanya karena field ini kosong.
	SyncEnabled      bool
	LaravelSyncURL   string
	LaravelSyncToken string
	SyncInterval     time.Duration
}

// Load membaca env var wajib. Kalau ada yang kosong, service langsung
// gagal start (fail-fast) daripada jalan dengan config setengah-setengah
// yang errornya baru ketahuan pas ada request masuk.
func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:  getEnvOrDefault("LISTEN_ADDR", ":8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		JWTSecret:   os.Getenv("JWT_SECRET"),

		SyncEnabled:      getEnvOrDefault("SYNC_ENABLED", "false") == "true",
		LaravelSyncURL:   os.Getenv("LARAVEL_SYNC_URL"),
		LaravelSyncToken: os.Getenv("LARAVEL_SYNC_TOKEN"),
	}

	if cfg.DatabaseURL == "" {
		return nil, errors.New("env DATABASE_URL wajib diisi")
	}
	if cfg.JWTSecret == "" {
		return nil, errors.New("env JWT_SECRET wajib diisi")
	}

	if cfg.SyncEnabled {
		// Field ini HANYA wajib kalau sinkronisasi diaktifkan — deployment
		// yang belum siap sisi Laravel-nya tetap bisa jalan normal dengan
		// SYNC_ENABLED=false (atau tidak di-set sama sekali).
		if cfg.LaravelSyncURL == "" {
			return nil, errors.New("env LARAVEL_SYNC_URL wajib diisi kalau SYNC_ENABLED=true")
		}
		if cfg.LaravelSyncToken == "" {
			return nil, errors.New("env LARAVEL_SYNC_TOKEN wajib diisi kalau SYNC_ENABLED=true")
		}

		intervalStr := getEnvOrDefault("SYNC_INTERVAL", "5m")
		interval, err := time.ParseDuration(intervalStr)
		if err != nil {
			return nil, fmt.Errorf("env SYNC_INTERVAL tidak valid (%q): %w", intervalStr, err)
		}
		if interval < time.Minute {
			// Batas bawah longgar — mencegah salah ketik (mis. "5s" alih-alih
			// "5m") yang bisa membanjiri API Laravel dengan request tiap
			// beberapa detik.
			return nil, fmt.Errorf("env SYNC_INTERVAL terlalu kecil (%s) — minimum 1 menit", interval)
		}
		cfg.SyncInterval = interval
	}

	return cfg, nil
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
