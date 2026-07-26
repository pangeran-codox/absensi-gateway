// Package db membuka koneksi ke PostgreSQL dan memastikan koneksinya
// benar-benar hidup sebelum service mulai menerima request.
package db

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq" // driver postgres, didaftarkan lewat side-effect import
)

// Connect membuka connection pool ke database dan melakukan Ping supaya
// error koneksi (host salah, password salah, dll) ketahuan saat startup —
// bukan baru muncul di request pertama dari device/guru.
func Connect(dsn string) (*sql.DB, error) {
	dbConn, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("gagal membuka koneksi: %w", err)
	}

	if err := dbConn.Ping(); err != nil {
		dbConn.Close()
		return nil, fmt.Errorf("gagal ping database: %w", err)
	}

	return dbConn, nil
}
