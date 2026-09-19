package media

import (
	"os"
	"testing"
)

func newTestCache(t *testing.T) *DiskCache {
	t.Helper()
	dir, err := os.MkdirTemp("", "photo-cache-test-*")
	if err != nil {
		t.Fatalf("gagal bikin temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	c, err := NewDiskCache(dir)
	if err != nil {
		t.Fatalf("NewDiskCache gagal: %v", err)
	}
	return c
}

func TestDiskCache_PutThenGet_SameSyncedAt(t *testing.T) {
	c := newTestCache(t)
	want := []byte("isi foto palsu")

	if err := c.Put("person-1", "2026-09-09T10:00:00Z", "image/jpeg", want); err != nil {
		t.Fatalf("Put gagal: %v", err)
	}

	data, contentType, ok := c.Get("person-1", "2026-09-09T10:00:00Z")
	if !ok {
		t.Fatal("mau cache hit, malah miss")
	}
	if string(data) != string(want) {
		t.Errorf("data cache tidak sesuai, mau %q dapat %q", want, data)
	}
	if contentType != "image/jpeg" {
		t.Errorf("content type mau image/jpeg, dapat %q", contentType)
	}
}

func TestDiskCache_Get_DifferentSyncedAt_Miss(t *testing.T) {
	c := newTestCache(t)
	c.Put("person-1", "2026-09-09T10:00:00Z", "image/jpeg", []byte("foto lama"))

	// synced_at BEDA (foto sudah berubah di Laravel sejak terakhir di-cache)
	// -> harus dianggap cache miss, bukan serve yang lama.
	_, _, ok := c.Get("person-1", "2026-09-09T11:00:00Z")
	if ok {
		t.Fatal("mau cache miss karena synced_at beda, malah hit")
	}
}

func TestDiskCache_Get_NeverCached_Miss(t *testing.T) {
	c := newTestCache(t)
	_, _, ok := c.Get("person-yang-belum-pernah-dicache", "2026-09-09T10:00:00Z")
	if ok {
		t.Fatal("mau miss untuk person yang belum pernah di-cache")
	}
}

func TestDiskCache_Isolation_BetweenPersons(t *testing.T) {
	c := newTestCache(t)
	c.Put("person-1", "2026-09-09T10:00:00Z", "image/jpeg", []byte("foto orang 1"))
	c.Put("person-2", "2026-09-09T10:00:00Z", "image/png", []byte("foto orang 2"))

	data1, ct1, ok1 := c.Get("person-1", "2026-09-09T10:00:00Z")
	data2, ct2, ok2 := c.Get("person-2", "2026-09-09T10:00:00Z")

	if !ok1 || !ok2 {
		t.Fatal("dua-duanya harusnya cache hit")
	}
	if string(data1) != "foto orang 1" || string(data2) != "foto orang 2" {
		t.Errorf("data tertukar antar person: data1=%q data2=%q", data1, data2)
	}
	if ct1 != "image/jpeg" || ct2 != "image/png" {
		t.Errorf("content type tertukar: ct1=%q ct2=%q", ct1, ct2)
	}
}

func TestDiskCache_Put_Overwrite(t *testing.T) {
	c := newTestCache(t)
	c.Put("person-1", "2026-09-09T10:00:00Z", "image/jpeg", []byte("foto versi 1"))
	c.Put("person-1", "2026-09-09T11:00:00Z", "image/png", []byte("foto versi 2"))

	data, contentType, ok := c.Get("person-1", "2026-09-09T11:00:00Z")
	if !ok {
		t.Fatal("mau cache hit untuk synced_at terbaru")
	}
	if string(data) != "foto versi 2" || contentType != "image/png" {
		t.Errorf("cache tidak ke-overwrite dengan versi baru, dapat data=%q contentType=%q", data, contentType)
	}
}

func TestDiskCache_GetStale_ReturnsRegardlessOfSyncedAt(t *testing.T) {
	c := newTestCache(t)
	c.Put("person-1", "2026-09-09T10:00:00Z", "image/jpeg", []byte("foto basi"))

	// GetStale TIDAK peduli synced_at cocok atau tidak -- dipakai sebagai
	// fallback darurat saat fetch ke Laravel gagal.
	data, contentType, ok := c.GetStale("person-1")
	if !ok {
		t.Fatal("mau GetStale berhasil ambil cache lama")
	}
	if string(data) != "foto basi" || contentType != "image/jpeg" {
		t.Errorf("data GetStale tidak sesuai: data=%q contentType=%q", data, contentType)
	}
}

func TestDiskCache_GetStale_NeverCached_Miss(t *testing.T) {
	c := newTestCache(t)
	_, _, ok := c.GetStale("person-tidak-pernah-ada")
	if ok {
		t.Fatal("mau miss untuk person yang belum pernah di-cache sama sekali")
	}
}
