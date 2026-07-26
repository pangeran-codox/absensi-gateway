package middleware

import "net/http"

// Ukuran maksimum body request, disesuaikan per jenis endpoint. Tanpa batas
// ini, endpoint bisa dikirimi body raksasa (mis. base64 image ratusan MB,
// dikirim berkali-kali) sampai server kehabisan memori — DoS (Denial of
// Service): server jadi lemot/mati karena kebanjiran, bukan karena diretas
// datanya, tapi karena "ditenggelamkan".
const (
	// SizeSmall untuk endpoint yang body-nya cuma field pendek (string/angka)
	// — check-in guru, heartbeat, dll. Body wajar untuk ini jauh di bawah 1 KB.
	SizeSmall = 4 * 1024 // 4 KB

	// SizeImage untuk endpoint yang membawa 1 foto (face check-in device).
	SizeImage = 6 * 1024 * 1024 // 6 MB

	// SizeEnrollment untuk enrollment kredensial face, yang bisa membawa
	// beberapa foto sekaligus (images_base64 array).
	SizeEnrollment = 10 * 1024 * 1024 // 10 MB
)

// MaxBodySize membatasi ukuran body yang boleh dibaca dari request.
// Kalau klien kirim lebih dari maxBytes, pembacaan body (lewat
// json.Decoder atau reader manapun) akan berhenti dengan error
// *http.MaxBytesError — bukan diam-diam terpotong atau bikin server
// kehabisan memori dulu baru gagal.
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}
