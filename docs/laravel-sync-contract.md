# Kontrak Sinkronisasi Data: Laravel → Absensi Gateway

Dokumen ini adalah **spesifikasi lengkap** 3 endpoint yang harus dibuat
di sisi Laravel (Eduzone) supaya `internal/sync` di gateway ini bisa
mengisi `schools_ref`, `people_ref`, dan `schedules_ref` secara otomatis
— menggantikan pengisian manual yang selama ini dipakai untuk testing.

Gateway ini **menjemput** data (pull), bukan menunggu dikirimi — jadi
sisi Laravel cukup menyediakan endpoint **baca**, tidak perlu memanggil
apa-apa ke gateway.

## Ringkasan Perilaku Gateway

- Setiap `SYNC_INTERVAL` (default 5 menit), gateway memanggil 3 endpoint
  di bawah secara berurutan: **schools dulu, baru people, baru
  schedules** (schools duluan karena `people_ref`/`schedules_ref` punya
  foreign key ke `schools_ref`).
- Panggilan pertama kali (belum pernah sync sebelumnya) TIDAK mengirim
  parameter `updated_since` — artinya: **kembalikan SEMUA data**.
  Panggilan berikutnya mengirim `updated_since` = waktu sync sukses
  terakhir, supaya Laravel cukup mengembalikan yang berubah saja.
- Gateway melakukan **UPSERT** (insert kalau baru, update kalau sudah
  ada) berdasarkan ID — jadi endpoint Laravel AMAN mengembalikan record
  yang sama berkali-kali (tidak akan jadi duplikat di sisi gateway).
- Kalau salah satu panggilan gagal (Laravel down, network error,
  response tidak valid), gateway **tidak crash** — cukup dicatat sebagai
  gagal, dicoba lagi di siklus berikutnya. Check-in tetap berfungsi
  normal walau sinkronisasi sedang bermasalah.

## Autentikasi

Semua request dari gateway membawa header:
```
X-Sync-Token: <isi env LARAVEL_SYNC_TOKEN gateway>
```

Ini **BUKAN** JWT_SECRET yang dipakai untuk token login guru — sengaja
dipisah jadi shared-secret sendiri khusus komunikasi server-to-server
ini, supaya kalau salah satu bocor, yang lain tidak ikut kena. Di sisi
Laravel, buat middleware sederhana yang membandingkan header ini dengan
env `ABSENSI_SYNC_TOKEN` (nama bebas, harus SAMA PERSIS dengan
`LARAVEL_SYNC_TOKEN` di `.env` gateway). Gunakan `hash_equals()` untuk
membandingkan, bukan `===`, supaya tidak rentan timing attack.

## Parameter Query (sama untuk ketiga endpoint)

| Param | Wajib | Contoh | Keterangan |
|---|---|---|---|
| `page` | ya | `1` | Halaman ke berapa, mulai dari 1 |
| `per_page` | ya | `500` | Gateway selalu minta 500. Kembalikan PERSIS sejumlah ini per halaman kalau datanya masih ada; kembalikan LEBIH SEDIKIT dari ini kalau ini halaman terakhir — itu jadi sinyal gateway berhenti minta halaman berikutnya |
| `updated_since` | tidak | `2026-07-26T10:00:00Z` (RFC3339) | Kalau ada, filter hanya record yang kolom `updated_at`-nya lebih baru dari ini. Kalau tidak ada param ini sama sekali, kembalikan SEMUA record |

**Response selalu array JSON polos** (bukan dibungkus `{"data": [...]}`),
langsung `[ {...}, {...} ]` — atau `[]` kalau tidak ada data.

## Soal Data yang Dihapus/Dinonaktifkan

Gateway **tidak pernah menghapus baris** dari cache-nya sendiri lewat
sinkronisasi ini (tidak ada endpoint "delete"). Kalau seorang siswa
keluar sekolah atau jadwal dibatalkan, kembalikan record itu dengan
**`is_active: false`** — gateway akan meng-update baris itu jadi
nonaktif, bukan menghapusnya. Pastikan `updated_at` ikut ter-update
saat status aktif berubah, supaya perubahan ini ikut terjemput di
siklus sync berikutnya (bukan cuma pas full-sync pertama).

---

## 1. `GET /api/internal/sync/schools`

Data sekolah untuk geofencing GPS check-in guru, dan untuk menentukan
status Terlambat pada agregasi absen harian.

**Contoh response:**
```json
[
  {
    "school_id": "11111111-1111-1111-1111-111111111111",
    "name": "SMA Test",
    "latitude": -7.7956,
    "longitude": 113.7108,
    "geofence_radius_meters": 150,
    "late_cutoff_time": "07:15:00",
    "is_active": true,
    "updated_at": "2026-07-20T08:30:00+07:00"
  }
]
```

| Field | Tipe | Keterangan |
|---|---|---|
| `school_id` | string (UUID) | Wajib, harus sama dengan `schools.id` |
| `name` | string | Wajib |
| `latitude`, `longitude` | number | Wajib, titik pusat sekolah |
| `geofence_radius_meters` | integer | Wajib, radius toleransi GPS dalam meter |
| `late_cutoff_time` | string `"HH:MM:SS"` atau `null` | Opsional — batas jam masuk sebelum dianggap Terlambat. Kirim `null` kalau sekolah belum mengatur jam masuk di Laravel; gateway TIDAK akan menandai siapa pun Terlambat sampai field ini terisi (aman, tidak salah label) |
| `is_active` | boolean | Wajib |
| `updated_at` | string (RFC3339) | Wajib — dipakai gateway sebagai watermark sync berikutnya |

**Kenapa bukan diatur manual di gateway:** supaya admin cuma perlu
mengatur jam masuk sekolah **1 kali, di 1 tempat** (Laravel) — nilainya
mengalir otomatis ke gateway lewat sinkronisasi berkala ini, gateway
tidak pernah punya kolom yang harus diisi manual terpisah dari Laravel.

## 2. `GET /api/internal/sync/people`

Data siswa, guru, dan staff — digabung jadi satu endpoint (bukan 3
endpoint terpisah), dibedakan lewat field `person_type`.

**Contoh response:**
```json
[
  {
    "person_id": "22222222-2222-2222-2222-222222222222",
    "school_id": "11111111-1111-1111-1111-111111111111",
    "person_type": "student",
    "full_name": "Andi Saputra",
    "photo_url": "https://cdn.eduzone.id/photos/andi.jpg",
    "class_id": "33333333-3333-3333-3333-333333333333",
    "grade": "10",
    "is_active": true,
    "updated_at": "2026-07-20T08:30:00+07:00"
  },
  {
    "person_id": "44444444-4444-4444-4444-444444444444",
    "school_id": "11111111-1111-1111-1111-111111111111",
    "person_type": "teacher",
    "full_name": "Budi Guru",
    "photo_url": null,
    "class_id": null,
    "grade": null,
    "is_active": true,
    "updated_at": "2026-07-19T14:00:00+07:00"
  }
]
```

| Field | Tipe | Keterangan |
|---|---|---|
| `person_id` | string (UUID) | Wajib — sama dengan `students.id` / `teachers.id` / `staff.id` sesuai `person_type` |
| `school_id` | string (UUID) | Wajib |
| `person_type` | string | Wajib, HARUS salah satu dari: `student`, `teacher`, `staff` |
| `full_name` | string | Wajib |
| `photo_url` | string atau `null` | Opsional — kirim `null` (bukan string kosong `""`) kalau belum ada foto |
| `class_id` | string (UUID) atau `null` | Wajib untuk `student`, `null` untuk `teacher`/`staff` |
| `grade` | string atau `null` | Opsional, biasanya cuma relevan untuk `student` |
| `is_active` | boolean | Wajib |
| `updated_at` | string (RFC3339) | Wajib |

**PENTING soal `person_id` unik:** kombinasi `(person_id, person_type)`
harus unik. Kalau di database Eduzone `students.id` dan `teachers.id`
memang dari tabel terpisah (jadi bisa saja angkanya "kebetulan sama"),
itu TIDAK masalah — gateway membedakan berdasarkan `person_type` juga,
bukan `person_id` saja.

## 3. `GET /api/internal/sync/schedules`

Jadwal pelajaran, dipakai gateway untuk auto-mendeteksi jam pelajaran
aktif saat device RFID/QR di ruang kelas tertentu men-scan.

**Contoh response:**
```json
[
  {
    "schedule_id": "55555555-5555-5555-5555-555555555555",
    "school_id": "11111111-1111-1111-1111-111111111111",
    "class_id": "33333333-3333-3333-3333-333333333333",
    "subject_name": "Matematika",
    "teacher_id": "44444444-4444-4444-4444-444444444444",
    "day_of_week": 1,
    "start_time": "07:00:00",
    "end_time": "08:30:00",
    "is_active": true,
    "updated_at": "2026-07-15T09:00:00+07:00"
  }
]
```

| Field | Tipe | Keterangan |
|---|---|---|
| `schedule_id` | string (UUID) | Wajib |
| `school_id` | string (UUID) | Wajib |
| `class_id` | string (UUID) | Wajib |
| `subject_name` | string | Wajib |
| `teacher_id` | string (UUID) | Wajib |
| `day_of_week` | integer | Wajib, **1 = Senin ... 7 = Minggu** (ISO 8601, BUKAN 0=Minggu ala sebagian sistem lain — pastikan konversinya benar kalau di Eduzone pakai konvensi lain) |
| `start_time`, `end_time` | string `"HH:MM:SS"` | Wajib, jam-dalam-hari (bukan timestamp lengkap, karena berulang tiap minggu) |
| `is_active` | boolean | Wajib |
| `updated_at` | string (RFC3339) | Wajib |

---

## Contoh Kerangka Implementasi Laravel

Ini KERANGKA, bukan kode jadi — sesuaikan nama model/kolom dengan
skema Eduzone yang sebenarnya (nama tabel/kolom di contoh ini cuma
tebakan wajar).

**`routes/api.php`:**
```php
Route::middleware('sync.token')->prefix('internal/sync')->group(function () {
    Route::get('/schools', [SyncController::class, 'schools']);
    Route::get('/people', [SyncController::class, 'people']);
    Route::get('/schedules', [SyncController::class, 'schedules']);
});
```

**Middleware `sync.token`** (`app/Http/Middleware/VerifySyncToken.php`):
```php
public function handle($request, Closure $next)
{
    $expected = config('services.absensi_gateway.sync_token'); // dari env ABSENSI_SYNC_TOKEN
    $given = $request->header('X-Sync-Token', '');

    if (!$expected || !hash_equals($expected, $given)) {
        return response()->json(['message' => 'Unauthorized'], 401);
    }

    return $next($request);
}
```

**`app/Http/Controllers/SyncController.php`** (contoh untuk `schools`,
pola yang sama dipakai untuk `people` dan `schedules`):
```php
public function schools(Request $request)
{
    $query = School::query();

    if ($request->filled('updated_since')) {
        $query->where('updated_at', '>', $request->query('updated_since'));
    }

    $schools = $query
        ->orderBy('updated_at') // penting: urutkan ASC supaya konsisten antar halaman
        ->paginate(
            perPage: (int) $request->query('per_page', 500),
            page: (int) $request->query('page', 1)
        );

    return response()->json(
        $schools->getCollection()->map(fn ($s) => [
            'school_id'              => $s->id,
            'name'                   => $s->name,
            'latitude'               => (float) $s->latitude,
            'longitude'              => (float) $s->longitude,
            'geofence_radius_meters' => $s->geofence_radius_meters ?? 150,
            'late_cutoff_time'       => $s->late_cutoff_time, // null kalau belum diatur, jangan kasih default
            'is_active'              => (bool) $s->is_active,
            'updated_at'             => $s->updated_at->toRfc3339String(),
        ])
    );
}
```

**Catatan `orderBy('updated_at')`:** ini penting kalau data di satu
halaman berubah SAAT proses paging berlangsung (misal ada yang update
data pas gateway lagi di tengah menjemput halaman 2 dari 3) — dengan
urutan ASC yang konsisten, risiko "kelewat" 1 baris karena pergeseran
halaman jauh lebih kecil dibanding tanpa `orderBy` sama sekali.

## Cara Testing Cepat

Setelah endpoint Laravel-nya jadi, tes manual dulu sebelum aktifin di
gateway:
```bash
curl -H "X-Sync-Token: <token>" "http://eduzone-app/api/internal/sync/schools?page=1&per_page=500"
```
Harus balikin JSON array (bisa kosong `[]`, yang penting formatnya
benar dan status 200).

## Mengaktifkan di Gateway

Setelah endpoint Laravel siap dan sudah dites manual, isi `.env`
gateway:
```
SYNC_ENABLED=true
LARAVEL_SYNC_URL=http://eduzone_app:80
LARAVEL_SYNC_TOKEN=<samain dengan ABSENSI_SYNC_TOKEN di .env Laravel>
SYNC_INTERVAL=5m
```
`LARAVEL_SYNC_URL` pakai hostname container Laravel (bukan
`eduzone.local`/NPM) — panggilan ini server-to-server langsung antar
container Docker, tidak perlu lewat reverse proxy.

**PENTING — jangan tambahkan path di belakangnya.** `LARAVEL_SYNC_URL`
cuma alamat DASAR (`http://host` atau `http://host:port`), TANPA
`/api/internal/sync` di belakangnya — gateway yang otomatis
menambahkan `/api/internal/sync/schools` dkk saat memanggil. Kalau
`LARAVEL_SYNC_URL` sudah ditambahi `/api/internal/sync` sendiri,
hasilnya path dobel dan SEMUA panggilan gagal 404:
```
SALAH:  LARAVEL_SYNC_URL=http://eduzone_app:80/api/internal/sync
BENAR:  LARAVEL_SYNC_URL=http://eduzone_app:80
```

Restart gateway, lalu pantau lognya:
```bash
docker logs -f absensi-gateway-absensi-gateway-1
```
Baris `sync schools: sukses, N record diproses` dst menandakan sync
berjalan.
