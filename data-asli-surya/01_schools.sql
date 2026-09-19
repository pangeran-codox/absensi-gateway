-- Sekolah asli: SMA Negeri 1 Surya Nusantara
-- late_cutoff_time default 07:15:00 -- ganti sesuai kebijakan sekolah kalau beda
INSERT INTO schools_ref (school_id, name, latitude, longitude, geofence_radius_meters, late_cutoff_time, is_active)
VALUES (
  '59f422c4-4fbc-4b89-b682-6649c945f02b', 'SMA Negeri 1 Surya Nusantara', -6.9147440, 107.6098100, 200, '07:15:00', true
)
ON CONFLICT (school_id) DO UPDATE SET
  name = EXCLUDED.name, latitude = EXCLUDED.latitude, longitude = EXCLUDED.longitude,
  geofence_radius_meters = EXCLUDED.geofence_radius_meters, late_cutoff_time = EXCLUDED.late_cutoff_time;
