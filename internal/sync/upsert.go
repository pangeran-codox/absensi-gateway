package sync

import (
	"context"
	"database/sql"
	"fmt"
)

// UpsertFailure mencatat 1 record yang gagal di-upsert beserta alasannya.
//
// KENAPA INI PENTING (bukan cuma nice-to-have): sebelumnya semua record
// dalam 1 siklus sync dibungkus 1 transaksi besar — begitu SATU record
// gagal (mis. foreign key ke sekolah yang belum ada), SELURUH batch ikut
// batal, DAN watermark tidak maju, sehingga siklus berikutnya mengulang
// batch yang SAMA, gagal lagi di record yang SAMA, tanpa henti — 1 orang
// dengan data tidak lengkap bisa memblokir sinkronisasi SEMUA orang lain
// selamanya. Ini pernah beneran kejadian (9-10 Sept 2026, orang dari
// sekolah yang belum diisi koordinat GPS-nya).
//
// Sekarang tiap record diproses independen — yang gagal dicatat &
// dilewati, yang lain tetap disimpan. Watermark tetap maju mencakup
// SEMUA record yang di-fetch (termasuk yang gagal) — kalau nanti data
// sumbernya diperbaiki di Laravel (mis. sekolah itu diisi koordinatnya),
// record itu otomatis ke-fetch ulang di siklus berikutnya begitu
// updated_at-nya berubah lagi, TANPA butuh logic retry khusus.
type UpsertFailure struct {
	ID    string
	Cause error
}

// UpsertSchools menyimpan/memperbarui records ke schools_ref. Tiap
// record diproses independen (lihat UpsertFailure) — TIDAK dibungkus 1
// transaksi besar, supaya 1 baris bermasalah tidak menggagalkan yang lain.
//
// UrutAN PENTING: UpsertSchools HARUS dipanggil SEBELUM UpsertPeople dan
// UpsertSchedules, karena people_ref & schedules_ref punya foreign key
// ke schools_ref(school_id) — kalau sekolahnya belum ada, insert
// people/schedules untuk sekolah itu akan gagal FK constraint (dicatat
// sebagai UpsertFailure, bukan menggagalkan seluruh batch lagi).
func UpsertSchools(ctx context.Context, db *sql.DB, records []SchoolRecord) ([]UpsertFailure, error) {
	if len(records) == 0 {
		return nil, nil
	}

	stmt, err := db.PrepareContext(ctx, `
		INSERT INTO schools_ref
			(school_id, name, latitude, longitude, geofence_radius_meters, late_cutoff_time, is_active, synced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (school_id) DO UPDATE SET
			name                   = EXCLUDED.name,
			latitude               = EXCLUDED.latitude,
			longitude              = EXCLUDED.longitude,
			geofence_radius_meters = EXCLUDED.geofence_radius_meters,
			late_cutoff_time       = EXCLUDED.late_cutoff_time,
			is_active              = EXCLUDED.is_active,
			synced_at              = now()
	`)
	if err != nil {
		return nil, fmt.Errorf("gagal menyiapkan statement upsert schools: %w", err)
	}
	defer stmt.Close()

	var failures []UpsertFailure
	for _, r := range records {
		if _, err := stmt.ExecContext(ctx,
			r.SchoolID, r.Name, r.Latitude, r.Longitude, r.GeofenceRadiusMeters, r.LateCutoffTime, r.IsActive,
		); err != nil {
			failures = append(failures, UpsertFailure{ID: r.SchoolID, Cause: err})
		}
	}

	return failures, nil
}

// UpsertPeople menyimpan/memperbarui records ke people_ref. Panggil
// SETELAH UpsertSchools (lihat catatan urutan di atas). Tiap record
// diproses independen — lihat komentar UpsertFailure.
func UpsertPeople(ctx context.Context, db *sql.DB, records []PersonRecord) ([]UpsertFailure, error) {
	if len(records) == 0 {
		return nil, nil
	}

	stmt, err := db.PrepareContext(ctx, `
		INSERT INTO people_ref
			(person_id, school_id, person_type, full_name, photo_url, class_id, grade, is_active, synced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (person_id, person_type) DO UPDATE SET
			school_id  = EXCLUDED.school_id,
			full_name  = EXCLUDED.full_name,
			photo_url  = EXCLUDED.photo_url,
			class_id   = EXCLUDED.class_id,
			grade      = EXCLUDED.grade,
			is_active  = EXCLUDED.is_active,
			synced_at  = now()
	`)
	if err != nil {
		return nil, fmt.Errorf("gagal menyiapkan statement upsert people: %w", err)
	}
	defer stmt.Close()

	var failures []UpsertFailure
	for _, r := range records {
		if _, err := stmt.ExecContext(ctx,
			r.PersonID, r.SchoolID, r.PersonType, r.FullName, r.PhotoURL, r.ClassID, r.Grade, r.IsActive,
		); err != nil {
			failures = append(failures, UpsertFailure{ID: r.PersonID, Cause: err})
		}
	}

	return failures, nil
}

// UpsertSchedules menyimpan/memperbarui records ke schedules_ref. Panggil
// SETELAH UpsertSchools (lihat catatan urutan di atas). Tiap record
// diproses independen — lihat komentar UpsertFailure.
func UpsertSchedules(ctx context.Context, db *sql.DB, records []ScheduleRecord) ([]UpsertFailure, error) {
	if len(records) == 0 {
		return nil, nil
	}

	stmt, err := db.PrepareContext(ctx, `
		INSERT INTO schedules_ref
			(schedule_id, school_id, class_id, subject_name, teacher_id, day_of_week, start_time, end_time, is_active, synced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
		ON CONFLICT (schedule_id) DO UPDATE SET
			school_id    = EXCLUDED.school_id,
			class_id     = EXCLUDED.class_id,
			subject_name = EXCLUDED.subject_name,
			teacher_id   = EXCLUDED.teacher_id,
			day_of_week  = EXCLUDED.day_of_week,
			start_time   = EXCLUDED.start_time,
			end_time     = EXCLUDED.end_time,
			is_active    = EXCLUDED.is_active,
			synced_at    = now()
	`)
	if err != nil {
		return nil, fmt.Errorf("gagal menyiapkan statement upsert schedules: %w", err)
	}
	defer stmt.Close()

	var failures []UpsertFailure
	for _, r := range records {
		if _, err := stmt.ExecContext(ctx,
			r.ScheduleID, r.SchoolID, r.ClassID, r.SubjectName, r.TeacherID,
			r.DayOfWeek, r.StartTime, r.EndTime, r.IsActive,
		); err != nil {
			failures = append(failures, UpsertFailure{ID: r.ScheduleID, Cause: err})
		}
	}

	return failures, nil
}
