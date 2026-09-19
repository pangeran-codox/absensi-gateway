-- Device key ASLI (plaintext, buat dipakai di header X-Device-Key saat testing): DEVKEY-SURYA-GERBANG-01
-- (hash-nya di bawah ini yang disimpan ke database, bukan plaintext-nya)
INSERT INTO devices (school_id, device_code, name, device_type, location, api_key_hash, is_active)
VALUES (
  '59f422c4-4fbc-4b89-b682-6649c945f02b', 'GERBANG-01', 'RFID Gerbang Utama', 'rfid_reader', 'Gerbang Utama', '6f7d25337bcd0d133bc05a860f01ba6611a1fcf45a756b0eb29e2e28753ea925', true
)
ON CONFLICT (school_id, device_code) DO UPDATE SET api_key_hash = EXCLUDED.api_key_hash;
