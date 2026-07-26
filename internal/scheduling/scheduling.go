package scheduling

import (
	"database/sql"
	"time"
)

// ResolveActiveSchedule mencari jadwal (schedules_ref) yang sedang aktif
// untuk sebuah kelas/ruang pada waktu tertentu. Dipakai supaya device
// yang terpasang tetap di 1 ruang (mis. Lab Komputer) bisa otomatis tahu
// "sekarang lagi jam pelajaran apa" tanpa device perlu tahu jadwal detail
// sama sekali — device cuma perlu tahu class_id-nya sendiri.
//
// Mengembalikan schedule_id kosong ("") kalau tidak ada jadwal aktif saat
// ini (mis. jam istirahat, atau di luar jam sekolah) — bukan error, itu
// kondisi normal.
func ResolveActiveSchedule(db *sql.DB, classID string, now time.Time) (string, error) {
	if classID == "" {
		return "", nil
	}

	// day_of_week: 1=Senin .. 7=Minggu. time.Weekday Go: 0=Minggu..6=Sabtu,
	// jadi perlu konversi.
	dow := int(now.Weekday())
	if dow == 0 {
		dow = 7
	}

	currentTime := now.Format("15:04:05")

	var scheduleID string
	err := db.QueryRow(`
		SELECT schedule_id FROM schedules_ref
		WHERE class_id = $1
		  AND day_of_week = $2
		  AND is_active = true
		  AND start_time <= $3::time
		  AND end_time >= $3::time
		LIMIT 1
	`, classID, dow, currentTime).Scan(&scheduleID)

	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return scheduleID, nil
}
