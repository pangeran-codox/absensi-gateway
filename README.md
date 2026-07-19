# Absensi Gateway

Service Go yang jadi pintu masuk semua metode absensi Eduzone: RFID, QR, Face
recognition (device tetap di sekolah), dan self check-in guru via web/PWA.

Lihat `api_contract.md` (di folder skema) untuk detail kontrak tiap endpoint.

## Status implementasi

Sudah jalan (tervalidasi lewat smoke test manual):
- `POST /api/v1/checkin/device` — RFID & QR (sinkron, hash-based matching)
- `GET /api/v1/checkin/device/jobs/{job_id}` — polling hasil face recognition
- `POST /api/v1/checkin/teacher` — check-in guru (GPS radius + whitelist jaringan sekolah)
- `POST /api/v1/enrollment/credentials` — daftar kredensial RFID/QR (face: stub)
- `GET /api/v1/attendance/daily` — query status absensi
- `POST /api/v1/devices/heartbeat` — device lapor "masih hidup"
- **Absen per jam pelajaran** — device yang `default_class_id`-nya diisi (mis.
  RFID reader tetap di Lab Komputer) otomatis mendeteksi jadwal yang sedang
  aktif (`schedules_ref`) dan menandai event dengan `schedule_id`. Device
  umum (mis. gerbang) tidak terpengaruh, tetap absen harian biasa.

**PENTING — Timezone server:** `scheduling.ResolveActiveSchedule` memakai
`time.Now()` yang mengikuti timezone sistem tempat container/binary
berjalan. Pastikan container di-deploy dengan `TZ=Asia/Jakarta` (atau
timezone sekolah yang bersangkutan), kalau tidak, pencocokan jadwal jam
pelajaran bisa salah beberapa jam. Contoh di docker-compose/stack.yml:
```yaml
environment:
  - TZ=Asia/Jakarta
```

**Masih stub/belum diimplementasikan (sengaja, menunggu komponen lain siap):**
- Face recognition: endpoint sudah ada, tapi belum terhubung ke worker Python
  (InsightFace + liveness check). `jobStore` saat ini in-memory placeholder —
  ganti dengan Redis Streams atau queue lain sebelum dipakai produksi.
- Local Presence Verifier (solusi CGNAT IndiHome): tabel `local_verifiers` &
  `presence_tickets` sudah ada di skema, tapi gateway ini belum memverifikasi
  presence_ticket — jaringan dengan `requires_local_verifier = true` untuk
  sementara selalu dianggap `network_recognized = false`.
- Hash chaining, device signing (Ed25519), QR token rotating, correction log
  approval flow — semua kolom/tabel sudah disiapkan di skema, logikanya belum
  diaktifkan di gateway ini.
- **Agregasi ke `attendance_daily` & `attendance_period`** — gateway ini cuma
  menulis raw event ke `attendance_events` (termasuk `schedule_id` kalau
  device terikat kelas). Proses agregasi harian & per-jam-pelajaran, plus
  sync ke `student_attendance`/`teacher_attendance`/`student_subject_attendance`/
  `teaching_attendance` di DB utama, sengaja BELUM dibuat di sini — rencananya
  jadi worker/job terpisah (lihat `sync_log` di skema) supaya gateway tetap
  ringan & cepat untuk jalur check-in.
- `schedules_ref` & `people_ref` perlu job sync berkala dari DB utama Eduzone
  — belum dibuat, jadi tabel ini masih harus diisi manual untuk testing.

## Testing lokal pakai Docker Compose

Cara paling cepat buat coba semuanya (Postgres + gateway) tanpa nyentuh
infra Swarm produksi. Sudah termasuk data dummy (sekolah, siswa, kredensial
RFID, 2 device, 1 jadwal) via `docker/initdb/`.

```bash
docker compose up --build
```

Postgres bakal jalan di `localhost:5433` (sengaja beda port dari 5432
default, biar tidak bentrok kalau di komputer lokal sudah ada Postgres
lain), gateway di `localhost:8080`.

**Coba check-in RFID di gerbang** (device umum, tanpa `schedule_id`):
```bash
curl -X POST http://localhost:8080/api/v1/checkin/device \
  -H "Content-Type: application/json" \
  -H "X-Device-Key: DEVKEY-GERBANG-01" \
  -d '{"method":"rfid","event_type":"check_in","credential_value":"CARD-ANDI-001"}'
```

**Coba check-in RFID di Lab** (device per-kelas, jadwal seed data selalu
dibuat aktif ±2 jam dari waktu compose dijalankan, jadi HARUS muncul
`schedule_id` di response):
```bash
curl -X POST http://localhost:8080/api/v1/checkin/device \
  -H "Content-Type: application/json" \
  -H "X-Device-Key: DEVKEY-LAB-01" \
  -d '{"method":"rfid","event_type":"check_in","credential_value":"CARD-ANDI-001"}'
```

**Reset data test** (hapus semua data & mulai dari seed lagi):
```bash
docker compose down -v   # -v menghapus volume, initdb akan jalan ulang
docker compose up --build
```

**Catatan penting yang kami temukan sendiri saat testing skema ini:**
`docker/initdb/02_seed.sql` sengaja `SET timezone = 'Asia/Jakarta'` di
baris pertama sebelum menghitung jadwal dummy. Kalau ini dihapus/lupa,
window jadwal akan dihitung pakai timezone default Postgres (UTC), padahal
gateway menghitung "jam sekarang" pakai `TZ=Asia/Jakarta` — jadwal yang
"seharusnya aktif" jadi tidak terdeteksi aktif. Pastikan asumsi timezone
ini konsisten di production juga (lihat catatan di bawah).

## Menjalankan secara lokal (tanpa Docker)

```bash
cp .env.example .env
# edit .env sesuai kebutuhan

export $(cat .env | xargs)
go run ./cmd/server
```

## Build & jalankan via Docker

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
infrastruktur yang sudah ada (Postgres, Redis, Nginx Proxy Manager), pakai
network overlay yang sama supaya bisa akses `eduzone_absensi` database.

## Setup database

Jalankan `absensi_schema.sql` (folder sebelah) ke database `eduzone_absensi`
yang baru (terpisah dari database utama Eduzone):

```bash
psql -h <host> -U <user> -d eduzone_absensi -f absensi_schema.sql
```
