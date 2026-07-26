-- Seed data untuk testing lokal via docker-compose. JANGAN dipakai di
-- production - ini cuma data dummy biar gateway bisa langsung dicoba
-- tanpa perlu isi data manual dulu.

-- PENTING: timezone sesi di-set eksplisit ke Asia/Jakarta di sini, supaya
-- CURRENT_TIME/CURRENT_TIMESTAMP di bawah dihitung dalam jam WIB - harus
-- SAMA PERSIS dengan TZ yang dipakai container absensi-gateway (lihat
-- docker-compose.yml). Kalau dua-duanya tidak konsisten, jadwal yang
-- "seharusnya aktif sekarang" bisa dianggap tidak aktif oleh Go gateway
-- (persis kasus yang kami temukan saat testing skema ini).
SET timezone = 'Asia/Jakarta';

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Sekolah dummy (koordinat: contoh titik di Surabaya)
INSERT INTO schools_ref (school_id, name, latitude, longitude, geofence_radius_meters)
VALUES ('11111111-1111-1111-1111-111111111111', 'Sekolah Test', -7.257472, 112.752090, 150);

-- Jaringan sekolah (contoh IP, ganti sesuai kebutuhan testing)
INSERT INTO school_networks (school_id, label, ip_or_hostname, is_dynamic, requires_local_verifier)
VALUES ('11111111-1111-1111-1111-111111111111', 'iForte Utama', '103.10.10.10', false, false);

-- Siswa dummy
INSERT INTO people_ref (person_id, school_id, person_type, full_name)
VALUES ('22222222-2222-2222-2222-222222222222', '11111111-1111-1111-1111-111111111111', 'student', 'Andi Saputra (Dummy)');

-- Kredensial RFID siswa. Raw UID kartu (buat dicoba lewat curl/Postman): CARD-ANDI-001
INSERT INTO credentials (school_id, person_id, person_type, method, credential_hash)
VALUES ('11111111-1111-1111-1111-111111111111', '22222222-2222-2222-2222-222222222222', 'student', 'rfid',
        encode(digest('CARD-ANDI-001', 'sha256'), 'hex'));

-- Device gerbang (device umum, tanpa default_class_id).
-- Raw device key (buat header X-Device-Key): DEVKEY-GERBANG-01
INSERT INTO devices (school_id, device_code, name, device_type, location, api_key_hash)
VALUES ('11111111-1111-1111-1111-111111111111', 'GATE-01', 'RFID Gerbang Utama', 'rfid_reader', 'Gerbang Sekolah',
        encode(digest('DEVKEY-GERBANG-01', 'sha256'), 'hex'));

-- Kelas & jadwal dummy yang SELALU aktif saat compose dijalankan (dihitung
-- dinamis dari waktu sekarang), supaya fitur absen per-jam-pelajaran bisa
-- langsung dicoba tanpa perlu atur ulang jadwal tiap kali testing.
INSERT INTO schedules_ref (schedule_id, school_id, class_id, subject_name, teacher_id, day_of_week, start_time, end_time)
VALUES (
  '44444444-4444-4444-4444-444444444444',
  '11111111-1111-1111-1111-111111111111',
  '55555555-5555-5555-5555-555555555555',
  'Matematika (Dummy)',
  '33333333-3333-3333-3333-333333333333',
  EXTRACT(ISODOW FROM CURRENT_TIMESTAMP)::smallint,
  (CURRENT_TIME - interval '2 hours')::time,
  (CURRENT_TIME + interval '2 hours')::time
);

-- Device di Lab, terikat ke class_id di atas -> otomatis tag schedule_id
-- saat check-in. Raw device key: DEVKEY-LAB-01
INSERT INTO devices (school_id, device_code, name, device_type, location, default_class_id, api_key_hash)
VALUES (
  '11111111-1111-1111-1111-111111111111', 'LAB-01', 'RFID Lab Komputer', 'rfid_reader', 'Lab Komputer 1',
  '55555555-5555-5555-5555-555555555555',
  encode(digest('DEVKEY-LAB-01', 'sha256'), 'hex')
);
