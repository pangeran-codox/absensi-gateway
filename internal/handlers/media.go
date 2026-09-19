package handlers

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"absensi-gateway/internal/media"
)

type MediaHandler struct {
	DB    *sql.DB
	Cache *media.DiskCache
}

func NewMediaHandler(db *sql.DB, cache *media.DiskCache) *MediaHandler {
	return &MediaHandler{DB: db, Cache: cache}
}

// GetPhoto adalah endpoint GET /api/v1/media/photo/{person_id}. Dipanggil
// langsung dari <img src="..."> di browser/kiosk lewat domain publik
// gateway (via NPM) — GATEWAY yang proxy + cache foto dari Laravel,
// browser TIDAK PERNAH diarahkan ke people_ref.photo_url secara
// langsung (itu cuma bisa diakses dari jaringan Docker internal, sesuai
// spesifikasi tim Laravel).
//
// SENGAJA TIDAK ADA AUTENTIKASI di endpoint ini — ini keputusan sadar
// (bukan kelupaan): <img src> tidak bisa mengirim header custom
// (Authorization/X-Device-Key), jadi perlindungannya mengandalkan
// person_id berbentuk UUID yang praktis tidak bisa ditebak, plus
// jaringan yang sudah dibatasi lewat NPM. Kalau kebutuhan keamanan foto
// ini berubah nanti (mis. dianggap lebih sensitif), pertimbangkan token
// sementara yang disisipkan di URL, bukan auth header biasa (yang tidak
// bisa dipakai tag <img> sama sekali).
func (h *MediaHandler) GetPhoto(w http.ResponseWriter, r *http.Request) {
	personID := r.PathValue("person_id")
	if personID == "" {
		writeError(w, http.StatusBadRequest, "missing_person_id", "person_id wajib diisi di path")
		return
	}

	// Catatan: people_ref primary key sebenarnya (person_id, person_type)
	// — path endpoint ini cuma punya person_id. Query di bawah TIDAK
	// membedakan person_type, jadi kalau (secara teori) ada 2 orang beda
	// tipe kebetulan share person_id yang sama, yang diambil adalah yang
	// paling baru di-sync (ORDER BY synced_at DESC). Risiko ini sangat
	// kecil karena Laravel generate UUID acak per record, bukan reused.
	var photoURL sql.NullString
	var fullName string
	var syncedAt time.Time
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT photo_url, full_name, synced_at
		FROM people_ref
		WHERE person_id = $1
		ORDER BY synced_at DESC
		LIMIT 1
	`, personID).Scan(&photoURL, &fullName, &syncedAt)

	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "person_not_found", "person_id tidak ditemukan")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Gagal mengambil data orang")
		return
	}

	// Belum punya foto tersimpan di people_ref -> avatar inisial, bukan
	// error. Konsisten dengan photo_url di response check-in device
	// (avatar.go) — selalu ada sesuatu untuk ditampilkan.
	if !photoURL.Valid || photoURL.String == "" {
		serveSVG(w, initialsAvatarSVG(fullName))
		return
	}

	// syncedAt dipakai sebagai "cap waktu" buat validasi cache — foto
	// dianggap masih akurat SELAMA syncedAt-nya belum berubah sejak
	// terakhir di-cache (lihat internal/media/cache.go).
	syncedAtKey := syncedAt.Format(time.RFC3339Nano)

	if data, contentType, ok := h.getFromCache(personID, syncedAtKey); ok {
		serveImage(w, data, contentType)
		return
	}

	data, contentType, err := media.FetchPhoto(r.Context(), photoURL.String)
	if err != nil {
		log.Printf("media: gagal ambil foto person_id=%s: %v", personID, err)

		// Laravel lagi bermasalah -> coba serve cache LAMA (foto agak
		// basi masih lebih baik daripada kiosk menampilkan ikon gambar
		// rusak sama sekali).
		if staleData, staleContentType, ok := h.getStaleFromCache(personID); ok {
			serveImage(w, staleData, staleContentType)
			return
		}

		// Tidak ada cache sama sekali -> avatar inisial, BUKAN error 500.
		// Kegagalan mengambil foto tidak boleh membuat tampilan kiosk
		// jadi ikon gambar rusak.
		serveSVG(w, initialsAvatarSVG(fullName))
		return
	}

	h.putToCache(personID, syncedAtKey, contentType, data)

	serveImage(w, data, contentType)
}

// getFromCache/getStaleFromCache/putToCache membungkus akses h.Cache
// dengan pengecekan nil — h.Cache bisa nil kalau folder cache gagal
// disiapkan saat startup (lihat main.go). Kalau itu terjadi, endpoint
// ini TETAP bisa serve foto (langsung fetch tiap kali dari Laravel),
// cuma tanpa manfaat cache-nya — bukan ikut membuat gateway gagal
// start gara-gara masalah folder yang sifatnya non-esensial.
func (h *MediaHandler) getFromCache(personID, syncedAtKey string) ([]byte, string, bool) {
	if h.Cache == nil {
		return nil, "", false
	}
	return h.Cache.Get(personID, syncedAtKey)
}

func (h *MediaHandler) getStaleFromCache(personID string) ([]byte, string, bool) {
	if h.Cache == nil {
		return nil, "", false
	}
	return h.Cache.GetStale(personID)
}

func (h *MediaHandler) putToCache(personID, syncedAtKey, contentType string, data []byte) {
	if h.Cache == nil {
		return
	}
	if err := h.Cache.Put(personID, syncedAtKey, contentType, data); err != nil {
		// Gagal MENYIMPAN cache tidak boleh menggagalkan request yang
		// sedang dilayani — foto tetap diserve, cuma request berikutnya
		// akan fetch ulang lagi (kurang optimal, bukan bug fatal).
		log.Printf("media: gagal simpan cache foto person_id=%s: %v", personID, err)
	}
}

func serveImage(w http.ResponseWriter, data []byte, contentType string) {
	w.Header().Set("Content-Type", contentType)
	// private: ini foto individu, bukan aset publik bersama. max-age 1
	// hari cukup aman -- begitu foto beneran berubah, permintaan
	// berikutnya (setelah cache browser habis) otomatis dapat versi baru
	// lewat pengecekan synced_at di atas.
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func serveSVG(w http.ResponseWriter, svg []byte) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.WriteHeader(http.StatusOK)
	w.Write(svg)
}
