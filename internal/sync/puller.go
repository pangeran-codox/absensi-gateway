package sync

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// Puller menjalankan siklus jemput-data dari Laravel secara berkala.
// SATU prinsip penting: kegagalan sync (Laravel down, network error, dll)
// TIDAK PERNAH membuat gateway utama berhenti atau menolak check-in —
// Puller cuma mencatat error & mencoba lagi di siklus berikutnya. Modul
// absensi harus tetap jalan normal walau sinkronisasi data sedang
// bermasalah.
type Puller struct {
	db       *sql.DB
	client   *LaravelClient
	interval time.Duration
}

func NewPuller(db *sql.DB, client *LaravelClient, interval time.Duration) *Puller {
	return &Puller{db: db, client: client, interval: interval}
}

// Run menjalankan siklus sync pertama SEGERA (tidak menunggu interval
// pertama), lalu mengulang tiap `interval` sampai ctx dibatalkan. Dipanggil
// sebagai goroutine terpisah dari main.go — tidak memblokir HTTP server.
func (p *Puller) Run(ctx context.Context) {
	p.pullAll(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pullAll(ctx)
		}
	}
}

// pullAll menjalankan 1 siklus penuh: schools dulu, baru people &
// schedules. Urutan ini WAJIB — lihat catatan foreign key di upsert.go.
func (p *Puller) pullAll(ctx context.Context) {
	p.pullSchools(ctx)
	p.pullPeople(ctx)
	p.pullSchedules(ctx)
}

func (p *Puller) pullSchools(ctx context.Context) {
	since, err := p.getWatermark(ctx, "schools")
	if err != nil {
		log.Printf("sync schools: gagal baca watermark: %v", err)
		return
	}

	records, err := p.client.FetchSchools(ctx, since)
	if err != nil {
		p.recordFailure(ctx, "schools", err)
		return
	}

	if err := UpsertSchools(ctx, p.db, records); err != nil {
		p.recordFailure(ctx, "schools", err)
		return
	}

	newWatermark := maxUpdatedAt(since, schoolTimes(records))
	p.recordSuccess(ctx, "schools", newWatermark, len(records))
}

func (p *Puller) pullPeople(ctx context.Context) {
	since, err := p.getWatermark(ctx, "people")
	if err != nil {
		log.Printf("sync people: gagal baca watermark: %v", err)
		return
	}

	records, err := p.client.FetchPeople(ctx, since)
	if err != nil {
		p.recordFailure(ctx, "people", err)
		return
	}

	if err := UpsertPeople(ctx, p.db, records); err != nil {
		p.recordFailure(ctx, "people", err)
		return
	}

	newWatermark := maxUpdatedAt(since, personTimes(records))
	p.recordSuccess(ctx, "people", newWatermark, len(records))
}

func (p *Puller) pullSchedules(ctx context.Context) {
	since, err := p.getWatermark(ctx, "schedules")
	if err != nil {
		log.Printf("sync schedules: gagal baca watermark: %v", err)
		return
	}

	records, err := p.client.FetchSchedules(ctx, since)
	if err != nil {
		p.recordFailure(ctx, "schedules", err)
		return
	}

	if err := UpsertSchedules(ctx, p.db, records); err != nil {
		p.recordFailure(ctx, "schedules", err)
		return
	}

	newWatermark := maxUpdatedAt(since, scheduleTimes(records))
	p.recordSuccess(ctx, "schedules", newWatermark, len(records))
}

// getWatermark membaca last_synced_at dari ref_sync_state untuk resource
// tertentu. nil berarti belum pernah sukses sync — Fetch<Resource> akan
// menjemput SEMUA data (bukan cuma yang berubah).
func (p *Puller) getWatermark(ctx context.Context, resource string) (*time.Time, error) {
	var t sql.NullTime
	err := p.db.QueryRowContext(ctx,
		`SELECT last_synced_at FROM ref_sync_state WHERE resource = $1`, resource,
	).Scan(&t)
	if err != nil {
		return nil, err
	}
	if !t.Valid {
		return nil, nil
	}
	return &t.Time, nil
}

func (p *Puller) recordSuccess(ctx context.Context, resource string, watermark *time.Time, count int) {
	_, err := p.db.ExecContext(ctx, `
		UPDATE ref_sync_state
		SET last_synced_at = COALESCE($2, last_synced_at),
		    last_status = 'success',
		    last_error = NULL,
		    last_record_count = $3,
		    updated_at = now()
		WHERE resource = $1
	`, resource, watermark, count)
	if err != nil {
		// Sengaja cuma di-log, bukan bikin proses berhenti — kegagalan
		// MENCATAT status sync tidak boleh sampai mengganggu fungsi utama
		// gateway. Efek sampingnya: siklus berikutnya mungkin menjemput
		// data yang sama lagi (tidak berbahaya, cuma kerja dobel).
		log.Printf("sync %s: gagal mencatat status sukses: %v", resource, err)
		return
	}
	log.Printf("sync %s: sukses, %d record diproses", resource, count)
}

func (p *Puller) recordFailure(ctx context.Context, resource string, cause error) {
	log.Printf("sync %s: GAGAL — %v", resource, cause)
	_, err := p.db.ExecContext(ctx, `
		UPDATE ref_sync_state
		SET last_status = 'failed', last_error = $2, updated_at = now()
		WHERE resource = $1
	`, resource, cause.Error())
	if err != nil {
		log.Printf("sync %s: gagal mencatat status gagal: %v", resource, err)
	}
}

// maxUpdatedAt mengembalikan waktu TERBARU di antara watermark saat ini
// (`current`, boleh nil) dan seluruh `times` — dipakai untuk menghitung
// watermark BARU setelah 1 siklus sync sukses. Dipisah jadi fungsi murni
// (tanpa DB/HTTP) supaya gampang di-unit-test.
func maxUpdatedAt(current *time.Time, times []time.Time) *time.Time {
	result := current
	for _, t := range times {
		if result == nil || t.After(*result) {
			tCopy := t
			result = &tCopy
		}
	}
	return result
}

func schoolTimes(records []SchoolRecord) []time.Time {
	out := make([]time.Time, len(records))
	for i, r := range records {
		out[i] = r.UpdatedAt
	}
	return out
}

func personTimes(records []PersonRecord) []time.Time {
	out := make([]time.Time, len(records))
	for i, r := range records {
		out[i] = r.UpdatedAt
	}
	return out
}

func scheduleTimes(records []ScheduleRecord) []time.Time {
	out := make([]time.Time, len(records))
	for i, r := range records {
		out[i] = r.UpdatedAt
	}
	return out
}
