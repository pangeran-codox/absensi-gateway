// Package config membaca konfigurasi service dari environment variable.
// Semua nilainya sesuai yang sudah didefinisikan di docker-compose.yml.
package config

import (
	"errors"
	"os"
)

type Config struct {
	ListenAddr  string
	DatabaseURL string
	JWTSecret   string
}

// Load membaca env var wajib. Kalau ada yang kosong, service langsung
// gagal start (fail-fast) daripada jalan dengan config setengah-setengah
// yang errornya baru ketahuan pas ada request masuk.
func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:  getEnvOrDefault("LISTEN_ADDR", ":8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
	}

	if cfg.DatabaseURL == "" {
		return nil, errors.New("env DATABASE_URL wajib diisi")
	}
	if cfg.JWTSecret == "" {
		return nil, errors.New("env JWT_SECRET wajib diisi")
	}

	return cfg, nil
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
