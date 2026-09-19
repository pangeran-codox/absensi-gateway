// Package media menangani cache & pengambilan foto profil dari Laravel,
// dipakai endpoint GET /api/v1/media/photo/{person_id} (lihat
// internal/handlers/media.go).
//
// Sesuai spesifikasi tim Laravel (spesifikasi-proxy-foto-absensi-gateway.md):
// gateway TIDAK meneruskan photo_url Laravel mentah-mentah ke browser
// (itu cuma bisa diakses dari jaringan Docker internal, bukan publik) —
// gateway proxy + cache foto itu sendiri, disajikan lewat domain publik
// gateway.
package media

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// meta disimpan berdampingan dengan file foto — berisi "kapan foto ini
// terakhir diketahui berubah" (dari people_ref.synced_at) dan tipe
// kontennya (dari header Content-Type respons Laravel).
type meta struct {
	SyncedAt    string `json:"synced_at"`
	ContentType string `json:"content_type"`
}

// DiskCache menyimpan foto yang sudah pernah diambil ke folder lokal,
// dan tau kapan harus dianggap basi lewat perbandingan `syncedAt` (bukan
// waktu kedaluwarsa tetap) — persis seperti yang diminta spesifikasi.
type DiskCache struct {
	dir string
}

// NewDiskCache menyiapkan folder cache (dibuat otomatis kalau belum ada).
func NewDiskCache(dir string) (*DiskCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("gagal membuat folder cache foto %q: %w", dir, err)
	}
	return &DiskCache{dir: dir}, nil
}

// cacheKey mengubah person_id jadi nama file yang aman (hash SHA-256) —
// bukan supaya "aman" secara kriptografis, cuma supaya person_id yang
// aneh-aneh (harusnya selalu UUID, tapi jaga-jaga) tidak pernah jadi path
// traversal atau nama file tidak valid di filesystem.
func (c *DiskCache) cacheKey(personID string) string {
	sum := sha256.Sum256([]byte(personID))
	return hex.EncodeToString(sum[:])
}

func (c *DiskCache) dataPath(personID string) string {
	return filepath.Join(c.dir, c.cacheKey(personID)+".data")
}

func (c *DiskCache) metaPath(personID string) string {
	return filepath.Join(c.dir, c.cacheKey(personID)+".meta.json")
}

// Get mengembalikan (data, contentType, true) HANYA kalau ada cache
// tersimpan UNTUK syncedAt yang PERSIS SAMA dengan yang diminta — kalau
// beda (foto sudah berubah sejak terakhir di-cache) atau belum pernah
// di-cache sama sekali, kembalikan ok=false supaya pemanggil tau harus
// fetch ulang dari Laravel.
func (c *DiskCache) Get(personID, syncedAt string) (data []byte, contentType string, ok bool) {
	metaBytes, err := os.ReadFile(c.metaPath(personID))
	if err != nil {
		return nil, "", false
	}

	var m meta
	if err := json.Unmarshal(metaBytes, &m); err != nil {
		return nil, "", false
	}

	if m.SyncedAt != syncedAt {
		// Cache basi — foto ini sudah berubah di Laravel sejak terakhir
		// disimpan (atau syncedAt yang diminta sekarang lebih baru).
		return nil, "", false
	}

	data, err = os.ReadFile(c.dataPath(personID))
	if err != nil {
		return nil, "", false
	}

	return data, m.ContentType, true
}

// Put menyimpan foto ke cache, menimpa cache lama untuk person_id yang
// sama kalau ada (tidak perlu dihapus manual dulu — os.WriteFile
// menimpa isi file yang sudah ada).
func (c *DiskCache) Put(personID, syncedAt, contentType string, data []byte) error {
	metaBytes, err := json.Marshal(meta{SyncedAt: syncedAt, ContentType: contentType})
	if err != nil {
		return fmt.Errorf("gagal encode metadata cache: %w", err)
	}

	if err := os.WriteFile(c.dataPath(personID), data, 0o644); err != nil {
		return fmt.Errorf("gagal menyimpan file foto ke cache: %w", err)
	}
	if err := os.WriteFile(c.metaPath(personID), metaBytes, 0o644); err != nil {
		return fmt.Errorf("gagal menyimpan metadata cache: %w", err)
	}
	return nil
}

// GetStale mengembalikan cache APAPUN kondisinya (basi atau tidak) —
// dipakai HANYA sebagai fallback saat fetch ke Laravel gagal (mis.
// Laravel sedang down), supaya kiosk/browser tetap dapat foto yang
// "agak lama" daripada tidak dapat apa-apa sama sekali.
func (c *DiskCache) GetStale(personID string) (data []byte, contentType string, ok bool) {
	metaBytes, err := os.ReadFile(c.metaPath(personID))
	if err != nil {
		return nil, "", false
	}
	var m meta
	if err := json.Unmarshal(metaBytes, &m); err != nil {
		return nil, "", false
	}
	data, err = os.ReadFile(c.dataPath(personID))
	if err != nil {
		return nil, "", false
	}
	return data, m.ContentType, true
}
