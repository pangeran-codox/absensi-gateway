package handlers

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Warna latar avatar — dipilih konsisten per nama (bukan acak tiap kali),
// supaya orang yang sama selalu dapat warna yang sama tiap check-in.
var avatarColors = []string{
	"#F87171", "#FB923C", "#FBBF24", "#4ADE80",
	"#22D3EE", "#60A5FA", "#A78BFA", "#F472B6",
}

// initialsAvatarDataURI membuat avatar SVG sederhana (lingkaran warna +
// 1-2 huruf inisial) dan mengembalikannya sebagai data URI
// ("data:image/svg+xml;base64,...") — bisa langsung dipakai di tag <img
// src="..."> tanpa perlu request tambahan ke mana pun (fotonya "menyatu"
// di dalam response JSON itu sendiri).
//
// Ini FALLBACK, dipakai HANYA kalau people_ref.photo_url kosong (siswa/
// guru belum punya foto tersimpan) — supaya kiosk/HP selalu punya sesuatu
// untuk ditampilkan, konsisten di semua device, tanpa tiap aplikasi klien
// harus menulis ulang logic "kalau nggak ada foto, tampilkan apa".
func initialsAvatarDataURI(fullName string) string {
	svg := initialsAvatarSVG(fullName)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(svg)
}

// initialsAvatarSVG membuat isi SVG mentah (bukan data URI) — dipakai
// bersama oleh initialsAvatarDataURI (dibungkus base64 buat response
// JSON check-in) dan MediaHandler (diserve langsung sebagai image/svg+xml
// buat endpoint foto, lihat internal/handlers/media.go).
func initialsAvatarSVG(fullName string) []byte {
	initials := nameInitials(fullName)
	color := colorForName(fullName)

	return []byte(fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="200" height="200" viewBox="0 0 200 200">`+
			`<rect width="200" height="200" fill="%s"/>`+
			`<text x="100" y="100" font-family="Arial, sans-serif" font-size="80" `+
			`fill="#ffffff" text-anchor="middle" dominant-baseline="central">%s</text>`+
			`</svg>`,
		color, initials,
	))
}

// nameInitials mengambil huruf pertama dari 2 kata pertama nama (mis.
// "Andi Saputra" -> "AS"), atau 1 huruf kalau cuma 1 kata. Selalu
// uppercase, dan tidak pernah string kosong (fallback "?" kalau nama
// kosong — seharusnya tidak terjadi karena full_name NOT NULL di skema,
// tapi dijaga supaya fungsi ini tidak pernah panic/hasilkan SVG rusak).
func nameInitials(fullName string) string {
	words := strings.Fields(fullName)
	switch {
	case len(words) == 0:
		return "?"
	case len(words) == 1:
		return strings.ToUpper(string([]rune(words[0])[:1]))
	default:
		first := []rune(words[0])[0]
		second := []rune(words[1])[0]
		return strings.ToUpper(string(first) + string(second))
	}
}

// colorForName memilih 1 warna dari avatarColors secara KONSISTEN
// berdasarkan isi nama (bukan acak tiap request) — dihitung dari jumlah
// nilai byte nama, supaya nama yang sama selalu dapat warna yang sama
// tiap kali di-generate ulang.
func colorForName(fullName string) string {
	var sum int
	for _, b := range []byte(fullName) {
		sum += int(b)
	}
	return avatarColors[sum%len(avatarColors)]
}
