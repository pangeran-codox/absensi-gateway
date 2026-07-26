package middleware

import (
	"sync"
	"time"
)

// Parameter pembatasan percobaan device key. Angka ini sengaja longgar
// (bukan super ketat) karena device fisik yang koneksinya kadang putus-
// nyambung (wifi sekolah) bisa wajar gagal beberapa kali berturut-turut,
// bukan cuma karena diserang.
const (
	maxFailedDeviceAuthAttempts = 10
	failedAttemptWindow         = 5 * time.Minute
	deviceAuthLockoutDuration   = 15 * time.Minute
)

// attemptTracker mencatat percobaan gagal per IP untuk satu jenis endpoint
// (di sini dipakai untuk X-Device-Key). Ini pengaman brute-force: tanpa
// ini, orang bisa nyoba nebak device key berkali-kali tanpa batas.
//
// Disimpan in-memory (map + mutex) — cukup untuk 1 instance gateway.
// CATATAN SKALA: kalau nanti gateway ini di-scale ke banyak instance
// (mis. lewat Docker Swarm replicas), catatan ini TIDAK dibagi antar
// instance — penyerang yang requestnya "diacak" round-robin ke instance
// berbeda bisa dapat lebih banyak jatah percobaan daripada yang
// dimaksudkan. Kalau itu terjadi nanti, tracker ini perlu dipindah ke
// Redis (semua instance baca/tulis ke tempat yang sama).
type attemptTracker struct {
	mu   sync.Mutex
	data map[string]*ipAttempts
}

type ipAttempts struct {
	count       int
	windowStart time.Time
	lockedUntil time.Time
}

func newAttemptTracker() *attemptTracker {
	return &attemptTracker{data: make(map[string]*ipAttempts)}
}

// isLocked mengecek TANPA mencatat percobaan baru apakah IP ini sedang
// dikunci. locked=true berarti request harus ditolak sebelum sempat
// mencoba verifikasi apapun ke database.
func (t *attemptTracker) isLocked(ip string) (locked bool, retryAfter time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	a, ok := t.data[ip]
	if !ok {
		return false, 0
	}
	if now := time.Now(); now.Before(a.lockedUntil) {
		return true, a.lockedUntil.Sub(now)
	}
	return false, 0
}

// recordFailure mencatat 1 percobaan gagal dari sebuah IP. Kalau dalam
// jendela waktu berjalan (failedAttemptWindow) IP itu sudah gagal
// maxFailedDeviceAuthAttempts kali, IP tersebut dikunci selama
// deviceAuthLockoutDuration.
func (t *attemptTracker) recordFailure(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	a, ok := t.data[ip]
	if !ok || now.Sub(a.windowStart) > failedAttemptWindow {
		// Belum pernah gagal, atau jendela waktu sebelumnya sudah lewat —
		// mulai hitungan baru dari 0.
		a = &ipAttempts{windowStart: now}
		t.data[ip] = a
	}

	a.count++
	if a.count >= maxFailedDeviceAuthAttempts {
		a.lockedUntil = now.Add(deviceAuthLockoutDuration)
	}
}

// recordSuccess menghapus riwayat gagal untuk IP ini. Dipanggil setelah
// autentikasi BERHASIL, supaya device asli yang kebetulan beberapa kali
// gagal (mis. salah konfigurasi lalu dibetulkan) tidak ikut kena kunci
// gara-gara riwayat lama yang sebenarnya sudah tidak relevan.
func (t *attemptTracker) recordSuccess(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.data, ip)
}
