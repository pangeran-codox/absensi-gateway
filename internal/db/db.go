// Package db membuka koneksi ke PostgreSQL dan memastikan koneksinya
// benar-benar hidup sebelum service mulai menerima request.
package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq" // driver postgres, didaftarkan lewat side-effect import
)

// Batas pool koneksi. Tanpa batas eksplisit, Go defaultnya TIDAK
// membatasi jumlah koneksi terbuka sama sekali (unlimited) — di bawah
// beban tinggi (banyak device check-in bersamaan), gateway bisa membuka
// koneksi ke Postgres tanpa kendali dan menghabiskan slot koneksi
// Postgres (default Postgres cuma mengizinkan ~100 koneksi bersamaan,
// dipakai bareng service lain juga kalau 1 Postgres dipakai beberapa
// aplikasi seperti di infra ini).
//
// Angka-angka ini PERKIRAAN AWAL yang wajar untuk skala pilot/testing,
// BUKAN hasil load testing — sesuaikan lagi setelah ada data trafik
// nyata (lihat docs/audit-kesiapan-production.md poin E1).
const (
	maxOpenConns    = 25
	maxIdleConns    = 5
	connMaxLifetime = 30 * time.Minute
)

// Connect membuka connection pool ke database dan melakukan Ping supaya
// error koneksi (host salah, password salah, dll) ketahuan saat startup —
// bukan baru muncul di request pertama dari device/guru.
func Connect(dsn string) (*sql.DB, error) {
	dbConn, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("gagal membuka koneksi: %w", err)
	}

	dbConn.SetMaxOpenConns(maxOpenConns)
	dbConn.SetMaxIdleConns(maxIdleConns)
	dbConn.SetConnMaxLifetime(connMaxLifetime)

	if err := dbConn.Ping(); err != nil {
		dbConn.Close()
		return nil, fmt.Errorf("gagal ping database: %w", err)
	}

	return dbConn, nil
}
