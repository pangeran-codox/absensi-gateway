package sync

import (
	"testing"
	"time"
)

func TestMaxUpdatedAt(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)

	t.Run("belum ada watermark, ambil waktu terbaru dari records", func(t *testing.T) {
		got := maxUpdatedAt(nil, []time.Time{t1, t3, t2})
		if got == nil || !got.Equal(t3) {
			t.Fatalf("mau %v, dapat %v", t3, got)
		}
	})

	t.Run("watermark lama lebih baru dari semua records, watermark tidak mundur", func(t *testing.T) {
		newer := t3.Add(24 * time.Hour)
		got := maxUpdatedAt(&newer, []time.Time{t1, t2})
		if got == nil || !got.Equal(newer) {
			t.Fatalf("watermark seharusnya tidak mundur, mau %v, dapat %v", newer, got)
		}
	})

	t.Run("records kosong, watermark lama dipertahankan apa adanya", func(t *testing.T) {
		got := maxUpdatedAt(&t1, nil)
		if got == nil || !got.Equal(t1) {
			t.Fatalf("mau %v, dapat %v", t1, got)
		}
	})

	t.Run("watermark dan records semua nil/kosong, hasilnya nil", func(t *testing.T) {
		got := maxUpdatedAt(nil, nil)
		if got != nil {
			t.Fatalf("mau nil, dapat %v", got)
		}
	})
}
