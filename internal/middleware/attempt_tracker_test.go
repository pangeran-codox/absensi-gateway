package middleware

import "testing"

func TestAttemptTracker_LocksAfterMaxFailures(t *testing.T) {
	tracker := newAttemptTracker()
	const ip = "10.0.0.5"

	// Sebelum mencapai batas, IP belum boleh dikunci.
	for i := 0; i < maxFailedDeviceAuthAttempts-1; i++ {
		tracker.recordFailure(ip)
		if locked, _ := tracker.isLocked(ip); locked {
			t.Fatalf("belum boleh terkunci di percobaan gagal ke-%d (batasnya %d)", i+1, maxFailedDeviceAuthAttempts)
		}
	}

	// Percobaan gagal ke-N (tepat di batas) harus mengunci IP tersebut.
	tracker.recordFailure(ip)
	locked, retryAfter := tracker.isLocked(ip)
	if !locked {
		t.Fatalf("harusnya sudah terkunci setelah %d percobaan gagal", maxFailedDeviceAuthAttempts)
	}
	if retryAfter <= 0 || retryAfter > deviceAuthLockoutDuration {
		t.Fatalf("retryAfter di luar rentang wajar: %v", retryAfter)
	}
}

func TestAttemptTracker_DifferentIPsIndependent(t *testing.T) {
	tracker := newAttemptTracker()

	for i := 0; i < maxFailedDeviceAuthAttempts; i++ {
		tracker.recordFailure("10.0.0.1")
	}

	if locked, _ := tracker.isLocked("10.0.0.1"); !locked {
		t.Fatal("10.0.0.1 harusnya terkunci")
	}
	// IP lain tidak boleh ikut terkunci gara-gara IP pertama gagal terus.
	if locked, _ := tracker.isLocked("10.0.0.2"); locked {
		t.Fatal("10.0.0.2 tidak seharusnya ikut terkunci")
	}
}

func TestAttemptTracker_SuccessResetsFailures(t *testing.T) {
	tracker := newAttemptTracker()
	const ip = "10.0.0.9"

	for i := 0; i < maxFailedDeviceAuthAttempts-1; i++ {
		tracker.recordFailure(ip)
	}
	// Berhasil autentikasi sebelum mencapai batas.
	tracker.recordSuccess(ip)

	// Setelah reset, harus butuh maxFailedDeviceAuthAttempts kegagalan LAGI
	// dari awal — bukan lanjut dari hitungan sebelumnya.
	for i := 0; i < maxFailedDeviceAuthAttempts-1; i++ {
		tracker.recordFailure(ip)
		if locked, _ := tracker.isLocked(ip); locked {
			t.Fatalf("tidak seharusnya terkunci di percobaan ke-%d setelah reset", i+1)
		}
	}
	tracker.recordFailure(ip)
	if locked, _ := tracker.isLocked(ip); !locked {
		t.Fatal("harusnya terkunci setelah mencapai batas lagi pasca-reset")
	}
}

func TestAttemptTracker_UnknownIPNotLocked(t *testing.T) {
	tracker := newAttemptTracker()
	if locked, _ := tracker.isLocked("192.168.1.1"); locked {
		t.Fatal("IP yang belum pernah tercatat tidak boleh dianggap terkunci")
	}
}
