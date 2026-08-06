// Package aggregation merangkum attendance_events (log mentah, insert-only)
// jadi attendance_daily (1 baris per orang per hari) secara berkala.
//
// PENTING: proses ini sepenuhnya berjalan DI DALAM database gateway ini
// sendiri — tidak memanggil Laravel atau layanan eksternal apapun. Jadi
// beda dari internal/sync (yang bergantung pada Laravel hidup), agregasi
// ini akan tetap jalan normal walau sinkronisasi data sedang bermasalah.
package aggregation

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// Aggregator menjalankan siklus agregasi berkala.
type Aggregator struct {
	db           *sql.DB
	interval     time.Duration
	lookbackDays int
}

// NewAggregator membuat Aggregator baru.
//
// lookbackDays menentukan berapa hari ke belakang yang DIHITUNG ULANG
// setiap siklus (bukan cuma "hari ini") — minimal 2 (hari ini + kemarin)
// supaya event yang terjadi mepet tengah malam tetap konsisten
// diagregasi ulang walau siklus sebelumnya sempat jalan "terlalu pagi".
// Query-nya SELALU menghitung ulang dari data attendance_events yang
// sebenarnya (bukan menambah incremental), jadi aman dijalankan berkali-
// kali untuk rentang tanggal yang sama (idempotent).
func NewAggregator(db *sql.DB, interval time.Duration, lookbackDays int) *Aggregator {
	return &Aggregator{db: db, interval: interval, lookbackDays: lookbackDays}
}

// Run menjalankan siklus agregasi pertama SEGERA (tidak menunggu interval
// pertama), lalu mengulang tiap `interval` sampai ctx dibatalkan. Dipanggil
// sebagai goroutine terpisah dari main.go — tidak memblokir HTTP server.
func (a *Aggregator) Run(ctx context.Context) {
	a.runOnce(ctx)

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.runOnce(ctx)
		}
	}
}

func (a *Aggregator) runOnce(ctx context.Context) {
	since := lookbackStartDate(time.Now(), a.lookbackDays)

	result, err := a.db.ExecContext(ctx, aggregationQuery, since.Format("2006-01-02"))
	if err != nil {
		// Sengaja cuma di-log, TIDAK menghentikan proses apapun — kegagalan
		// agregasi tidak boleh sampai mengganggu check-in yang sedang
		// berjalan. Siklus berikutnya otomatis mencoba lagi, dan karena
		// query ini idempotent, tidak ada data yang "kelewat" permanen
		// akibat 1-2 siklus gagal.
		log.Printf("agregasi absen: GAGAL — %v", err)
		return
	}

	rows, _ := result.RowsAffected()
	log.Printf("agregasi absen: sukses, %d baris attendance_daily diproses (data sejak %s)",
		rows, since.Format("2006-01-02"))
}

// lookbackStartDate menghitung tanggal paling awal yang diagregasi ulang
// di 1 siklus — hari ini dikurangi lookbackDays. Dipisah jadi fungsi murni
// (tanpa DB) supaya gampang di-unit-test.
func lookbackStartDate(now time.Time, lookbackDays int) time.Time {
	return now.AddDate(0, 0, -lookbackDays)
}

// aggregationQuery menghitung ULANG (bukan menambah) baris attendance_daily
// untuk semua (school_id, person_id, person_type, date) yang punya event
// sejak parameter $1 (tanggal, format "YYYY-MM-DD").
//
// Catatan desain penting:
//   - status HANYA membedakan 'Hadir' vs 'Terlambat' — status lain
//     ('Sakit', 'Izin', 'Alpa') SENGAJA tidak pernah ditentukan di sini,
//     karena itu keputusan administratif yang gateway ini tidak punya
//     datanya (surat izin, siapa yang seharusnya hadir). Itu keputusan
//     Laravel, nanti lewat proses sync balik (belum dibuat).
//   - 'Terlambat' HANYA ditandai kalau schools_ref.late_cutoff_time
//     TERISI (tidak NULL) DAN ada check-in valid yang jamnya lebih
//     lambat dari cutoff itu. Sekolah yang belum mengatur jam masuknya
//     di Laravel (late_cutoff_time NULL) tidak akan pernah dapat status
//     Terlambat — mencegah salah label tanpa data yang jelas.
//   - primary_method diambil dari check-in valid PALING AWAL hari itu.
//   - has_anomaly true kalau ADA SAJA event hari itu yang tidak valid
//     atau punya flagged_reason (mis. duplicate_scan_within_5s).
//   - updated_at diisi now() secara eksplisit di SELECT (bukan cuma di
//     klausa ON CONFLICT DO UPDATE) — INSERT butuh nilai untuk SEMUA
//     kolom di target list, termasuk untuk baris yang benar-benar baru
//     (belum pernah ada, jadi bukan lewat jalur UPDATE).
const aggregationQuery = `
INSERT INTO attendance_daily
	(school_id, person_id, person_type, date, first_check_in, last_check_out,
	 status, primary_method, total_events, has_anomaly, updated_at)
SELECT
	e.school_id,
	e.person_id,
	e.person_type,
	e.recorded_at::date AS date,
	MIN(e.recorded_at) FILTER (WHERE e.event_type = 'check_in' AND e.is_valid)::time AS first_check_in,
	MAX(e.recorded_at) FILTER (WHERE e.event_type = 'check_out' AND e.is_valid)::time AS last_check_out,
	CASE
		WHEN sr.late_cutoff_time IS NOT NULL
		     AND (MIN(e.recorded_at) FILTER (WHERE e.event_type = 'check_in' AND e.is_valid))::time > sr.late_cutoff_time
		THEN 'Terlambat'
		ELSE 'Hadir'
	END AS status,
	(ARRAY_AGG(e.method ORDER BY e.recorded_at) FILTER (WHERE e.event_type = 'check_in' AND e.is_valid))[1] AS primary_method,
	COUNT(*) AS total_events,
	BOOL_OR(NOT e.is_valid OR e.flagged_reason IS NOT NULL) AS has_anomaly,
	now() AS updated_at
FROM attendance_events e
JOIN schools_ref sr ON sr.school_id = e.school_id
WHERE e.person_id IS NOT NULL
  AND e.recorded_at >= $1::date
GROUP BY e.school_id, e.person_id, e.person_type, e.recorded_at::date, sr.late_cutoff_time
ON CONFLICT (school_id, person_id, person_type, date) DO UPDATE SET
	first_check_in = EXCLUDED.first_check_in,
	last_check_out = EXCLUDED.last_check_out,
	status         = EXCLUDED.status,
	primary_method = EXCLUDED.primary_method,
	total_events   = EXCLUDED.total_events,
	has_anomaly    = EXCLUDED.has_anomaly,
	updated_at     = now()
`