# Status Absensi Gateway & Tugas untuk Laravel

Dokumen ini jawaban untuk 2 pertanyaan: **(1) endpoint absen ini
sekarang udah bisa apa aja**, dan **(2) apa yang harus dibuat di sisi
Laravel** supaya semuanya bisa dipakai penuh — bukan cuma testing pakai
data dummy.

---

## Bagian 1: Endpoint yang Sudah Bisa Dipakai

| Endpoint | Fungsi | Status |
|---|---|---|
| `POST /api/v1/checkin/device` | Check-in RFID/QR (device tetap seperti gerbang/lab) | ✅ Teruji manual (kiosk, curl) **+ data sekolah asli** (322 siswa/guru SMA Negeri 1 Surya Nusantara, lihat `docs/kesiapan-testing-data-asli.md`) |
| `GET /api/v1/checkin/device/jobs/{job_id}` | Cek hasil face recognition (polling) | ⚠️ Endpoint ada, tapi worker pengenalan wajahnya belum ada — job akan selalu "processing" selamanya |
| `POST /api/v1/devices/heartbeat` | Device lapor "masih hidup" | ✅ Sudah dibangun, belum ada yang manggil beneran dari device fisik |
| `POST /api/v1/checkin/teacher` | Check-in guru via HP (GPS + jaringan sekolah) | ⚠️ Kode selesai & lolos test, **BELUM PERNAH dites end-to-end** — butuh token JWT asli dari Laravel yang sampai sekarang belum ada (lihat Bagian 2.1) |
| `GET /api/v1/attendance/daily` | Cek status absen harian 1 orang | ⚠️ Sama seperti di atas, butuh JWT guru buat dites |
| `POST /api/v1/enrollment/credentials` | Admin daftarkan kartu RFID/QR baru | ⚠️ Butuh JWT dengan role admin buat dites |
| `GET /api/v1/media/photo/{person_id}` | Proxy + cache foto profil dari Laravel, untuk `<img src>` | ✅ Kode selesai, lolos unit test (cache + fetch). **BELUM dites end-to-end pakai Laravel beneran** — tunggu konfirmasi endpoint sync people (baris di bawah) beneran hidup |

**Yang jalan otomatis di background** (bukan endpoint, tapi proses berkala):
| Proses | Fungsi | Status |
|---|---|---|
| Agregasi harian | `attendance_events` (log mentah) → `attendance_daily` (rekap per orang per hari), termasuk deteksi status Terlambat | ✅ Aktif otomatis, sempat ada bug SQL yang baru diperbaiki — perlu 1 kali lagi tes konfirmasi |
| Sync dari Laravel | Isi otomatis `schools_ref`/`people_ref`/`schedules_ref` | ✅ **TERKONFIRMASI JALAN** (10 Sept 2026) — dugaan (b) di riwayat sebelumnya benar: `LARAVEL_SYNC_URL` salah format (path dobel). Setelah dibenerin, `sync schools` & `sync schedules` sukses konek ke Laravel beneran. `sync people` sukses SEBAGIAN — lihat bug ditemukan+diperbaiki di bawah. |

**Kesimpulan Bagian 1:** RFID/QR check-in itu **satu-satunya jalur yang
udah beneran teruji end-to-end**. Semua yang butuh token JWT dari
Laravel (check-in guru, cek status harian, enrollment admin) **kodenya
sudah jadi tapi belum pernah dites sama sekali** — bukan karena ada
bug yang diketahui, tapi karena belum ada cara menghasilkan token JWT
yang valid untuk mengetesnya. Itu PR nomor 1 di Laravel.

---

## Bagian 2: Yang Harus Dibuat di Laravel

Diurutkan dari yang paling menghambat.

### 2.1 WAJIB PALING PENTING — Penerbitan JWT saat guru login

Tanpa ini, **3 endpoint** (`checkin/teacher`, `attendance/daily`,
`enrollment/credentials`) sama sekali tidak bisa dites atau dipakai —
bukan cuma "belum optimal", tapi benar-benar tidak bisa dipanggil sama
sekali (selalu 401 Unauthorized).

**Yang harus dibuat:** endpoint/proses di Laravel yang, setelah guru
login berhasil, menerbitkan JWT dengan struktur PERSIS seperti ini:

```json
{
  "user_id": "<uuid guru, sama dengan people_ref.person_id nanti>",
  "school_id": "<uuid sekolah>",
  "role": "teacher",
  "exp": 1785999999,
  "iat": 1785990000
}
```

- Algoritma: **HS256**, secret-nya **HARUS SAMA PERSIS** dengan
  `JWT_SECRET` di `.env` gateway (lihat README bagian Status Keamanan
  poin 8 soal risiko shared-secret ini — sudah diketahui, sengaja belum
  diperbaiki sekarang).
- Untuk admin yang mau akses `enrollment/credentials`, `role` harus
  persis string `"admin"` (dicek case-sensitive, exact match, lihat
  `internal/middleware/auth.go` fungsi `RequireRole`).
- `user_id` inilah yang dipakai gateway sebagai `person_id` saat
  mencatat event `attendance_events` untuk guru — HARUS konsisten
  dengan `person_id` yang nanti disinkron lewat `people_ref` (Bagian
  2.2), kalau tidak, nama guru tidak akan muncul benar di data.

**Cara tes cepat setelah dibuat:** minta 1 token JWT contoh (bisa lewat
Laravel Tinker atau endpoint login beneran), lalu:
```bash
curl -X POST http://localhost:8080/api/v1/checkin/teacher \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"event_type":"check_in","latitude":-7.257472,"longitude":112.752090}'
```
Kalau responsnya bukan `401`, berarti token-nya diterima gateway.

### 2.2 Sinkronisasi Data — `schools_ref`/`people_ref`/`schedules_ref`

Kontraknya **sudah lengkap ditulis** di
[`docs/laravel-sync-contract.md`](laravel-sync-contract.md) — 3
endpoint (`/api/internal/sync/schools`, `/people`, `/schedules`),
format field, autentikasi (`X-Sync-Token`), contoh kerangka kode
Laravel. Tinggal diimplementasi sesuai skema Eduzone yang sebenarnya.

Tanpa ini, `people_ref`/`schools_ref`/`schedules_ref` di gateway harus
terus diisi manual — nama siswa/guru, jam masuk sekolah
(`late_cutoff_time`), dan jadwal pelajaran tidak akan pernah
ter-update otomatis dari data asli Eduzone.

### 2.2b Proxy & Cache Foto — SUDAH diimplementasi di sisi Gateway (9 Sept 2026)

Tim Laravel mengirim spesifikasi
(`docs/spesifikasi-proxy-foto-absensi-gateway.md`) yang menjelaskan
`photo_url` dari sync `people` cuma bisa diakses dari jaringan Docker
internal — gateway HARUS proxy + cache foto itu sendiri, bukan
meneruskan URL Laravel mentah ke browser/kiosk.

**Sudah dikerjakan di sisi Go:** endpoint baru
`GET /api/v1/media/photo/{person_id}`, cache disk (invalidasi otomatis
lewat `people_ref.synced_at`, bukan waktu kedaluwarsa tetap), fallback
avatar inisial kalau belum ada foto atau fetch ke Laravel gagal. Lolos
unit test (`internal/media`).

**Belum bisa dites end-to-end** — bergantung pada endpoint sync
`people` beneran hidup & terisi `photo_url` (lihat status "STATUS
BELUM JELAS" di atas). Begitu itu terkonfirmasi jalan, tinggal buka
`http://localhost:8080/api/v1/media/photo/{salah-satu-person_id-asli}`
di browser buat tes visual.

**Keputusan desain yang perlu diketahui tim Laravel juga:** endpoint
ini SENGAJA tanpa autentikasi (keterbatasan teknis `<img src>` tidak
bisa kirim header custom) — keamanannya mengandalkan `person_id`
berbentuk UUID + jaringan yang sudah dibatasi NPM. Ini foto anak di
bawah umur, jadi kalau tim Laravel/sekolah menganggap ini perlu
proteksi lebih (mis. token sementara di URL), perlu didiskusikan lagi
sebelum production beneran — bukan sesuatu yang gateway putuskan
sepihak permanen.

### 2.2c Bug Ditemukan & Diperbaiki: Sync `people` Rentan Macet Total (10 Sept 2026)

Begitu `LARAVEL_SYNC_URL` dibenerin dan sync beneran konek ke Laravel,
ketahuan bug baru: **1 orang dengan `school_id` yang belum ada di
`schools_ref` gateway bikin SELURUH batch sync `people` gagal**, dan
karena watermark tidak maju, siklus berikutnya mengulang batch yang
SAMA, gagal lagi di orang yang SAMA, **tanpa henti** — 1 data
bermasalah bisa memblokir sinkronisasi semua orang lain selamanya.

**Sudah diperbaiki di sisi gateway** — sekarang tiap orang diproses
independen; yang gagal dicatat & dilewati di log (`sync people: record
<id> DILEWATI (gagal disimpan): ...`), yang lain tetap tersimpan.
Watermark tetap maju, jadi begitu data sumbernya diperbaiki di
Laravel, record itu otomatis ke-sync lagi di siklus berikutnya —
tanpa perlu campur tangan manual di gateway.

**Perlu diteruskan ke tim Laravel:** akar masalahnya ada 1 sekolah
("sekolah kedua" di data testing) yang datanya belum lengkap — tidak
punya `latitude`/`longitude`, jadi kemungkinan sengaja tidak dikirim
di endpoint `/api/internal/sync/schools` (schools sync melapor
"sukses, 0 record" untuk sekolah ini), TAPI orang-orang yang terdaftar
di sekolah itu tetap dikirim lewat `/api/internal/sync/people`. Ini
menciptakan data yang "yatim" (orang ada, sekolahnya tidak) dari sudut
pandang gateway. Idealnya salah satu dari ini dilakukan di sisi
Laravel: (a) lengkapi data sekolah itu (isi koordinat GPS-nya), atau
(b) jangan sertakan orang-orang dari sekolah yang datanya belum
lengkap di endpoint sync `people`, sampai sekolahnya sendiri lengkap
dan berhasil disinkronkan.

### 2.3 Belum mendesak, tapi diperlukan untuk fitur lengkap nanti

- **Sync balik** — hasil absen (`attendance_daily`) belum pernah
  dikirim balik ke database utama Eduzone
  (`student_attendance`/`teacher_attendance`/dst). Sampai ini dibuat,
  data absen HANYA ada di database `eduzone_absensi`, tidak akan
  muncul di dashboard/laporan Eduzone yang sudah ada. Belum ada
  kontraknya sama sekali (beda dari 2.2 yang kontraknya sudah siap).
- **UI admin buat manggil `enrollment/credentials`** — form di panel
  admin Eduzone buat daftarin kartu RFID/QR baru per siswa/guru
  (manggil endpoint gateway ini dari sisi Laravel/frontend).
- **Worker Python untuk Face Recognition** — ini bukan Laravel, tapi
  service terpisah lagi (di luar cakupan dokumen ini).

---

## Ringkasan Prioritas

1. **JWT issuance** (Bagian 2.1) — tanpa ini, 3 dari 6 endpoint gateway
   tidak bisa dipakai/dites sama sekali. Paling menghambat, MASIH
   BELUM SELESAI.
2. ~~**3 endpoint sync** (Bagian 2.2)~~ — ✅ **SUDAH JALAN** (10 Sept
   2026), termasuk bug ketahanan sync yang ditemukan & diperbaiki
   (2.2c). Tersisa 1 data quality issue di sisi Laravel yang perlu
   diteruskan (lihat 2.2c).
3. **Proxy foto** (2.2b) — kode gateway selesai, tunggu data
   `photo_url` beneran mengalir lewat sync `people` yang sekarang
   sudah jalan, lalu tes end-to-end.
4. Sisanya (2.3) bisa menyusul setelah di atas selesai.
