---
name: absensi-gateway-dev
description: "Panduan kerja untuk membantu development Absensi Gateway — microservice Go (net/http standar, tanpa framework) + PostgreSQL yang jadi pintu masuk semua metode absensi EduZone (RFID, QR, Face recognition, self check-in guru via GPS). Gunakan skill ini setiap kali mengerjakan kode di repo ini: menambah/mengubah handler, middleware, migration schema, atau apa pun yang menyentuh folder internal/, main.go, atau absensi_schema.sql. Wajib dipakai sebelum menambah endpoint baru atau menyentuh autentikasi (X-Device-Key / JWT) karena ada pola keamanan (rate limit, body size limit, transaksi+lock) yang gampang terlewat atau diregresi."
---

# Absensi Gateway — Panduan Development

Microservice Go yang menangani semua jalur absensi EduZone (RFID, QR,
Face, self check-in guru), terpisah dari monolith Laravel EduZone
karena diperkirakan jadi endpoint dengan trafik terpadat. Database
sendiri (`eduzone_absensi`), terpisah dari database utama EduZone —
data siswa/guru/sekolah/jadwal diakses lewat cache lokal
(`people_ref`/`schools_ref`/`schedules_ref`), bukan join langsung.

Stack: Go 1.22+ (`net/http` standar, `ServeMux` pattern routing —
BUKAN Gin/Echo/Fiber), `database/sql` + `github.com/lib/pq`,
`github.com/golang-jwt/jwt/v5`, PostgreSQL. Tidak ada ORM — semua
query SQL ditulis manual.

Baca `README.md` di root project untuk dokumentasi arsitektur, daftar
endpoint, dan status keamanan lengkap. Skill ini merangkum aturan &
pola yang paling gampang dilanggar atau terlewat saat menambah kode.

## Struktur Wajib Dipahami Sebelum Menambah Kode

```
main.go                    — SEMUA route didaftarkan di sini via mux.Handle(),
                              dibungkus middleware pakai helper chain(...)
internal/config/           — baca env var, fail-fast kalau ada yang kosong
internal/db/                — buka koneksi Postgres, Ping saat startup
internal/middleware/        — auth (device key & JWT), rate limit, body size limit, ClientIP
internal/handlers/          — 1 file per grup endpoint (checkin_device, checkin_teacher, dst)
internal/geofence/          — hitung jarak GPS (Haversine)
internal/scheduling/         — resolve jadwal pelajaran aktif
internal/sync/               — pull berkala schools_ref/people_ref/schedules_ref dari Laravel
internal/aggregation/        — agregasi berkala attendance_events -> attendance_daily
```

Setiap handler baru WAJIB didaftarkan di `main.go` lewat
`mux.Handle("METHOD /path", chain(middleware...)(http.HandlerFunc(...)))`
— jangan bikin router/mux baru di file lain.

## Agregasi Absen Harian (`internal/aggregation`)

Beda dari `internal/sync` (bergantung Laravel hidup), agregasi ini murni
baca-tulis ke database gateway sendiri — AKTIF secara default
(`AGGREGATION_ENABLED=true`), bukan opt-in seperti sync. Kalau menambah
kolom baru ke `attendance_daily` yang perlu dihitung dari
`attendance_events`:

1. Update `aggregationQuery` di `internal/aggregation/aggregation.go` —
   ingat query ini WAJIB tetap idempotent (aman dijalankan ulang untuk
   rentang tanggal yang sama, hasilnya selalu FULL RECOMPUTE dari
   `attendance_events`, bukan increment).
2. Kalau kolom barunya butuh data dari `schools_ref` (seperti
   `late_cutoff_time` untuk status Terlambat), JOIN ke `schools_ref` dan
   masukkan kolom itu ke `GROUP BY` juga (Postgres tidak otomatis
   mengenali functional dependency lintas tabel via JOIN).
3. **JANGAN** membuat agregasi ini menentukan status yang butuh data di
   luar `attendance_events`/`schools_ref` (mis. Sakit/Izin/Alpa yang
   butuh data surat izin) — itu keputusan Laravel, bukan gateway. Kalau
   ada kebutuhan begitu, itu masuk ke proses "sync balik ke Laravel"
   (masih stub), bukan ditambahkan ke sini.

## Sinkronisasi Data dari Laravel (`internal/sync`)

Gateway ini JEMPUT data dari Laravel secara berkala (pull, bukan push)
— lihat `docs/laravel-sync-contract.md` untuk kontrak endpoint yang
harus disediakan Laravel. Kalau menambah tabel `*_ref` baru yang juga
perlu disinkron dari Laravel:

1. Tambah struct record baru di `internal/sync/types.go`, field JSON-nya
   HARUS sama persis dengan nama kolom tabel `_ref` tujuan.
2. Tambah `Fetch<Resource>` di `client.go` (copy pola yang sudah ada,
   pakai helper pagination yang sama).
3. Tambah `Upsert<Resource>` di `upsert.go` — WAJIB pakai
   `ON CONFLICT ... DO UPDATE`, WAJIB dibungkus 1 transaksi.
4. Tambah `pull<Resource>` di `puller.go`, panggil dari `pullAll` — kalau
   tabel barunya punya foreign key ke `schools_ref`, taruh urutan
   panggilannya SETELAH `pullSchools`.
5. Tambah baris baru ke `ref_sync_state` (lewat migration/schema, resource
   name baru) supaya watermark-nya tercatat terpisah.
6. **Update `docs/laravel-sync-contract.md`** — ini kontrak yang harus
   sinkron dengan sisi Laravel, jangan sampai berubah di sini tanpa
   dokumentasinya ikut diupdate.

Prinsip yang HARUS dipertahankan: kegagalan sync (Laravel down, dll)
TIDAK PERNAH boleh membuat gateway berhenti atau menolak check-in — cuma
di-log dan dicoba lagi siklus berikutnya (lihat `recordFailure` di
`puller.go` sebagai contoh pola-nya).

## Aturan Wajib — Menambah Endpoint Baru

1. **Body size limit selalu dipasang eksplisit.** Setiap route baru di
   `main.go` HARUS dibungkus `middleware.MaxBodySize(n)` sebagai
   middleware PALING LUAR. Pilih ukurannya dari konstanta yang sudah
   ada di `internal/middleware/bodylimit.go` (`SizeSmall` 4 KB untuk
   body berisi field pendek, `SizeImage` 6 MB kalau membawa 1 foto,
   `SizeEnrollment` 10 MB kalau bisa membawa beberapa foto) — jangan
   biarkan endpoint tanpa batas, itu celah DoS.
2. **Decode body pakai `handlers.decodeJSONBody`**, bukan
   `json.NewDecoder(r.Body).Decode(...)` langsung — supaya body yang
   kelewat besar dapat respons 413 yang jelas, bukan disamakan dengan
   body rusak format (400).
3. **IP klien selalu lewat `middleware.ClientIP(r)`**, jangan baca
   `r.Header.Get("X-Forwarded-For")` langsung di handler manapun.
   Fungsi ini sudah menangani asumsi topologi (1 reverse proxy
   tepercaya di depan gateway, prioritas `X-Real-IP` lalu elemen
   TERAKHIR `X-Forwarded-For`) — jangan duplikasi logic ini di tempat
   lain, supaya tidak diam-diam berbeda antar endpoint.
4. **Endpoint dengan auth berbasis "kunci rahasia yang bisa ditebak"**
   (device key, API key, dll) harus dipasangi rate limit percobaan
   gagal seperti pola di `internal/middleware/attempt_tracker.go`
   (`DeviceKeyAuth`) — jangan biarkan endpoint auth baru tanpa
   pembatas brute-force.
5. **Operasi "cek dulu baru insert/update" yang bisa dipanggil hampir
   bersamaan** (mis. deteksi duplikat) WAJIB dibungkus 1 transaksi +
   `pg_advisory_xact_lock(hashtext(...))` per kunci yang relevan
   (contoh: `handleSyncCheckin` di `checkin_device.go`) — dua query
   terpisah tanpa lock punya race condition.
6. **Data yang dikembalikan lewat endpoint "polling status by ID"**
   (job async, dll) HARUS dicatat kepemilikannya (device/user mana yang
   membuat) dan divalidasi ulang saat diambil — contoh pola di
   `FaceJobResult`/`GetFaceJobResult`. Kalau tidak cocok, balikin error
   yang SAMA dengan "tidak ditemukan" (jangan bedakan 403 vs 404),
   supaya tidak bocor info validitas ID ke pihak yang mencoba menebak.
7. **Input angka yang punya rentang fisik wajar (GPS, dll) divalidasi
   rentangnya** sebelum dipakai kalkulasi lebih lanjut — pola di
   `isValidCoordinate`.
8. **`attendance_events` itu insert-only** — jangan pernah menulis
   kode yang UPDATE/DELETE baris di tabel ini, termasuk saat menambah
   fitur koreksi/pembatalan (pakai tabel `attendance_correction_log`
   yang memang disiapkan untuk itu, bukan mengubah event asli).

## Autentikasi

- **Device (RFID/QR/Face/heartbeat):** header `X-Device-Key`, di-hash
  SHA-256, dicocokkan ke `devices.api_key_hash` lewat
  `middleware.DeviceKeyAuth`. Context yang tersedia di handler:
  `middleware.CtxDeviceID`, `middleware.CtxSchoolID`,
  `middleware.CtxDeviceClassID` (opsional, kalau device terikat kelas).
- **Guru/admin:** JWT Bearer token yang diterbitkan Laravel EduZone,
  divalidasi `middleware.JWTAuth` pakai `JWT_SECRET` (HS256, shared
  secret dengan Laravel — lihat catatan risiko soal ini di README
  bagian Status Keamanan sebelum mengubah apapun terkait JWT).
  Context: `middleware.CtxSchoolID`, role dicek lewat middleware
  `adminOnly` untuk endpoint khusus admin.

## Timezone — Wajib Diperhatikan

`internal/scheduling` memakai `time.Now()` yang ikut timezone sistem.
Selalu pastikan `TZ=Asia/Jakarta` di-set di environment (Dockerfile
atau docker-compose), dan kalau menulis seed/test data jadwal, `SET
timezone = 'Asia/Jakarta'` juga di sisi SQL — ketidakcocokan timezone
antara Go dan Postgres bikin pencocokan jadwal aktif salah tanpa error
yang jelas (bukan crash, cuma "jadwal kelihatannya nggak aktif padahal
seharusnya aktif").

## Alur Menambah Fitur Baru

1. Kalau butuh tabel baru → tambahkan ke `absensi_schema.sql`
   (skema ada di root project, bukan folder migration terpisah —
   project ini belum pakai migration tool)
2. Handler baru → file baru di `internal/handlers/`, constructor
   `New<Nama>Handler(db *sql.DB) *<Nama>Handler`
3. Daftarkan route di `main.go` (lihat Aturan Wajib poin 1)
4. Kalau endpoint menyentuh logic keamanan (auth baru, body besar,
   data sensitif per-pemilik) → cek Aturan Wajib di atas dulu SEBELUM
   nulis kode, bukan sesudah
5. Tulis unit test untuk logic yang bisa ditest tanpa Postgres (lihat
   pola di `internal/middleware/*_test.go` dan
   `internal/handlers/checkin_device_test.go`) — tidak perlu database
   asli untuk test logic murni (validasi, rate limit, ownership check)
6. `go build ./... && go vet ./... && go test ./...` sebelum
   dianggap selesai — kalau ada test yang butuh Postgres beneran,
   jalankan smoke test manual lewat `docker compose up --build`
   (lihat README bagian Testing)

## Konvensi Penamaan

| Tipe | Konvensi | Contoh |
|---|---|---|
| Package | lowercase pendek | `handlers`, `middleware`, `geofence` |
| Handler struct | PascalCase + `Handler` | `DeviceHandler`, `AttendanceHandler` |
| Constructor | `New<Nama>Handler` | `NewEnrollmentHandler(db)` |
| Context key | `Ctx<Nama>` di package middleware | `CtxDeviceID`, `CtxSchoolID` |
| Kolom boolean | prefix `is_`/`has_` | `is_active`, `has_anomaly` |
| Kode alasan error (`reason` di response) | snake_case, deskriptif | `invalid_device_key`, `body_too_large` |

## Yang Sering Jadi Celah Kalau Terlewat (riwayat dari review keamanan)

Daftar ini diambil dari sesi review keamanan sebelumnya — dicatat di
sini supaya pola yang sama tidak terulang saat menambah kode baru:

- Endpoint baru tanpa `middleware.MaxBodySize` → bisa DoS lewat body
  raksasa.
- Baca IP klien manual dari header alih-alih `middleware.ClientIP` →
  gampang salah asumsi soal `X-Forwarded-For` (elemen pertama vs
  terakhir) dan jadi celah spoofing IP.
- Endpoint auth baru tanpa rate limit percobaan gagal → brute force
  terbuka.
- "Cek existing lalu insert" tanpa transaksi+lock → race condition,
  terutama untuk deteksi duplikat/anomali yang dipanggil device
  secara paralel.
- Data per-pemilik (job, hasil proses async, dll) yang cuma dicek
  "auth-nya valid" tanpa cek "ini punya siapa" → data leak antar
  device/user yang sama-sama valid auth-nya tapi beda kepemilikan.
- Field numerik dari klien (koordinat, dll) dipakai langsung tanpa
  cek rentang wajar → data sampah masuk database, meski bukan celah
  keamanan langsung.

Detail lengkap tiap poin (kenapa berbahaya, bagaimana fix-nya
diimplementasikan) ada di README.md bagian "Status Keamanan".
