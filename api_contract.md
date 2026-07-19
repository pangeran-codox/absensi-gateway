# API Contract — Absensi Gateway (Go Service)

Base URL (contoh): `https://absensi.wazzgroup.com/api/v1`

## Autentikasi

| Klien | Metode |
|---|---|
| Device tetap (RFID/QR/Face di sekolah) | Header `X-Device-Key: <raw api key>` — diverifikasi terhadap `devices.api_key_hash` |
| Guru via web/PWA | Header `Authorization: Bearer <JWT>` — JWT diterbitkan Eduzone (Laravel) saat login, diverifikasi Go gateway secara stateless pakai shared secret/public key |
| Admin (enrollment kredensial) | Sama seperti guru (JWT Eduzone), dicek role admin di klaim token |

Catatan: Go gateway **tidak pernah** memanggil balik Laravel untuk verifikasi token — verifikasi signature JWT dilakukan lokal, cepat, dan tidak bergantung koneksi ke Eduzone utama.

---

## 1. Check-in Siswa (Device Tetap)

`POST /checkin/device`

Dipakai RFID reader / QR kiosk / kamera face-recognition yang terpasang di sekolah.

**Headers:** `X-Device-Key: <key>`

**Body (RFID/QR — cepat, sinkron):**
```json
{
  "method": "rfid",
  "event_type": "check_in",
  "credential_value": "raw_uid_or_qr_token",
  "client_timestamp": "2026-07-19T07:00:00+07:00"
}
```

**Response sukses (200):**
```json
{
  "status": "accepted",
  "event_id": 88123,
  "person": { "id": "uuid...", "name": "Andi Saputra", "type": "student" },
  "schedule_id": "uuid...",
  "recorded_at": "2026-07-19T07:00:01+07:00"
}
```
Field `schedule_id` hanya muncul kalau device terpasang tetap di 1 ruang
kelas/lab (`devices.default_class_id` terisi) DAN saat ini memang sedang
ada jam pelajaran aktif untuk kelas tersebut (dicocokkan otomatis lewat
`schedules_ref`). Device umum (mis. gerbang) tidak akan pernah punya
field ini di response-nya.

**Response gagal — kredensial tidak dikenali (404):**
```json
{
  "status": "rejected",
  "reason": "credential_not_found",
  "message": "Kartu/token tidak terdaftar"
}
```

**Response gagal — anomali (200, tapi ditandai):**
```json
{
  "status": "accepted_with_flag",
  "event_id": 88124,
  "person": { "id": "uuid...", "name": "Andi Saputra", "type": "student" },
  "anomaly_reasons": ["duplicate_scan_within_5s"]
}
```

**Body (Face — async, karena inference berat):**
```json
{
  "method": "face",
  "event_type": "check_in",
  "image_base64": "...",
  "client_timestamp": "2026-07-19T07:00:00+07:00"
}
```

**Response awal (202 Accepted):**
```json
{
  "status": "processing",
  "job_id": "job_9f8e7d..."
}
```

Device lalu polling hasilnya:

`GET /checkin/device/jobs/{job_id}`

```json
{
  "status": "done",
  "result": "matched",
  "person": { "id": "uuid...", "name": "Andi Saputra", "type": "student" },
  "confidence_score": 94.2,
  "event_id": 88125
}
```
atau
```json
{ "status": "done", "result": "no_match" }
```
atau
```json
{ "status": "done", "result": "liveness_failed" }
```

---

## 2. Check-in Guru (Web/PWA via HP)

`POST /checkin/teacher`

**Headers:** `Authorization: Bearer <JWT Eduzone>`

**Body:**
```json
{
  "event_type": "check_in",
  "latitude": -6.123456,
  "longitude": 106.123456,
  "accuracy_meters": 12.5,
  "client_timestamp": "2026-07-19T07:00:00+07:00"
}
```

Catatan: IP request **diambil dari sisi server** (bukan dari body), supaya tidak bisa dipalsukan klien.

**Response — semua validasi lolos (200):**
```json
{
  "status": "accepted",
  "event_id": 88200,
  "validations": { "gps_in_radius": true, "network_recognized": true }
}
```

**Response — soft flag, tetap tercatat (200):**
```json
{
  "status": "accepted_with_flag",
  "event_id": 88201,
  "validations": { "gps_in_radius": false, "network_recognized": true },
  "anomaly_reasons": ["gps_out_of_radius"]
}
```

---

## 3. Enrollment Kredensial (Admin)

`POST /enrollment/credentials`

**Headers:** `Authorization: Bearer <JWT Eduzone>` (role: admin)

**Body (RFID/QR):**
```json
{
  "person_id": "uuid...",
  "person_type": "student",
  "method": "rfid",
  "credential_value": "raw_uid_kartu"
}
```

**Body (Face):**
```json
{
  "person_id": "uuid...",
  "person_type": "student",
  "method": "face",
  "images_base64": ["...", "...", "..."]
}
```
→ dikirim ke queue, worker Python bikin embedding, disimpan terenkripsi ke `face_templates`.

**Response (201):**
```json
{ "status": "enrolled", "credential_id": "uuid..." }
```

---

## 4. Query Status Absensi

`GET /attendance/daily?person_id={uuid}&date=2026-07-19`

**Response (200):**
```json
{
  "person_id": "uuid...",
  "date": "2026-07-19",
  "status": "Hadir",
  "first_check_in": "07:00:01",
  "last_check_out": null,
  "primary_method": "rfid",
  "has_anomaly": false
}
```

---

## 5. Device Heartbeat

`POST /devices/heartbeat`

**Headers:** `X-Device-Key: <key>`

**Response (200):**
```json
{ "status": "ok", "server_time": "2026-07-19T07:00:00+07:00" }
```
Dipakai gateway update `devices.last_seen_at`, juga buat device tahu jam server (sinkronisasi waktu).

---

## Urutan Validasi (Check-in Guru)

1. Verifikasi JWT valid & belum expired
2. Ambil IP request dari server (bukan klien)
3. Cek IP terhadap `school_networks` sekolah guru tsb:
   - Kalau match jaringan yang `requires_local_verifier = false` → `network_recognized = true`
   - Kalau jaringan itu `requires_local_verifier = true` (mis. IndiHome CGNAT) → butuh `presence_ticket` valid dari `local_verifiers` (belum aktif — lihat catatan di skema)
   - Kalau tidak match sama sekali → `network_recognized = false`
4. Hitung jarak GPS klien ke koordinat sekolah, cek radius
5. Kalau GPS **dan** network dua-duanya valid → `accepted`
6. Kalau salah satu gagal → `accepted_with_flag` (soft flag, tetap tercatat + `has_anomaly = true` di `attendance_daily`)

## Kode Error Umum

| HTTP Code | Arti |
|---|---|
| 400 | Body request tidak valid/field kurang |
| 401 | Token/API key tidak valid atau expired |
| 404 | Kredensial/device/person tidak ditemukan |
| 409 | Duplicate event (mis. sudah check-in hari ini, coba check-in lagi) |
| 422 | Validasi bisnis gagal (mis. metode tidak aktif untuk sekolah ini) |
| 500 | Error internal gateway |
