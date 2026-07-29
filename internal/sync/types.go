package sync

import "time"

// Bentuk record ini adalah KONTRAK yang harus dipenuhi endpoint Laravel
// (lihat docs/laravel-sync-contract.md untuk spesifikasi lengkap tiap
// endpoint). Nama field JSON sengaja dibuat identik dengan nama kolom di
// schools_ref/people_ref/schedules_ref supaya gampang ditelusuri.

// SchoolRecord — satu baris dari GET /api/internal/sync/schools.
type SchoolRecord struct {
	SchoolID             string    `json:"school_id"`
	Name                 string    `json:"name"`
	Latitude             float64   `json:"latitude"`
	Longitude            float64   `json:"longitude"`
	GeofenceRadiusMeters int       `json:"geofence_radius_meters"`
	// LateCutoffTime format "HH:MM:SS", NULLABLE (pointer) — sekolah yang
	// belum mengatur jam masuk di Laravel akan mengirim null di sini, dan
	// agregasi absen di gateway tidak akan menandai siapa pun "Terlambat"
	// untuk sekolah itu (cuma Hadir) sampai nilainya diisi.
	LateCutoffTime *string   `json:"late_cutoff_time"`
	IsActive       bool      `json:"is_active"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// PersonRecord — satu baris dari GET /api/internal/sync/people.
// PhotoURL, ClassID, Grade nullable (pointer) karena memang opsional di
// Laravel — misal siswa yang belum difoto, atau staff yang tidak punya
// class_id.
type PersonRecord struct {
	PersonID   string    `json:"person_id"`
	SchoolID   string    `json:"school_id"`
	PersonType string    `json:"person_type"` // "student" | "teacher" | "staff"
	FullName   string    `json:"full_name"`
	PhotoURL   *string   `json:"photo_url"`
	ClassID    *string   `json:"class_id"`
	Grade      *string   `json:"grade"`
	IsActive   bool      `json:"is_active"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ScheduleRecord — satu baris dari GET /api/internal/sync/schedules.
// StartTime/EndTime dikirim sebagai string format "HH:MM:SS" (bukan
// time.Time) karena ini jam-dalam-hari berulang tiap minggu, bukan
// timestamp — mengikuti tipe kolom `time` (bukan `timestamp`) di skema.
type ScheduleRecord struct {
	ScheduleID  string    `json:"schedule_id"`
	SchoolID    string    `json:"school_id"`
	ClassID     string    `json:"class_id"`
	SubjectName string    `json:"subject_name"`
	TeacherID   string    `json:"teacher_id"`
	DayOfWeek   int       `json:"day_of_week"` // 1=Senin .. 7=Minggu
	StartTime   string    `json:"start_time"`
	EndTime     string    `json:"end_time"`
	IsActive    bool      `json:"is_active"`
	UpdatedAt   time.Time `json:"updated_at"`
}
