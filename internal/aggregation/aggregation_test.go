package aggregation

import (
	"testing"
	"time"
)

func TestLookbackStartDate(t *testing.T) {
	cases := []struct {
		name         string
		now          time.Time
		lookbackDays int
		want         time.Time
	}{
		{
			name:         "lookback 2 hari (default)",
			now:          time.Date(2026, 7, 26, 15, 30, 0, 0, time.UTC),
			lookbackDays: 2,
			want:         time.Date(2026, 7, 24, 15, 30, 0, 0, time.UTC),
		},
		{
			name:         "lookback 1 hari",
			now:          time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC),
			lookbackDays: 1,
			want:         time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC),
		},
		{
			name:         "lookback melewati batas bulan",
			now:          time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			lookbackDays: 3,
			want:         time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := lookbackStartDate(c.now, c.lookbackDays)
			if !got.Equal(c.want) {
				t.Errorf("lookbackStartDate(%v, %d) = %v, mau %v", c.now, c.lookbackDays, got, c.want)
			}
		})
	}
}
