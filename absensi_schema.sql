-- =====================================================================
-- SKEMA DATABASE: eduzone_absensi
-- Database terpisah khusus fitur Absensi Canggih (multi-metode)
-- Didesain untuk TIDAK menyentuh database utama Eduzone (maikel/laravel).
-- Referensi ke school_id / student_id / teacher_id disimpan sebagai UUID
-- polos (tanpa FK lintas-database), lalu divalidasi & disinkron via
-- job/queue dari aplikasi Laravel utama.
-- =====================================================================

-- Rekomendasi: buat sebagai database baru, bukan schema baru di DB yang sama,
-- supaya benar-benar terisolasi (resource, backup, dan risiko locking terpisah).
-- Contoh: createdb -O laravel eduzone_absensi

-- ---------------------------------------------------------------------
-- 1. schools_ref & people_ref
-- Cache lokal ringan dari data master di DB utama.
-- Tujuan: validasi cepat tanpa cross-database query setiap kali ada event
-- absensi (RFID tap / QR scan bisa terjadi ratusan kali per menit saat jam masuk).
-- Disinkron berkala (mis. tiap 5-15 menit) atau via event saat data berubah.
-- ---------------------------------------------------------------------

CREATE TABLE schools_ref (
    school_id               uuid PRIMARY KEY,   -- sama dengan schools.id di DB utama
    name                    varchar(255) NOT NULL,
    latitude                numeric(10, 7) NOT NULL,  -- titik pusat sekolah, untuk geofencing GPS guru
    longitude               numeric(10, 7) NOT NULL,
    geofence_radius_meters  integer NOT NULL DEFAULT 150,
    is_active               boolean NOT NULL DEFAULT true,
    synced_at               timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE people_ref (
    person_id       uuid NOT NULL,             -- sama dengan students.id / teachers.id / staff.id
    school_id       uuid NOT NULL REFERENCES schools_ref(school_id) ON DELETE CASCADE,
    person_type     varchar(20) NOT NULL CHECK (person_type IN ('student', 'teacher', 'staff')),
    full_name       varchar(255) NOT NULL,
    class_id        uuid,                      -- khusus student, cache dari classes.id
    grade           varchar(50),
    is_active       boolean NOT NULL DEFAULT true,
    synced_at       timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (person_id, person_type)
);

CREATE INDEX idx_people_ref_school ON people_ref(school_id);
CREATE INDEX idx_people_ref_class ON people_ref(class_id) WHERE class_id IS NOT NULL;

-- ---------------------------------------------------------------------
-- 1c. schedules_ref
-- Cache jadwal pelajaran dari DB utama (tabel schedules Eduzone).
-- Dibutuhkan supaya gateway tahu "jam segini, kelas ini, mapel apa,
-- diajar guru siapa" saat ada check-in per jam pelajaran (bukan cuma
-- absen harian gerbang). Disinkron satu arah dari Eduzone utama,
-- sama seperti people_ref & schools_ref.
-- ---------------------------------------------------------------------

CREATE TABLE schedules_ref (
    schedule_id     uuid PRIMARY KEY,          -- sama dengan schedules.id di DB utama
    school_id       uuid NOT NULL REFERENCES schools_ref(school_id) ON DELETE CASCADE,
    class_id        uuid NOT NULL,
    subject_name    varchar(150) NOT NULL,
    teacher_id      uuid NOT NULL,
    day_of_week     smallint NOT NULL CHECK (day_of_week BETWEEN 1 AND 7),  -- 1=Senin .. 7=Minggu
    start_time      time NOT NULL,
    end_time        time NOT NULL,
    is_active       boolean NOT NULL DEFAULT true,
    synced_at       timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_schedules_class_day ON schedules_ref(class_id, day_of_week) WHERE is_active = true;
CREATE INDEX idx_schedules_teacher_day ON schedules_ref(teacher_id, day_of_week) WHERE is_active = true;

-- ---------------------------------------------------------------------
-- 1b. school_networks
-- Daftar jaringan/IP yang diizinkan untuk validasi check-in guru via HP
-- (web/PWA + GPS + IP publik sekolah). Sengaja berupa DAFTAR, bukan 1
-- kolom IP tunggal, karena sekolah bisa punya lebih dari 1 ISP
-- (mis. iForte statis + IndiHome cadangan). AKTIF DIPAKAI dari awal,
-- karena ini fondasi akurasi geofencing guru.
-- ---------------------------------------------------------------------

CREATE TABLE school_networks (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id               uuid NOT NULL REFERENCES schools_ref(school_id) ON DELETE CASCADE,
    label                   varchar(100) NOT NULL,   -- mis. "iForte Utama", "IndiHome Cadangan"
    ip_or_hostname          varchar(255) NOT NULL,   -- IP statis, atau hostname DDNS kalau is_dynamic
    is_dynamic              boolean NOT NULL DEFAULT false,  -- true = perlu re-resolve DDNS tiap validasi
    requires_local_verifier boolean NOT NULL DEFAULT false,  -- true untuk jaringan CGNAT (mis. IndiHome residensial)
                                                               -- yang IP publiknya tidak bisa diandalkan
    is_active               boolean NOT NULL DEFAULT true,
    created_at              timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_school_networks_school ON school_networks(school_id) WHERE is_active = true;

-- ---------------------------------------------------------------------
-- 2. devices
-- Semua alat/terminal absensi: kamera face-recognition, RFID reader,
-- QR scanner, atau hybrid. Satu sekolah/lab bisa punya banyak device.
-- ---------------------------------------------------------------------

CREATE TABLE devices (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       uuid NOT NULL REFERENCES schools_ref(school_id) ON DELETE CASCADE,
    device_code     varchar(50) NOT NULL,      -- kode unik alat, mis. "LAB1-CAM-01"
    name            varchar(150) NOT NULL,
    device_type     varchar(20) NOT NULL CHECK (device_type IN ('face_camera', 'rfid_reader', 'qr_scanner', 'hybrid', 'manual_kiosk')),
    location        varchar(150),              -- mis. "Pintu Gerbang", "Lab Komputer 1"
    default_class_id uuid,                     -- kalau device ini terpasang tetap di 1 ruang kelas/lab
                                                 -- tertentu, dipakai untuk auto-resolve schedules_ref
                                                 -- yang sedang aktif saat check-in terjadi. NULL untuk
                                                 -- device umum (mis. gerbang utama) yang bukan per-kelas.
    ip_address      varchar(45),
    api_key_hash    varchar(64),               -- untuk otentikasi device saat POST event
    last_seen_at    timestamp(0),
    is_active       boolean NOT NULL DEFAULT true,
    created_at      timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (school_id, device_code)
);

-- ---------------------------------------------------------------------
-- 3. credentials
-- Satu orang bisa punya beberapa metode absensi sekaligus (RFID + QR
-- cadangan, misalnya). Nilai kredensial sensitif (UID kartu, token QR)
-- disimpan dalam bentuk hash, bukan plaintext.
-- ---------------------------------------------------------------------

CREATE TABLE credentials (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id           uuid NOT NULL REFERENCES schools_ref(school_id) ON DELETE CASCADE,
    person_id           uuid NOT NULL,
    person_type         varchar(20) NOT NULL CHECK (person_type IN ('student', 'teacher', 'staff')),
    method              varchar(20) NOT NULL CHECK (method IN ('rfid', 'qr', 'face', 'fingerprint', 'manual')),
    credential_hash     varchar(128),          -- hash dari UID RFID / token QR (null utk 'face' & 'manual')
    is_active           boolean NOT NULL DEFAULT true,
    enrolled_at         timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at          timestamp(0),
    FOREIGN KEY (person_id, person_type) REFERENCES people_ref(person_id, person_type) ON DELETE CASCADE,
    UNIQUE (school_id, method, credential_hash)
);

CREATE INDEX idx_credentials_person ON credentials(person_id, person_type);

-- ---------------------------------------------------------------------
-- 4. face_templates
-- Dipisah dari 'credentials' karena data biometrik butuh perlakuan
-- khusus (enkripsi, retensi, hak hapus). Mengikuti pola *_sensitive_data
-- yang sudah ada di DB utama Eduzone.
-- Simpan embedding terenkripsi di application layer (AES), kolom di
-- sini menampung ciphertext, bukan vector mentah.
-- ---------------------------------------------------------------------

CREATE TABLE face_templates (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    credential_id       uuid NOT NULL REFERENCES credentials(id) ON DELETE CASCADE,
    embedding_encrypted bytea NOT NULL,        -- hasil enkripsi vector embedding (mis. dari FaceNet/InsightFace)
    model_version       varchar(50) NOT NULL,  -- penting untuk migrasi kalau ganti model
    quality_score       numeric(5,2),
    created_at          timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ---------------------------------------------------------------------
-- 5. attendance_events (RAW LOG - immutable)
-- Setiap tap/scan/deteksi wajah masuk sini apa adanya, termasuk yang
-- gagal/diragukan. Ini sumber kebenaran mentah untuk audit & anti-fraud.
-- TIDAK di-update, hanya di-insert.
-- ---------------------------------------------------------------------

CREATE TABLE attendance_events (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    school_id           uuid NOT NULL REFERENCES schools_ref(school_id) ON DELETE CASCADE,
    device_id           uuid REFERENCES devices(id) ON DELETE SET NULL,
    schedule_id         uuid REFERENCES schedules_ref(schedule_id),  -- NULL = absen harian biasa (gerbang),
                                                                       -- terisi = absen terkait jam pelajaran
                                                                       -- tertentu (mis. masuk lab utk mapel X)
    person_id           uuid,                  -- bisa NULL jika gagal dikenali (unknown face, kartu tak terdaftar)
    person_type         varchar(20) CHECK (person_type IN ('student', 'teacher', 'staff')),
    method              varchar(20) NOT NULL CHECK (method IN ('rfid', 'qr', 'face', 'fingerprint', 'manual')),
    event_type          varchar(15) NOT NULL CHECK (event_type IN ('check_in', 'check_out', 'unknown')),
    confidence_score    numeric(5,2),          -- khusus face recognition (0-100)
    is_valid            boolean NOT NULL DEFAULT true,
    flagged_reason       varchar(100),          -- mis. 'low_confidence', 'duplicate_scan', 'out_of_schedule'
    raw_payload         jsonb,                 -- payload asli dari device (utk debugging)
    recorded_at         timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_events_school_date ON attendance_events(school_id, recorded_at);
CREATE INDEX idx_events_person ON attendance_events(person_id, person_type, recorded_at);
CREATE INDEX idx_events_device ON attendance_events(device_id, recorded_at);
CREATE INDEX idx_events_schedule ON attendance_events(schedule_id, recorded_at) WHERE schedule_id IS NOT NULL;
CREATE INDEX idx_events_unrecognized ON attendance_events(school_id, recorded_at) WHERE person_id IS NULL;

-- ---------------------------------------------------------------------
-- 6. attendance_daily (AGREGAT - hasil olahan dari attendance_events)
-- Satu baris per orang per hari. Dihitung ulang (upsert) tiap kali ada
-- event baru masuk. Struktur ini SENGAJA dibuat mirip dengan
-- student_attendance / teacher_attendance di DB utama supaya proses
-- sinkronisasi jadi mapping 1:1.
-- ---------------------------------------------------------------------

CREATE TABLE attendance_daily (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id           uuid NOT NULL REFERENCES schools_ref(school_id) ON DELETE CASCADE,
    person_id           uuid NOT NULL,
    person_type         varchar(20) NOT NULL CHECK (person_type IN ('student', 'teacher', 'staff')),
    date                date NOT NULL,
    first_check_in      time(0),
    last_check_out      time(0),
    status              varchar(20) NOT NULL DEFAULT 'Hadir' CHECK (status IN ('Hadir', 'Terlambat', 'Sakit', 'Izin', 'Alpa')),
    primary_method      varchar(20),           -- metode yang dipakai saat check-in pertama
    total_events        integer NOT NULL DEFAULT 0,
    has_anomaly         boolean NOT NULL DEFAULT false,
    updated_at          timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (school_id, person_id, person_type, date)
);

CREATE INDEX idx_daily_school_date ON attendance_daily(school_id, date);

-- ---------------------------------------------------------------------
-- 7b. attendance_period
-- Agregat absensi PER JAM PELAJARAN — beda dari attendance_daily yang
-- cuma 1 baris per orang per hari. Di sini bisa ada banyak baris per
-- hari (satu per schedule/jam mapel). Struktur ini yang nanti disync
-- ke student_subject_attendance (siswa) & teaching_attendance (guru)
-- di DB utama Eduzone, yang memang sudah dipisah per jadwal.
-- ---------------------------------------------------------------------

CREATE TABLE attendance_period (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id           uuid NOT NULL REFERENCES schools_ref(school_id) ON DELETE CASCADE,
    schedule_id         uuid NOT NULL REFERENCES schedules_ref(schedule_id),
    person_id           uuid NOT NULL,
    person_type         varchar(20) NOT NULL CHECK (person_type IN ('student', 'teacher')),
    date                date NOT NULL,
    first_check_in      time(0),
    last_check_out      time(0),
    status              varchar(20) NOT NULL DEFAULT 'Hadir' CHECK (status IN ('Hadir', 'Terlambat', 'Sakit', 'Izin', 'Alpa')),
    primary_method      varchar(20),
    total_events        integer NOT NULL DEFAULT 0,
    has_anomaly         boolean NOT NULL DEFAULT false,
    updated_at          timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (school_id, schedule_id, person_id, person_type, date)
);

CREATE INDEX idx_period_school_date ON attendance_period(school_id, date);
CREATE INDEX idx_period_schedule ON attendance_period(schedule_id, date);
CREATE INDEX idx_period_person ON attendance_period(person_id, person_type, date);

-- ---------------------------------------------------------------------
-- 7. sync_log
-- Antrian & jejak sinkronisasi agregat harian ke tabel student_attendance
-- / teacher_attendance di database utama Eduzone. Dijalankan oleh queue
-- worker Laravel (mis. tiap beberapa menit atau end-of-day).
-- ---------------------------------------------------------------------

CREATE TABLE sync_log (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source_table        varchar(30) NOT NULL CHECK (source_table IN ('attendance_daily', 'attendance_period')),
    source_id           uuid NOT NULL,          -- id baris di attendance_daily ATAU attendance_period
    target_table        varchar(50) NOT NULL,   -- 'student_attendance' / 'teacher_attendance' /
                                                  -- 'student_subject_attendance' / 'teaching_attendance'
    status              varchar(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'success', 'failed')),
    error_message        text,
    attempted_at        timestamp(0),
    synced_at           timestamp(0),
    created_at          timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_sync_log_pending ON sync_log(status) WHERE status = 'pending';
CREATE INDEX idx_sync_log_source ON sync_log(source_table, source_id);

-- ---------------------------------------------------------------------
-- 8. attendance_events - kolom tambahan untuk hash chaining & signing
-- BELUM DIAKTIFKAN/DIISI. Kolom disiapkan dulu; logika pengisian &
-- verifikasi baru diimplementasikan saat lapisan keamanan ini benar-
-- benar dipakai.
-- ---------------------------------------------------------------------

ALTER TABLE attendance_events
    ADD COLUMN row_hash   varchar(64),   -- SHA-256 dari (data baris ini + prev_hash)
    ADD COLUMN prev_hash  varchar(64),   -- row_hash milik baris sebelumnya (rantai)
    ADD COLUMN signature  text;          -- signature dari device atas raw_payload

-- ---------------------------------------------------------------------
-- 9. device_keys
-- Public key tiap device untuk verifikasi signature (device signing).
-- Private key TETAP di device, tidak pernah dikirim/disimpan di server.
-- BELUM DIPAKAI - siapkan dulu strukturnya, wajibkan signature belakangan.
-- ---------------------------------------------------------------------

CREATE TABLE device_keys (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id           uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    public_key          text NOT NULL,          -- Ed25519 public key (base64)
    algorithm           varchar(20) NOT NULL DEFAULT 'ed25519',
    is_active           boolean NOT NULL DEFAULT true,
    registered_at       timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at          timestamp(0)
);

CREATE INDEX idx_device_keys_device ON device_keys(device_id) WHERE is_active = true;

-- ---------------------------------------------------------------------
-- 10. qr_tokens
-- Token QR dinamis/rotating untuk mencegah replay (screenshot & sebar
-- ulang). Satu token hanya valid dalam rentang waktu singkat & sekali
-- pakai. BELUM DIPAKAI - kiosk QR statis dulu di versi awal, token
-- rotating diaktifkan belakangan kalau titip-absen via QR jadi masalah.
-- ---------------------------------------------------------------------

CREATE TABLE qr_tokens (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id           uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    token               varchar(20) NOT NULL,
    valid_from          timestamp(0) NOT NULL,
    valid_until         timestamp(0) NOT NULL,
    used_at             timestamp(0),           -- diisi begitu token dipakai; NULL = belum dipakai
    used_by_event_id    bigint REFERENCES attendance_events(id),
    UNIQUE (device_id, token)
);

CREATE INDEX idx_qr_tokens_active ON qr_tokens(device_id, valid_until) WHERE used_at IS NULL;

-- ---------------------------------------------------------------------
-- 11. attendance_correction_log
-- Jejak audit untuk koreksi manual terhadap attendance_daily, mengikuti
-- pola audit_keuangan yang sudah ada di DB utama Eduzone (data_lama/
-- data_baru dalam jsonb). Koreksi TIDAK langsung UPDATE attendance_daily,
-- harus lewat alur approval yang tercatat di sini.
-- BELUM DIPAKAI - proses koreksi masih manual/langsung di versi awal.
-- ---------------------------------------------------------------------

CREATE TABLE attendance_correction_log (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_table        varchar(30) NOT NULL CHECK (source_table IN ('attendance_daily', 'attendance_period')),
    source_id           uuid NOT NULL,          -- id baris di attendance_daily ATAU attendance_period
    requested_by        uuid NOT NULL,          -- users.id di DB utama (wali kelas/guru)
    reason              text NOT NULL,
    data_lama           jsonb NOT NULL,
    data_baru           jsonb NOT NULL,
    status              varchar(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    reviewed_by         uuid,                   -- users.id yang approve/reject
    reviewed_at         timestamp(0),
    created_at          timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_correction_pending ON attendance_correction_log(status) WHERE status = 'pending';
CREATE INDEX idx_correction_source ON attendance_correction_log(source_table, source_id);

-- ---------------------------------------------------------------------
-- 12. local_verifiers
-- Registry service "Local Presence Verifier" - 1 instance kecil per
-- sekolah yang HANYA bisa diakses dari jaringan lokal (LAN) sekolah,
-- tidak exposed ke internet. Dipakai untuk membuktikan HP guru benar-
-- benar berada di WiFi sekolah, tanpa bergantung pada IP publik ISP
-- (menyelesaikan masalah CGNAT IndiHome). BELUM DIPAKAI - jalankan
-- school_networks + GPS dulu, verifier ini aktif belakangan setelah
-- service-nya benar-benar dideploy di infra sekolah.
-- ---------------------------------------------------------------------

CREATE TABLE local_verifiers (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id           uuid NOT NULL REFERENCES schools_ref(school_id) ON DELETE CASCADE,
    internal_hostname   varchar(255) NOT NULL,   -- mis. "192.168.1.10" atau "verifier.local"
    public_key          text NOT NULL,           -- untuk verifikasi signature tiket yang diterbitkan
    algorithm           varchar(20) NOT NULL DEFAULT 'ed25519',
    is_active           boolean NOT NULL DEFAULT true,
    last_heartbeat_at   timestamp(0),            -- verifier lapor "masih hidup" secara berkala
    created_at          timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_local_verifiers_school ON local_verifiers(school_id) WHERE is_active = true;

-- ---------------------------------------------------------------------
-- 13. presence_tickets
-- Tiket sementara yang diterbitkan local_verifiers, membuktikan HP guru
-- ada di LAN sekolah saat itu. Dikirim bersamaan dengan request check-in
-- ke server pusat, lalu diverifikasi & langsung invalid (sekali pakai).
-- BELUM DIPAKAI.
-- ---------------------------------------------------------------------

CREATE TABLE presence_tickets (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    verifier_id         uuid NOT NULL REFERENCES local_verifiers(id) ON DELETE CASCADE,
    nonce               varchar(64) NOT NULL,    -- string acak, mencegah tiket ditebak
    signature           text NOT NULL,           -- signature verifier atas (nonce + issued_at)
    issued_at           timestamp(0) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at          timestamp(0) NOT NULL,   -- valid singkat, mis. 30-60 detik
    used_at             timestamp(0),
    used_by_event_id    bigint,                  -- diisi setelah dipakai untuk 1 check-in guru
    UNIQUE (verifier_id, nonce)
);

CREATE INDEX idx_presence_tickets_active ON presence_tickets(verifier_id, expires_at) WHERE used_at IS NULL;

-- =====================================================================
-- CATATAN: LAPISAN KEAMANAN LANJUTAN (BELUM AKTIF)
--
-- Struktur di atas (row_hash/prev_hash, device_keys, signature,
-- qr_tokens, attendance_correction_log) sengaja disiapkan sejak awal
-- supaya tidak perlu migrasi besar nanti, TAPI belum ada logika
-- aplikasi yang mengisi/menegakkannya. Fokus tahap pertama: sistem
-- absen jalan dulu dengan baik (RFID/QR/Face + geofencing guru).
--
-- Yang PERLU dilakukan nanti kalau lapisan ini diaktifkan:
-- 1. Hash chaining: isi row_hash/prev_hash tiap INSERT baru ke
--    attendance_events, plus job verifikasi berkala.
-- 2. Device signing: device generate keypair Ed25519, public key
--    didaftarkan ke device_keys, gateway Go verifikasi signature
--    tiap request masuk sebelum insert.
-- 3. QR rotating: kiosk generate token baru tiap 15-30 detik ke
--    qr_tokens, endpoint scan cek valid_until & used_at.
-- 4. Correction log: ubah endpoint edit attendance_daily supaya
--    insert ke attendance_correction_log dulu (status pending),
--    endpoint approve terpisah yang baru benar-benar update data.
-- 5. Permission hardening: REVOKE UPDATE, DELETE ON attendance_events
--    FROM <role_aplikasi>; jalankan setelah alur insert-only teruji.
-- =====================================================================

-- =====================================================================
-- CATATAN ALUR (untuk implementasi di Maikel/Eduzone nanti):
--
-- 1. Device (RFID/QR/Face) -> POST event mentah ke API absensi
--    -> INSERT ke attendance_events (selalu insert, walau gagal kenali).
--
-- 2. Worker/trigger mengolah event terbaru per (school_id, person_id, date)
--    -> UPSERT ke attendance_daily (hitung first_check_in, last_check_out,
--       status, deteksi anomali seperti duplicate tap < 5 detik).
--
-- 3. Job terjadwal (Laravel queue) membaca attendance_daily yang belum
--    sync -> upsert ke student_attendance / teacher_attendance di DB utama
--    -> catat hasilnya di sync_log.
--
-- 4. schools_ref & people_ref disinkron SATU ARAH dari DB utama ke DB ini
--    (bukan sebaliknya), supaya data induk (nama siswa, kelas) tetap
--    Eduzone utama sebagai source of truth.
-- =====================================================================
