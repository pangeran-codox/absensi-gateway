# Absensi Gateway

Service Go yang jadi pintu masuk semua metode absensi Eduzone: RFID, QR,
Face recognition (device tetap di sekolah), dan self check-in guru via
web/PWA. Berjalan sebagai microservice terpisah dari aplikasi utama
Eduzone (Laravel), khusus menangani jalur absensi yang butuh throughput
tinggi & latency rendah.

Lihat `api_contract.md` untuk detail kontrak request/response tiap
endpoint, dan `absensi_schema.sql` untuk skema database lengkap.

## Kenapa service terpisah dari Eduzone (Laravel)

Absensi diperkirakan jadi endpoint dengan trafik paling padat (ratusan
device RFID/QR/Face nge-hit bersamaan pas jam masuk sekolah), jadi
dipisah jadi microservice Go + database sendiri (`eduzone_absensi`),
bukan menumpang di monolith Laravel + database utama. Laravel tetap jadi
sumber kebenaran untuk data siswa/guru/sekolah/jadwal — data itu
disinkronkan **satu arah** ke cache lokal (`people_ref`, `schools_ref`,
`schedules_ref`) di database gateway ini, bukan diakses lewat join
langsung ke database utama.

## Arsitektur & Alur Data

```
┌─────────────┐        ┌──────────────────┐        ┌─────────────────┐
│  Device RFID│──HTTP─▶│                  │        │                 │
│  Device QR  │        │  Absensi Gateway │──SQL──▶│  eduzone_absensi│
│  Device Face│        │      (Go)        │        │   (PostgreSQL)  │
└─────────────┘        │                  │        └─────────────────┘
┌─────────────┐        │                  │
│ Guru (PWA)  │──HTTP─▶│                  │
└─────────────┘        └──────────────────┘
                                 ▲
                                 │ JWT (shared secret HS256)
                                 │
                        ┌──────────────────┐        ┌─────────────────┐
                        │  Eduzone (Laravel)│──SQL──▶│  DB utama Eduzone│
                        └──────────────────┘        └─────────────────┘
```

Semua request (device maupun guru) masuk lewat **Nginx Proxy Manager**
sebelum sampai ke gateway ini — gateway TIDAK boleh expose port
langsung ke internet (lihat bagian Keamanan, poin soal `X-Real-IP`).

- **Device (RFID/QR/Face)** diautentikasi pakai header `X-Device-Key`
  (di-hash SHA-256, dicocokkan ke kolom `devices.api_key_hash`).
- **Guru** diautentikasi pakai JWT yang diterbitkan Laravel (Eduzone),
  divalidasi gateway ini pakai secret yang sama (HS256) — lihat catatan
  risiko di bagian Keamanan.
- `attendance_events` adalah tabel **insert-only** (append log mentah).
  Agregasi ke `attendance_daily`/`attendance_period` dan sinkron balik ke
  database utama Eduzone (`student_attendance`, `teacher_attendance`,
  dst) sengaja **belum dibuat di gateway ini** — direncanakan jadi
  worker/job terpisah yang baca `attendance_events` secara berkala,
  progresnya dicatat di `sync_log`.

## Struktur Project

```
main.go                          — wiring routing + middleware chain
internal/
  config/config.go               — baca env var (LISTEN_ADDR, DATABASE_URL, JWT_SECRET)
  db/db.go                       — buka koneksi Postgres
  middleware/
    auth.go                      — DeviceKeyAuth (X-Device-Key) & JWTAuth (Bearer token guru)
    attempt_tracker.go           — rate limit percobaan gagal X-Device-Key per IP
    clientip.go                  — ekstraksi IP asli klien di balik NPM (dipakai auth.go & checkin_teacher.go)
    bodylimit.go                 — batas ukuran body request per jenis endpoint
  handlers/
    checkin_device.go            — POST /checkin/device (RFID/QR sinkron, Face async) + polling job
    checkin_teacher.go           — POST /checkin/teacher (GPS radius + whitelist jaringan sekolah)
    enrollment.go                — POST /enrollment/credentials (admin, daftar kredensial RFID/QR)
    attendance.go                — GET /attendance/daily (query status agregat)
    device_ops.go                — POST /devices/heartbeat
    response.go                  — helper writeJSON/writeError/decodeJSONBody/mustJSON/generateJobID
  geofence/geofence.go           — hitung jarak GPS (Haversine) untuk validasi radius sekolah
  scheduling/scheduling.go       — resolve jadwal pelajaran aktif untuk device per-kelas
```

## Endpoint

| Method & Path | Auth | Keterangan |
|---|---|---|
| `POST /api/v1/checkin/device` | `X-Device-Key` | RFID/QR (sinkron) atau Face (async, kembalikan `job_id`) |
| `GET /api/v1/checkin/device/jobs/{job_id}` | `X-Device-Key` | Polling hasil face recognition — **hanya device pembuat job** yang bisa akses |
| `POST /api/v1/devices/heartbeat` | `X-Device-Key` | Device lapor "masih hidup" (update `last_seen_at`) |
| `POST /api/v1/checkin/teacher` | JWT (Bearer) | Check-in guru — validasi GPS radius + IP jaringan sekolah |
| `GET /api/v1/attendance/daily` | JWT (Bearer) | Query status absensi harian per `person_id` |
| `POST /api/v1/enrollment/credentials` | JWT (Bearer, role admin) | Daftarkan kredensial RFID/QR baru (Face: belum aktif) |

## Status Keamanan

Bagian ini isinya potensi celah yang sudah ditemukan & statusnya —
supaya siapapun yang lanjut kerjakan project ini (termasuk sesi Claude
berikutnya) tahu apa yang sudah aman dan apa yang masih jadi risiko
sadar (bukan kelupaan).

### Sudah diperbaiki

1. **Spoofing IP lewat `X-Forwarded-For`** — `middleware.ClientIP`
   mengutamakan header `X-Real-IP` (ditimpa NPM, bukan diteruskan
   mentah dari klien), fallback ke elemen **terakhir** `X-Forwarded-For`
   (bukan pertama). Dipakai untuk validasi jaringan sekolah di check-in
   guru. **Asumsi:** gateway ini hanya bisa diakses lewat 1 reverse
   proxy tepercaya (NPM) — kalau topologi berubah (nambah proxy lagi di
   depan NPM), logic ini perlu ditinjau ulang.
2. **Body request tanpa batas ukuran (potensi DoS)** —
   `middleware.MaxBodySize` dipasang di semua route dengan ukuran sesuai
   payload wajar tiap endpoint (`SizeSmall` 4 KB, `SizeImage` 6 MB,
   `SizeEnrollment` 10 MB). `handlers.decodeJSONBody` membedakan body
   kelewat besar (413) vs body rusak format (400).
3. **Job face-recognition bisa diintip device lain** —
   `FaceJobResult` sekarang menyimpan `deviceID` pemiliknya (field
   unexported, tidak ikut ke-JSON-encode). `GetFaceJobResult` menolak
   (404, disamakan dengan "job tidak ada" — sengaja, biar tidak bocor
   info) kalau device yang polling bukan pembuat job.
4. **Brute force `X-Device-Key`** — `middleware.attemptTracker`
   mengunci IP yang gagal 10x dalam 5 menit selama 15 menit (kode 429 +
   header `Retry-After`). Percobaan berhasil me-reset hitungan; error
   internal (DB down, dll) tidak dihitung sebagai percobaan gagal.
5. **Race condition duplicate-scan check** — cek anomali + insert event
   di `checkin_device.go` dibungkus 1 transaksi + `pg_advisory_xact_lock`
   per `(person_id, method)`, supaya 2 scan nyaris bersamaan tidak
   lolos berdua dari deteksi `duplicate_scan_within_5s`.
6. **`flagged_reason` tidak konsisten tersimpan** — check-in device kini
   ikut menyimpan alasan anomali ke kolom `flagged_reason`, sama seperti
   check-in guru (sebelumnya cuma ada di response, hilang dari DB).
7. **Koordinat GPS tidak divalidasi** — `isValidCoordinate` menolak
   (400) latitude di luar -90..90 atau longitude di luar -180..180,
   sebelum dipakai hitung jarak.

### Diketahui, sengaja belum diperbaiki (butuh keputusan/kerja di sisi Laravel juga)

8. **JWT pakai shared secret (HS256) antara Laravel & gateway ini.**
   Laravel & gateway sama-sama pegang `JWT_SECRET` yang identik — kalau
   secret ini bocor dari sisi gateway (yang permukaan serangnya lebih
   besar karena diakses device & guru dari luar), penyerang bisa
   menerbitkan token palsu untuk **seluruh sistem Eduzone**, bukan cuma
   modul absensi. Solusi standarnya: pindah ke RS256 (asymmetric) —
   Laravel pegang private key (buat menerbitkan token), gateway ini
   cuma pegang public key (buat verifikasi saja, tidak bisa dipakai
   menerbitkan token palsu). **Ini butuh perubahan di sisi Laravel
   (generate keypair, ganti cara nerbitin JWT) yang di luar cakupan
   repo ini** — didokumentasikan di sini supaya tidak terlupakan, bukan
   diselesaikan diam-diam.

### Batasan yang perlu diketahui (bukan bug, tapi trade-off desain)

- `middleware.attemptTracker` dan `DeviceHandler.jobStore` disimpan
  **in-memory** (map + mutex) — cukup untuk 1 instance gateway. Kalau
  nanti di-scale ke banyak instance/replika, keduanya perlu dipindah ke
  penyimpanan bersama (mis. Redis) supaya rate-limit & job-store
  konsisten di semua instance.
- `detectAnomalies` (deteksi duplicate scan) tidak scoping eksplisit ke
  `school_id` — mengandalkan `person_id` unik lintas sekolah. Aman
  selama asumsi itu benar, tapi bukan defense-in-depth penuh.

## Konfigurasi (Environment Variable)

| Variable | Wajib | Contoh | Keterangan |
|---|---|---|---|
| `LISTEN_ADDR` | tidak (default `:8080`) | `:8080` | Alamat & port HTTP server |
| `DATABASE_URL` | ya | `postgres://user:pass@host:5432/eduzone_absensi?sslmode=disable` | Koneksi ke database `eduzone_absensi` |
| `JWT_SECRET` | ya | — | Secret HS256, **harus sama** dengan yang dipakai Laravel menerbitkan token guru (lihat catatan risiko di atas) |
| `TZ` | sangat disarankan | `Asia/Jakarta` | Lihat peringatan timezone di bawah |

Saat dijalankan lewat `docker compose` (lihat bagian Testing di bawah),
`DATABASE_URL` disusun otomatis dari `POSTGRES_USER`/`POSTGRES_PASSWORD`
di file `.env` (disalin dari `.env.example`, TIDAK ke-commit ke git) —
bukan ditulis manual di `docker-compose.yml`.

## Peringatan Timezone

`scheduling.ResolveActiveSchedule` memakai `time.Now()` yang mengikuti
timezone sistem tempat container/binary berjalan. Pastikan container
di-deploy dengan `TZ=Asia/Jakarta` (atau timezone sekolah yang
bersangkutan) — kalau tidak, pencocokan jadwal jam pelajaran bisa salah
beberapa jam.

```yaml
environment:
  - TZ=Asia/Jakarta
```

`docker/initdb/02_seed.sql` sengaja `SET timezone = 'Asia/Jakarta'` di
baris pertama sebelum menghitung jadwal dummy — konsistensi timezone ini
harus dijaga di production juga. Binary gateway ini juga meng-embed
database zoneinfo IANA sendiri (`import _ "time/tzdata"` di `main.go`),
jadi `TZ=Asia/Jakarta` tetap valid meski base image Docker diganti ke
image yang lebih minimal.

## Status Implementasi Fitur

Sudah jalan (dibangun & lolos `go build`/`go vet`/`go test` — belum
tentu berarti sudah dites end-to-end dengan Postgres beneran):

- Check-in RFID & QR sinkron (hash-based matching)
- Check-in Face — endpoint & job-polling ada, **tapi worker
  pengenalan wajahnya sendiri belum ada** (lihat bagian "Masih stub")
- Check-in guru (GPS radius + whitelist jaringan sekolah)
- Enrollment kredensial RFID/QR (Face: ditolak eksplisit, belum aktif)
- Query status absensi harian
- Heartbeat device
- Absen per jam pelajaran — device dengan `default_class_id` terisi
  (mis. RFID reader tetap di Lab Komputer) otomatis mendeteksi jadwal
  aktif (`schedules_ref`) dan menandai event dengan `schedule_id`.
  Device umum (mis. gerbang) tidak terpengaruh, tetap absen harian biasa.

**Masih stub/belum diimplementasikan (sengaja, menunggu komponen lain siap):**

- Face recognition: endpoint sudah ada, tapi belum terhubung ke worker
  Python (InsightFace + liveness check). `jobStore` saat ini in-memory
  placeholder — ganti dengan Redis Streams atau queue lain sebelum
  dipakai produksi.
- Local Presence Verifier (solusi CGNAT IndiHome): tabel
  `local_verifiers` & `presence_tickets` sudah ada di skema, tapi
  gateway ini belum memverifikasi `presence_ticket` — jaringan dengan
  `requires_local_verifier = true` untuk sementara selalu dianggap
  `network_recognized = false`.
- Hash chaining, device signing (Ed25519), QR token rotating,
  correction log approval flow — semua kolom/tabel sudah disiapkan di
  skema, logikanya belum diaktifkan di gateway ini.
- **Agregasi ke `attendance_daily` & `attendance_period`** — gateway ini
  cuma menulis raw event ke `attendance_events`. Proses agregasi
  harian & per-jam-pelajaran, plus sync ke
  `student_attendance`/`teacher_attendance`/`student_subject_attendance`/
  `teaching_attendance` di DB utama, sengaja BELUM dibuat di sini.
- `schedules_ref` & `people_ref` perlu job sync berkala dari DB utama
  Eduzone — belum dibuat, jadi tabel ini masih harus diisi manual untuk
  testing.
- **JWT RS256** — lihat bagian Status Keamanan poin 8.

## Testing

Test unit (tidak butuh Postgres — jalan cepat, cek logic murni):

```bash
go build ./...
go vet ./...
go test ./... -v
```

Cakupan test saat ini:
- `internal/middleware`: `MaxBodySize` (body oversized ditolak),
  `attemptTracker` (lockout brute-force device key)
- `internal/handlers`: kepemilikan job face-recognition
  (`GetFaceJobResult`), validasi rentang koordinat GPS
  (`isValidCoordinate`)

Belum ada test yang butuh Postgres beneran (integration test) — semua
alur yang menyentuh database (check-in RFID/QR, enrollment, dll) baru
tervalidasi lewat `go build`/`go vet` (kompilasi & tipe data benar),
BUKAN lewat eksekusi nyata terhadap database. Sebelum deploy ke
production, tetap perlu smoke test manual pakai Docker Compose (lihat
bagian di bawah).

## Testing Lokal Pakai Docker Compose

**Setup ini pakai Postgres INFRASTRUKTUR YANG SUDAH JALAN** (container
`postgres` shared, bukan bikin container Postgres baru khusus project
ini) — sesuai pola yang sama dipakai EduZone & Lab Management.
`docker-compose.yml` di sini cuma berisi service `absensi-gateway`,
digabungkan ke network Docker external `network` (tempat container
`postgres` juga nyambung) lewat hostname `postgres`.

**1. Buat database baru di dalam container Postgres yang sudah ada**
(ganti `<user>` sesuai kredensial Postgres kamu, biasanya ada di `.env`
EduZone):
```powershell
docker exec -it postgres psql -U <user> -c "CREATE DATABASE eduzone_absensi;"
```

**2. Masukkan skema + data dummy** (dari folder `docker/initdb/`,
dijalankan manual karena container Postgres-nya sudah lama jalan —
auto-seed `docker-entrypoint-initdb.d` cuma jalan otomatis saat
container BARU pertama kali dibuat):
```powershell
Get-Content docker\initdb\01_schema.sql | docker exec -i postgres psql -U <user> -d eduzone_absensi
Get-Content docker\initdb\02_seed.sql   | docker exec -i postgres psql -U <user> -d eduzone_absensi
```

**3. Siapkan file `.env`** (sekali saja, isinya kredensial — TIDAK ke-commit ke git):
```powershell
copy .env.example .env
notepad .env   # isi POSTGRES_USER, POSTGRES_PASSWORD (samakan dengan container postgres existing), dan JWT_SECRET
```

**4. Build & jalankan gateway-nya:**
```powershell
docker compose up --build
```
Gateway jalan di `localhost:8080`. Postgres TIDAK dibuka port baru di
compose ini (sudah ada `0.0.0.0:5432` dari container `postgres`
existing kamu).

**Coba check-in RFID di gerbang** (device umum, tanpa `schedule_id`):
```bash
curl -X POST http://localhost:8080/api/v1/checkin/device \
  -H "Content-Type: application/json" \
  -H "X-Device-Key: DEVKEY-GERBANG-01" \
  -d '{"method":"rfid","event_type":"check_in","credential_value":"CARD-ANDI-001"}'
```

**Coba check-in RFID di Lab** (device per-kelas, jadwal seed data selalu
dibuat aktif ±2 jam dari waktu seed dijalankan, jadi HARUS muncul
`schedule_id` di response):
```bash
curl -X POST http://localhost:8080/api/v1/checkin/device \
  -H "Content-Type: application/json" \
  -H "X-Device-Key: DEVKEY-LAB-01" \
  -d '{"method":"rfid","event_type":"check_in","credential_value":"CARD-ANDI-001"}'
```

**Reset data test** (hapus & bikin ulang database di container Postgres
yang sama — BUKAN `docker compose down -v`, karena volume Postgres-nya
bukan milik compose ini):
```powershell
docker exec -it postgres psql -U <user> -c "DROP DATABASE eduzone_absensi;"
docker exec -it postgres psql -U <user> -c "CREATE DATABASE eduzone_absensi;"
Get-Content docker\initdb\01_schema.sql | docker exec -i postgres psql -U <user> -d eduzone_absensi
Get-Content docker\initdb\02_seed.sql   | docker exec -i postgres psql -U <user> -d eduzone_absensi
```

## Menjalankan Secara Lokal (tanpa Docker)

```bash
export DATABASE_URL="postgres://user:pass@localhost:5433/eduzone_absensi?sslmode=disable"
export JWT_SECRET="samakan-dengan-punya-laravel"
export TZ="Asia/Jakarta"
go run .
```

## Build & Jalankan via Docker

```bash
docker build -t iswant/absensi-gateway:0.1.0 .
docker run --env-file .env -p 8080:8080 iswant/absensi-gateway:0.1.0
```

## Deploy ke Docker Swarm

Ikuti pola yang sudah dipakai untuk `iswant/lab-management`:

```bash
docker build -t iswant/absensi-gateway:0.1.0 .
docker push iswant/absensi-gateway:0.1.0

# di node tameng
set -a; source .env; set +a
docker stack deploy -c docker-compose.yml eduzone-absensi
```

Tambahkan service `absensi-gateway` ke stack file yang sama dengan
infrastruktur yang sudah ada (Postgres, Redis, Nginx Proxy Manager),
pakai network overlay yang sama supaya bisa akses `eduzone_absensi`
database. **Gateway ini tidak boleh expose port langsung ke internet**
— harus lewat NPM (lihat asumsi di bagian Status Keamanan poin 1).

## Setup Database

Jalankan `absensi_schema.sql` (folder sebelah) ke database
`eduzone_absensi` yang baru (terpisah dari database utama Eduzone):

```bash
psql -h <host> -U <user> -d eduzone_absensi -f absensi_schema.sql
```
