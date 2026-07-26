package middleware

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP mengambil IP asli klien. Asumsi topologi: HANYA ADA 1 reverse
// proxy tepercaya di depan gateway ini (Nginx Proxy Manager) — klien luar
// tidak bisa langsung menghubungi gateway ini tanpa lewat NPM.
//
// Urutan prioritas:
//  1. Header X-Real-IP — Nginx (termasuk NPM) biasanya MENIMPA header ini
//     dengan IP asli yang konek ke dirinya, bukan meneruskan nilai dari
//     klien mentah-mentah. Jadi ini paling bisa dipercaya.
//  2. Header X-Forwarded-For, tapi ambil elemen PALING BELAKANG, bukan
//     paling depan. X-Forwarded-For itu daftar yang bisa "ditumpuk": kalau
//     klien nakal sudah mengisi header ini sendiri (mis. "X-Forwarded-For:
//     1.2.3.4" — IP bohongan), NPM akan MENAMBAHKAN IP asli klien di
//     BELAKANG daftar itu, bukan menggantinya. Jadi elemen pertama bisa
//     berisi klaim palsu dari klien, sedangkan elemen terakhir adalah yang
//     ditambahkan NPM sendiri saat request itu benar-benar sampai ke NPM.
//  3. RemoteAddr — fallback kalau kedua header di atas tidak ada sama
//     sekali (mis. testing langsung tanpa lewat proxy).
//
// PENTING: ini valid SELAMA gateway betul-betul hanya bisa diakses lewat
// NPM (tidak ada jalur lain yang expose port gateway langsung ke internet).
// Kalau topologinya berubah (mis. nambah proxy/load balancer lagi di
// depan NPM), jumlah hop tepercaya berubah dan logic ini perlu disesuaikan.
//
// Dipakai bersama oleh checkin_teacher.go (validasi jaringan sekolah) dan
// auth.go (pembatas percobaan device key) — sengaja 1 fungsi, bukan
// disalin dua kali, supaya logic penentuan IP tepercaya ini tidak
// diam-diam berbeda antara dua tempat yang sama-sama bergantung padanya.
func ClientIP(r *http.Request) string {
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[len(parts)-1])
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
