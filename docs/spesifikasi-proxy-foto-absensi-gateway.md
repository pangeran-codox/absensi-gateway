# Spesifikasi: Proxy & Cache Foto di `absensi-gateway`

Dibuat: 9 Sep 2026. Ditujukan untuk siapa pun yang mengerjakan
`absensi-gateway` (Go). Menjelaskan cara memakai `photo_url` yang sudah
dikirim lewat sync `people`, supaya foto bisa ditampilkan ke
browser/kiosk dengan **cepat** dan **akurat**, tanpa mengekspos Laravel
langsung ke jaringan publik/sekolah.

---

## 1. Yang sudah tersedia dari sisi Laravel

Endpoint `GET /api/internal/sync/people` (sudah aktif, sudah dites)
mengirim field `photo_url` per orang, contoh:

```json
{
  "person_id": "c6b1c7c0-4893-441f-bb91-c2818a8d49a9",
  "photo_url": "http://nginx/media/person-photo/GvvsqdoEILtKop3cmbuOgwluwCrtb1orGtduyc9t",
  "updated_at": "2026-09-09T00:52:32+00:00",
  ...
}
```

**Sifat-sifat penting `photo_url` ini:**

- **Cuma bisa diakses dari dalam Docker network yang sama** (`network`
  di lokal, `shared_network` di production nanti) — host `nginx` itu
  DNS internal Docker, **bukan** domain publik. Sengaja dibiarkan
  begitu (bukan bug) — Laravel memang tidak didesain untuk diakses
  langsung dari browser/kiosk, cuma dari service lain di jaringan yang
  sama (persis seperti gateway mengakses endpoint sync ini sendiri).
- **`null` kalau orang itu belum punya foto** — selalu cek ini dulu
  sebelum mencoba fetch.
- **Token di URL berubah otomatis setiap foto diganti** — begitu foto
  siswa/guru di-update lewat Laravel, `photo_url` di sync berikutnya
  akan berbeda persis (token baru). Token LAMA otomatis berhenti valid
  (404) begitu diganti — tidak ada mekanisme "kedaluwarsa berdasarkan
  waktu", murni berdasarkan foto masih dipakai atau tidak.
- **`updated_at` di record yang sama** mencerminkan kapan record
  (termasuk foto) terakhir berubah — field ini yang dipakai untuk
  invalidasi cache (lihat bagian 3).

## 2. Yang perlu dibangun di sisi Go

Endpoint baru di gateway, contoh: `GET /api/v1/media/photo/{person_id}`
— inilah yang dipanggil browser/kiosk (lewat domain publik gateway,
`/gateway/...` via NPM), **bukan** `photo_url` Laravel secara langsung.

Alur di dalam endpoint ini:
1. Cari `photo_url` untuk `person_id` ini dari cache lokal `people_ref`
   (data yang sudah disinkron, sudah ada di database gateway).
2. Kalau `photo_url` kosong/`null` → balas `404`, atau serve avatar
   inisial default (opsional, sesuai kebutuhan UI).
3. Kalau ada, lanjut ke strategi cache di bagian 3.

## 3. Strategi cache — kenapa dan bagaimana

**Kenapa perlu cache (bukan langsung proxy tiap request):**
- Setiap request foto baru dari browser kalau langsung diteruskan ke
  Laravel berarti 1 round-trip network tambahan tiap kali — untuk 300+
  siswa yang fotonya sering ditampilkan berulang (mis. di kiosk
  absensi), ini boros dan lambat.
- Cache lokal di gateway membuat foto yang sudah pernah diambil
  langsung tersedia tanpa perlu tanya Laravel lagi.

**Cara invalidasi cache — pakai `updated_at`, bukan waktu kedaluwarsa
tetap:**
- Simpan cache dengan key gabungan `person_id` + `updated_at` (atau
  simpan `updated_at` sebagai metadata cache terpisah).
- Saat melayani request foto: bandingkan `updated_at` yang tersimpan di
  cache dengan `updated_at` terbaru dari `people_ref` (data ini sudah
  di-refresh otomatis tiap `SYNC_INTERVAL` — lihat
  `laravel-sync-contract.md`).
- Kalau `updated_at` cache **sama** dengan yang di `people_ref` → cache
  masih akurat, serve dari cache.
- Kalau **beda** (lebih lama) → cache basi (foto sudah diganti di
  Laravel sejak terakhir di-cache) → fetch ulang dari `photo_url`
  terbaru, timpa cache, serve yang baru.

Ini memastikan **akurat** (tidak pernah serve foto lama setelah
diganti) sekaligus **cepat** (tidak fetch ulang kalau memang belum
berubah).

**Implementasi cache:** bebas dipilih sesuai yang paling praktis untuk
skala saat ini — cache di disk lokal (`/data/photo-cache/{person_id}`)
atau in-memory dengan TTL pendek + validasi `updated_at` saat cache
hit. Tidak perlu Redis/sistem cache terpisah kecuali memang sudah ada
infrastrukturnya.

## 4. Response headers ke browser

Setelah gateway serve foto (dari cache atau hasil fetch baru), sertakan
header cache di level HTTP juga, supaya browser sendiri tidak perlu
minta ulang ke gateway kalau memang sudah punya:

```
Cache-Control: private, max-age=86400
```

`private` karena ini foto individu (bukan aset publik bersama).
`max-age` bisa disesuaikan — 1 hari cukup aman karena begitu foto
benar-benar berubah di Laravel, endpoint gateway ini otomatis
mendeteksi lewat `updated_at` pada request berikutnya yang menembus
cache browser (setelah `max-age` habis).

## 5. Ringkasan alur lengkap (end-to-end)

```
Browser/kiosk
    │  GET /gateway/api/v1/media/photo/{person_id}   (lewat NPM, domain publik)
    ▼
absensi-gateway (Go)
    │  Cek people_ref: photo_url + updated_at untuk person_id ini
    │  Cek cache lokal: ada & updated_at cocok?
    │    ├─ Ya  → serve dari cache
    │    └─ Tidak → GET {photo_url} ke Laravel (via network internal)
    │                → simpan ke cache (key: person_id + updated_at)
    │                → serve
    ▼
Browser menampilkan foto, dengan Cache-Control dari gateway
```

## 6. Yang TIDAK perlu diubah

- Format `photo_url` dari Laravel — sudah benar apa adanya, cuma
  dikonsumsi server-to-server oleh gateway, bukan diteruskan mentah ke
  browser.
- Tidak perlu ubah apapun di `SyncController::resolvePhotoUrl()` atau
  `PersonPhotoController` di Laravel.
