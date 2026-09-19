# Kesiapan Endpoint untuk Testing & Debugging dengan Data Asli

Dokumen ini fokus buat 1 hal: **kalau kamu mau mulai tes pakai data
sekolah asli (bukan dummy `Andi Saputra`)**, endpoint mana yang siap,
apa yang perlu disiapin dulu, dan risiko apa yang belum pernah teruji.

---

## Ringkasan Cepat (Matriks Kesiapan)

| Endpoint | Kesiapan | Blocker utama |
|---|---|---|
| `POST /checkin/device` (RFID/QR) | 🟢✅ **TERUJI dengan data asli** | Sudah dites 8 Sept 2026 — check-in `Yance Puspasari` (siswa asli SMA Negeri 1 Surya Nusantara) berhasil, `event_id` nyambung, `photo_url` (avatar fallback) muncul benar. Database: 322 orang asli (302 siswa + 20 guru) + 1 device |
| `POST /checkin/device` (Face) | 🔴 **Belum bisa dites sama sekali** | Worker Python belum ada, job selalu "processing" |
| `GET /checkin/device/jobs/{id}` | 🔴 Sama seperti di atas | — |
| `POST /devices/heartbeat` | 🟢 Siap dites data asli | Perlu device asli ter-input |
| `POST /checkin/teacher` | 🟡 **Kodenya siap, TAPI belum pernah dites sekali pun** | Butuh JWT asli dari Laravel (belum ada — lihat `docs/status-dan-tugas-laravel.md` Bagian 2.1) |
| `GET /attendance/daily` | 🟡 Sama seperti di atas | Butuh JWT asli |
| `POST /enrollment/credentials` | 🟡 Sama seperti di atas | Butuh JWT asli role admin |
| `GET /media/photo/{person_id}` | 🟡 **Kodenya siap (lolos unit test), belum pernah dites end-to-end** | Butuh `people_ref.photo_url` beneran terisi — nunggu konfirmasi sync `people` dari Laravel benar-benar jalan (lihat `docs/status-dan-tugas-laravel.md`) |
| Agregasi harian (background) | 🟢 Siap, tapi baru 1x dites setelah bugfix terakhir | Perlu 1 siklus penuh dites lagi untuk konfirmasi final |
| Sync dari Laravel (background) | 🔴 Tidak akan berhasil sampai endpoint Laravel dibuat | Lihat `docs/status-dan-tugas-laravel.md` Bagian 2.2 |

🟢 Siap dites sekarang · 🟡 Siap kodenya, belum pernah tereksekusi · 🔴 Belum bisa dites sama sekali

**Rekomendasi urutan testing:** mulai dari 🟢 (RFID/QR + heartbeat)
karena ini yang paling banyak "jam terbang"-nya dan risikonya paling
kecil. Baru lanjut ke 🟡 setelah JWT issuance dari Laravel selesai —
karena begitu dicoba, ini kemungkinan besar bakal nemu bug baru
(namanya juga belum pernah dieksekusi sekali pun).

---

## Bagian 1: Data yang Harus Disiapkan buat Sekolah Asli

**Penting — beberapa tabel ini TIDAK PUNYA endpoint API buat
diisi/didaftarkan.** Cuma bisa lewat `psql` langsung. Ini bukan bug,
memang belum dibikinin endpoint admin buat provisioning-nya (di luar
cakupan sampai sekarang).

| Tabel | Ada endpoint API? | Cara isi sekarang |
|---|---|---|
| `schools_ref` | Tidak (kecuali lewat sync, belum aktif) | `INSERT` manual via `psql`, atau tunggu sync Laravel jadi |
| `devices` | **Tidak ada endpoint registrasi device** | `INSERT` manual via `psql` — generate `api_key_hash` = SHA-256 dari device key pilihan kamu |
| `school_networks` | Tidak | `INSERT` manual via `psql` — buat whitelist IP jaringan sekolah (dipakai validasi check-in guru) |
| `people_ref` | Tidak (kecuali lewat sync) | `INSERT` manual, atau tunggu sync Laravel |
| `credentials` (RFID/QR) | **Ada** — `POST /enrollment/credentials` | Tapi butuh JWT admin dulu (masih 🟡, lihat matriks) — sampai itu siap, isi manual via `psql` juga |

**Kalau mau tes RFID/QR sekarang dengan data sekolah asli** (tanpa
nunggu JWT/sync beres), urutan `INSERT` manual-nya:

```sql
-- 1. Sekolah asli
INSERT INTO schools_ref (school_id, name, latitude, longitude, geofence_radius_meters, late_cutoff_time, is_active)
VALUES ('<uuid>', 'Nama Sekolah Asli', <lat>, <lng>, 150, '07:15:00', true);

-- 2. Device asli (gerbang, misalnya)
-- api_key_hash = SHA-256("device-key-pilihan-kamu") dalam hex
INSERT INTO devices (id, school_id, device_code, name, device_type, api_key_hash, is_active)
VALUES ('<uuid>', '<school_id di atas>', 'GERBANG-01', 'RFID Gerbang Utama', 'rfid_reader', '<hash>', true);

-- 3. Orang asli (siswa/guru)
INSERT INTO people_ref (person_id, school_id, person_type, full_name, is_active)
VALUES ('<uuid>', '<school_id>', 'student', 'Nama Siswa Asli', true);

-- 4. Kredensial RFID asli (credential_hash = SHA-256 dari UID kartu fisik)
INSERT INTO credentials (id, school_id, person_id, person_type, method, credential_hash, is_active)
VALUES ('<uuid>', '<school_id>', '<person_id di atas>', 'student', 'rfid', '<hash UID kartu>', true);
```

**Cara generate hash SHA-256** (buat `api_key_hash` dan
`credential_hash`), contoh pakai PowerShell:
```powershell
$text = "isi-yang-mau-di-hash"
[System.BitConverter]::ToString([System.Security.Cryptography.SHA256]::Create().ComputeHash([System.Text.Encoding]::UTF8.GetBytes($text))) -replace '-','' | ForEach-Object { $_.ToLower() }
```

### Dataset asli yang SUDAH ada di database (per 8 Sept 2026)

Sekolah **"SMA Negeri 1 Surya Nusantara"** (data asli dari
`Student`/`Teacher`/`School` Laravel, di-export lewat Tinker) sudah
di-`INSERT` ke database gateway — **jangan generate ulang dari nol**
kalau butuh data testing, cek dulu apakah datanya masih ada:
```sql
SELECT COUNT(*) FROM people_ref WHERE school_id = '59f422c4-4fbc-4b89-b682-6649c945f02b';
-- harusnya 322 (302 siswa + 20 guru)
```

- **1 device tes**: `GERBANG-01`, device key plaintext
  `DEVKEY-SURYA-GERBANG-01`
- **322 kartu RFID sintetis**: `TESTCARD-0001` s/d `TESTCARD-0322`
  (bukan nomor kartu fisik asli — dibuat khusus testing, mapping
  lengkap nama↔nomor kartu ada di `kartu-referensi.csv` yang sudah
  dikirim ke pengguna, disimpan di folder `data-asli-surya/` proyek)
- **1 siswa di-skip** — ada 1 siswa (dari 303 di data asli) yang
  terdaftar di sekolah lain ("sekolah kedua") yang belum punya
  koordinat GPS, jadi belum ikut di-insert
- Endpoint `POST /checkin/device` (RFID) **sudah teruji beneran**
  pakai data ini — check-in `Yance Puspasari` (`TESTCARD-0001`)
  berhasil, `event_id` tercatat, avatar fallback muncul benar.

---

## Bagian 2: Risiko/Hal yang Belum Pernah Teruji dengan Data Asli

Ini daftar "yang perlu diwaspadai pas debugging", karena semua testing
sejauh ini pakai 1 device, 1 sekolah, 1 orang (`Andi Saputra`) — belum
ada yang benar-benar menguji kondisi dunia nyata di bawah ini:

1. **Multi-sekolah sekaligus** — semua logic multi-tenant (`school_id`
   scoping) sudah ditulis dengan asumsi itu, tapi belum pernah dites
   dengan 2+ sekolah aktif bersamaan dalam 1 database.
2. **Banyak device/orang sekaligus (load)** — belum ada uji beban.
   Kalau nanti device fisik beneran nge-hit bersamaan pas jam masuk
   sekolah (puluhan RFID tap dalam hitungan detik), belum ada jaminan
   performanya — meskipun secara desain sudah dipikirkan (transaksi +
   lock di `checkin_device.go`, index di skema).
3. **`X-Real-IP`/`X-Forwarded-For` dari NPM beneran** — asumsi soal NPM
   menimpa header ini (bukan meneruskan mentah dari klien) itu asumsi
   standar, TAPI belum pernah divalidasi eksplisit dengan cara kirim
   header palsu dari luar terus cek apakah beneran ketolak. Penting
   khususnya untuk `checkin/teacher` yang mengandalkan IP buat validasi
   jaringan sekolah — dan endpoint ini sendiri belum pernah dites sama
   sekali (lihat matriks).
4. **`late_cutoff_time` NULL vs terisi** — baru dites dengan 1 nilai
   (`07:15:00`). Kalau lupa isi ini buat sekolah baru, status Terlambat
   TIDAK PERNAH muncul (sengaja, biar aman) — tapi ini juga berarti
   kalau lupa, kesannya "kok gak ada yang kedeteksi telat", padahal
   bukan bug, itu perilaku by design.
5. **Device dengan `default_class_id` terisi (auto-jadwal)** — cuma
   dites 1 device (Lab). Belum dites skenario device pindah kelas,
   atau 1 kelas dipakai gantian beberapa mata pelajaran hari yang sama.
6. **Rate limiting device key (`attemptTracker`)** — 10 gagal dalam 5
   menit baru dites lewat unit test (server tiruan), belum pernah
   dipicu beneran dengan device fisik (misal device salah konfigurasi
   device key terus-menerus).

---

## Bagian 3: Checklist Debugging Cepat

Kalau ada yang aneh pas testing data asli, urutan cek yang disaranin:

1. **Log gateway dulu** — `docker logs -f absensi-gateway-absensi-gateway-1`.
   Semua error (auth gagal, DB error, sync gagal) muncul di sini dengan
   pesan berbahasa Indonesia yang cukup jelas.
2. **Cek langsung ke database** kalau responsnya "sukses" tapi hasilnya
   kelihatan salah:
   ```sql
   SELECT * FROM attendance_events ORDER BY recorded_at DESC LIMIT 10;
   SELECT * FROM attendance_daily ORDER BY updated_at DESC LIMIT 10;
   SELECT * FROM ref_sync_state;  -- status sync terakhir
   ```
3. **Kalau abis ganti kode**, WAJIB `docker compose build --no-cache &&
   docker compose up -d --force-recreate` — `restart` doang TIDAK
   menarik kode baru (pernah kejadian, lihat README bagian
   Troubleshooting).
4. **Kalau curiga masalah timezone** (jam check-in kelihatan geser),
   cek `TZ=Asia/Jakarta` beneran ke-set di container:
   ```bash
   docker exec absensi-gateway-absensi-gateway-1 date
   ```
