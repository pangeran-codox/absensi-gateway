// Package geofence menghitung apakah sebuah titik koordinat GPS berada
// dalam radius tertentu dari titik referensi (lokasi sekolah).
package geofence

import "math"

const earthRadiusMeters = 6371000

// WithinRadius mengembalikan true kalau titik (lat, lng) berjarak <=
// radiusMeters dari titik referensi (refLat, refLng), dihitung pakai
// rumus Haversine (jarak garis lurus di permukaan bumi, mengabaikan
// ketinggian/elevasi).
//
// Catatan: akurasi GPS HP biasa +/- 5-20 meter, jadi radiusMeters
// sebaiknya diberi toleransi (mis. 150m seperti default di
// docker-compose.yml), bukan diset terlalu ketat sampai pas-pasan
// dengan luas gedung sekolah.
func WithinRadius(lat, lng, refLat, refLng, radiusMeters float64) bool {
	distance := haversineDistance(lat, lng, refLat, refLng)
	return distance <= radiusMeters
}

// haversineDistance menghitung jarak dalam meter antara dua titik
// koordinat (lat/lng dalam derajat desimal).
func haversineDistance(lat1, lng1, lat2, lng2 float64) float64 {
	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	deltaLat := (lat2 - lat1) * math.Pi / 180
	deltaLng := (lng2 - lng1) * math.Pi / 180

	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(deltaLng/2)*math.Sin(deltaLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusMeters * c
}
