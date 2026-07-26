package handlers

import "testing"

func TestIsValidCoordinate(t *testing.T) {
	cases := []struct {
		name     string
		lat, lng float64
		want     bool
	}{
		{"koordinat wajar Jakarta", -6.2, 106.8, true},
		{"batas maksimum tepat", 90, 180, true},
		{"batas minimum tepat", -90, -180, true},
		{"nol,nol (default kosong)", 0, 0, true}, // format valid; ketidakwajaran ditangani oleh geofence radius, bukan di sini
		{"latitude kelewat besar", 99999, 106.8, false},
		{"latitude kelewat kecil", -91, 106.8, false},
		{"longitude kelewat besar", -6.2, 200, false},
		{"longitude kelewat kecil", -6.2, -200, false},
		{"dua-duanya ngaco", 999, -999, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isValidCoordinate(c.lat, c.lng)
			if got != c.want {
				t.Errorf("isValidCoordinate(%v, %v) = %v, mau %v", c.lat, c.lng, got, c.want)
			}
		})
	}
}
