package sync

import (
	"context"
	"database/sql"
	"fmt"
)

// UpsertSchools menyimpan/memperbarui records ke schools_ref dalam 1
// transaksi (semua-atau-tidak-sama-sekali per siklus sync, supaya tidak
// ada kondisi "setengah ke-update" kalau salah satu baris gagal).
//
// UrutAN PENTING: UpsertSchools HARUS dipanggil SEBELUM UpsertPeople dan
// UpsertSchedules, karena people_ref & schedules_ref punya foreign key
// ke schools_ref(school_id) — kalau sekolahnya belum ada, insert
// people/schedules untuk sekolah itu akan gagal FK constraint.
func UpsertSchools(ctx context.Context, db *sql.DB, records []SchoolRecord) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("gagal memulai transaksi upsert schools: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
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
		return fmt.Errorf("gagal menyiapkan statement upsert schools: %w", err)
	}
	defer stmt.Close()

	for _, r := range records {
		if _, err := stmt.ExecContext(ctx,
			r.SchoolID, r.Name, r.Latitude, r.Longitude, r.GeofenceRadiusMeters, r.LateCutoffTime, r.IsActive,
		); err != nil {
			return fmt.Errorf("gagal upsert school %s: %w", r.SchoolID, err)
		}
	}

	return tx.Commit()
}

// UpsertPeople menyimpan/memperbarui records ke people_ref. Panggil
// SETELAH UpsertSchools (lihat catatan urutan di atas).
func UpsertPeople(ctx context.Context, db *sql.DB, records []PersonRecord) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("gagal memulai transaksi upsert people: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
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
		return fmt.Errorf("gagal menyiapkan statement upsert people: %w", err)
	}
	defer stmt.Close()

	for _, r := range records {
		if _, err := stmt.ExecContext(ctx,
			r.PersonID, r.SchoolID, r.PersonType, r.FullName, r.PhotoURL, r.ClassID, r.Grade, r.IsActive,
		); err != nil {
			return fmt.Errorf("gagal upsert person %s: %w", r.PersonID, err)
		}
	}

	return tx.Commit()
}

// UpsertSchedules menyimpan/memperbarui records ke schedules_ref. Panggil
// SETELAH UpsertSchools (lihat catatan urutan di atas).
func UpsertSchedules(ctx context.Context, db *sql.DB, records []ScheduleRecord) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("gagal memulai transaksi upsert schedules: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
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
		return fmt.Errorf("gagal menyiapkan statement upsert schedules: %w", err)
	}
	defer stmt.Close()

	for _, r := range records {
		if _, err := stmt.ExecContext(ctx,
			r.ScheduleID, r.SchoolID, r.ClassID, r.SubjectName, r.TeacherID,
			r.DayOfWeek, r.StartTime, r.EndTime, r.IsActive,
		); err != nil {
			return fmt.Errorf("gagal upsert schedule %s: %w", r.ScheduleID, err)
		}
	}

	return tx.Commit()
}
