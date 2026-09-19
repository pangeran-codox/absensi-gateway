# Audit Kesiapan Production — Absensi Gateway

Daftar ini buat 1 tujuan: **sebelum service ini dianggap layak
di-`docker build` ke production beneran** (bukan lagi testing/pilot),
tiap poin di bawah harus dicek dan lolos standar kesiapannya.

Format tiap poin: **Apa yang dicek** → **Standar kesiapan (lolos
kalau...)** → **Status sekarang** (aku isi berdasarkan baca kode
langsung, bukan tebakan — kalau statusnya "belum dicek", itu artinya
belum ada cara mengetahui tanpa dites langsung).

---

## A. Keamanan

| # | Yang dicek | Standar kesiapan | Status sekarang |
|---|---|---|---|
| A1 | JWT shared-secret (HS256) | Idealnya sudah pindah ke RS256 (private key di Laravel, public key di gateway) — SEBELUM production, minimal harus ada keputusan sadar "kita terima risikonya" bukan lupa | 🔴 Masih HS256, risiko sudah didokumentasikan (README Status Keamanan poin 8), belum diperbaiki |
| A2 | Semua endpoint yang butuh auth beneran dicek authnya | Coba akses tiap endpoint TANPA header auth yang benar → harus selalu 401/403, tidak ada yang lolos | 🟡 Sudah by-design (middleware dipasang di `main.go`), belum ada test otomatis yang secara eksplisit mencoba "akses tanpa auth" ke semua endpoint sekaligus |
| A3 | Rate limiting device key & endpoint lain | Percobaan brute-force ke `X-Device-Key` harus kena limit — SUDAH ADA untuk device key; endpoint lain (`checkin/teacher`, `enrollment`) BELUM ada rate limit sama sekali | 🟡 Cuma device key yang dilindungi, JWT-based endpoint belum |
| A4 | Body size limit di semua endpoint | Tidak ada endpoint yang menerima body tanpa batas ukuran | 🟢 Sudah ada di semua route (`middleware.MaxBodySize`) |
| A5 | SQL injection | Semua query pakai parameterized query (`$1, $2, ...`), TIDAK ADA string concatenation manual ke query SQL | 🟢 Sudah dicek manual — semua file `.go` yang query DB pakai placeholder, tidak ada `fmt.Sprintf` ke dalam SQL |
| A6 | Kredensial sensitif tidak pernah masuk log | `JWT_SECRET`, `LARAVEL_SYNC_TOKEN`, `credential_hash`, `api_key_hash`, isi JWT token tidak pernah di-`log.Printf` mentah | 🟡 Belum ada audit eksplisit baris-per-baris; secara desain tidak ada log yang sengaja mencetak ini, tapi belum di-grep manual untuk memastikan |
| A7 | Docker image jalan sebagai non-root user | `Dockerfile` punya instruksi `USER` (bukan root) | 🟢 **Sudah diperbaiki** — `Dockerfile` sekarang bikin user `absensi` (non-root) dan `USER absensi` sebelum `ENTRYPOINT`, dengan `--chown` supaya binary tetap bisa dieksekusi |
| A8 | Dependency vulnerability scan | `govulncheck ./...` (tool resmi Go) dijalankan, tidak ada CVE kritis pada dependency (`lib/pq`, `golang-jwt/jwt`) | 🔴 Belum pernah dijalankan sama sekali |
| A9 | `.dockerignore` ada | File `.env`, `.git`, dll tidak ikut ke build context Docker | 🔴 **BELUM ADA** file `.dockerignore` sama sekali |
| A10 | HTTPS/TLS di endpoint publik | Semua trafik dari device/HP guru ke NPM harus lewat HTTPS (bukan HTTP polos), termasuk domain production (bukan cuma `eduzone.local` testing) | 🟡 Bergantung pada konfigurasi NPM production, di luar kode gateway ini — perlu dicek terpisah saat setup domain asli |
| A11 | Enrollment endpoint tidak bisa diakses non-admin | `POST /enrollment/credentials` beneran menolak role selain `"admin"` | 🟢 Sudah ada (`RequireRole("admin")`), tapi **belum pernah dites dengan token asli** (lihat `docs/kesiapan-testing-data-asli.md`) |

---

## B. Keandalan Server (Reliability)

| # | Yang dicek | Standar kesiapan | Status sekarang |
|---|---|---|---|
| B1 | Graceful shutdown | Server berhenti dengan rapi saat menerima `SIGTERM` (mis. saat `docker stop` atau redeploy) — request yang sedang diproses diberi kesempatan selesai, bukan langsung diputus | 🟢 **Sudah diperbaiki** — `main.go` sekarang pakai `signal.NotifyContext` + `srv.Shutdown(ctx)`, request yang sedang jalan diberi waktu 15 detik untuk selesai sebelum dipaksa berhenti. Background goroutine (sync, agregasi) juga ikut berhenti lewat context yang sama, bukan berjalan sendiri-sendiri |
| B2 | Timeout HTTP server | Server punya `ReadTimeout`/`WriteTimeout`/`IdleTimeout` eksplisit, supaya koneksi lambat/macet (disengaja atau tidak — mis. serangan Slowloris) tidak menghabiskan resource server tanpa batas | 🟢 **Sudah diperbaiki** — `http.Server{}` custom dengan `ReadTimeout: 10s`, `WriteTimeout: 15s`, `IdleTimeout: 60s`. Angka-angka ini perkiraan awal wajar, belum divalidasi lewat load testing (lihat E1) |
| B3 | Health check endpoint | Ada endpoint (mis. `GET /health` atau `/healthz`) yang dipakai orkestrasi (Docker healthcheck, load balancer) buat tau apakah instance ini sehat (termasuk cek konektivitas DB) | 🔴 **BELUM ADA** endpoint ini sama sekali |
| B4 | Docker `HEALTHCHECK` | `Dockerfile` punya instruksi `HEALTHCHECK` yang manggil endpoint di atas | 🔴 Belum ada (nunggu B3 selesai dulu) |
| B5 | Database connection pool | `sql.DB` diatur `SetMaxOpenConns`, `SetMaxIdleConns`, `SetConnMaxLifetime` — supaya di bawah beban tinggi gateway tidak membuka koneksi ke Postgres tanpa batas dan menghabiskan slot koneksi Postgres | 🟢 **Sudah diperbaiki** — `internal/db/db.go` sekarang set `MaxOpenConns=25`, `MaxIdleConns=5`, `ConnMaxLifetime=30m`. Angka ini perkiraan awal wajar untuk skala pilot, BUKAN hasil load testing — perlu disesuaikan lagi setelah ada data trafik nyata |
| B6 | Perilaku saat database mati/lambat | Request tetap dapat respons yang jelas (error 500 dengan pesan wajar, timeout terkontrol) dalam waktu wajar, bukan menggantung tanpa batas | 🟡 Sebagian sudah — banyak handler pakai `r.Context()` sehingga bisa dibatalkan, tapi TIDAK ADA timeout eksplisit di level query (bergantung timeout koneksi TCP OS, bisa lama) |

---

## C. Observability (Log, Metrik, Alerting)

| # | Yang dicek | Standar kesiapan | Status sekarang |
|---|---|---|---|
| C1 | Format log | Idealnya structured logging (JSON), bukan teks bebas — supaya gampang di-parse tool log aggregator (mis. Loki, ELK) kalau nanti dipasang | 🔴 Semua masih `log.Printf` teks biasa berbahasa Indonesia — bagus untuk baca manual (`docker logs`), tapi tidak ideal untuk parsing otomatis |
| C2 | Level log | Ada pembedaan level (INFO/WARN/ERROR), bukan semua log rata sama pentingnya | 🔴 Belum ada — semua pakai `log.Printf` polos tanpa level |
| C3 | Metrik (request rate, error rate, latency) | Ada endpoint metrik (mis. `/metrics` format Prometheus) buat dipantau dashboard | 🔴 Belum ada sama sekali |
| C4 | Alerting kalau sync/agregasi gagal terus-menerus | Ada notifikasi (bukan cuma tercatat di log yang mungkin tidak pernah dibaca) kalau `sync schools: GAGAL` atau `agregasi absen: GAGAL` terjadi berkali-kali berturut-turut | 🔴 Belum ada — sekarang cuma nyangkut di `docker logs`, harus dicek manual |
| C5 | Log rotation | Log Docker tidak membengkak tak terbatas dan memenuhi disk | 🟡 Bergantung konfigurasi `docker-compose.yml`/Docker daemon (`log-driver`, `max-size`) — belum diatur eksplisit di compose file ini |

---

## D. Database & Integritas Data

| # | Yang dicek | Standar kesiapan | Status sekarang |
|---|---|---|---|
| D1 | Strategi backup database `eduzone_absensi` | Ada jadwal backup otomatis (`pg_dump` terjadwal atau snapshot volume), dan **sudah pernah dites proses restore-nya** (bukan cuma backup jalan, tapi restore juga terbukti berhasil) | 🔴 Belum ada strategi/jadwal backup sama sekali untuk database ini secara spesifik |
| D2 | Migration schema terversi | Perubahan skema ke depan (nambah kolom, dll) pakai tool migration (mis. `golang-migrate`, `goose`) yang terversi dan bisa di-rollback — BUKAN edit `absensi_schema.sql` lalu drop-recreate database (yang sekarang dipakai, aman untuk data testing, BERBAHAYA untuk data production karena datanya ikut hilang) | 🔴 Belum ada tool migration — cara update skema sekarang (drop & recreate) TIDAK BOLEH dipakai lagi begitu ada data production |
| D3 | Foreign key & constraint konsisten | Semua relasi antar tabel (`school_id`, `person_id`, dll) punya FK constraint yang benar, tidak mengandalkan disiplin aplikasi semata | 🟢 Sudah ada FK di skema (`REFERENCES ... ON DELETE CASCADE`) |
| D4 | Data sensitif terenkripsi | `face_templates` (biometrik wajah) — skema sudah mencatat "simpan ciphertext, enkripsi di application layer", TAPI enkripsi itu sendiri belum diimplementasikan (wajar, karena Face Recognition sendiri masih stub) | 🔴 Belum diimplementasikan (menunggu fitur Face Recognition dibangun) |
| D5 | Retensi data GPS check-in guru | Ada kebijakan jelas berapa lama data lokasi GPS (`latitude`/`longitude` di `attendance_events`) disimpan, dan alasan retensinya — data lokasi itu sensitif | 🔴 Belum ada kebijakan retensi tertulis |

---

## E. Performa & Skalabilitas

| # | Yang dicek | Standar kesiapan | Status sekarang |
|---|---|---|---|
| E1 | Load testing | Sudah dicoba simulasi banyak device check-in bersamaan (mis. pakai `k6`, `vegeta`, atau tool load-test lain) — minimal setara jumlah device riil di puncak jam masuk sekolah, sistem tidak crash/error dan latensinya tetap wajar | 🔴 **BELUM PERNAH SAMA SEKALI** — semua testing sejauh ini manual satu-satu |
| E2 | Index database untuk query yang sering dipakai | Query yang sering jalan (cek duplicate scan, agregasi harian, dll) sudah ada index pendukungnya | 🟢 Sudah ada beberapa index di skema (`idx_events_school_date`, `idx_events_person`, dll) — belum pernah divalidasi pakai `EXPLAIN ANALYZE` di data volume besar |
| E3 | Perilaku di bawah banyak sekolah sekaligus (multi-tenant nyata) | Sudah dites dengan 2+ sekolah aktif bersamaan dalam 1 database, bukan cuma 1 sekolah dummy | 🔴 Belum pernah dites (lihat `docs/kesiapan-testing-data-asli.md` Bagian 2 poin 1) |
| E4 | Ukuran resource container (CPU/memory limit) | `docker-compose.yml`/stack production punya `deploy.resources.limits` supaya 1 container tidak bisa menghabiskan semua resource node kalau ada bug/lonjakan trafik | 🔴 Belum diatur di `docker-compose.yml` sekarang (yang ada baru untuk testing lokal) |

---

## F. Docker & Deployment

| # | Yang dicek | Standar kesiapan | Status sekarang |
|---|---|---|---|
| F1 | Image di-tag versi, bukan `latest` | Setiap deploy production pakai tag spesifik (mis. `iswant/absensi-gateway:0.2.0`), bukan `latest` — supaya rollback ke versi tertentu bisa dilakukan | 🟡 Contoh di README sudah pakai tag versi (`0.1.0`), tapi belum ada proses formal/CI yang memaksa ini |
| F2 | Rencana rollback | Ada langkah jelas "kalau versi baru bermasalah, begini cara balik ke versi sebelumnya" — sudah didokumentasikan, bukan baru dipikirkan saat insiden terjadi | 🔴 Belum didokumentasikan |
| F3 | Environment production terpisah dari testing | `.env` production TIDAK sama dengan yang dipakai testing lokal (`JWT_SECRET`, `LARAVEL_SYNC_TOKEN` beda nilai, bukan reuse) | 🟡 Bergantung disiplin operasional saat deploy, tidak ada mekanisme teknis yang mencegah reuse |
| F4 | Secrets tidak nangkring di image | Dicek `docker history <image>` tidak ada layer yang mengandung isi `.env`/secret | 🟡 Multi-stage build sudah mengurangi risiko ini (image final cuma binary, bukan source+`.env`), tapi belum pernah divalidasi eksplisit dengan `docker history` |
| F5 | Restart policy container | `docker-compose.yml` punya `restart: unless-stopped` (atau setara) — supaya container otomatis nyala lagi kalau Docker Desktop/host restart, TANPA perlu `docker compose up -d` manual | 🟢 **Sudah diperbaiki** (ditemukan lewat insiden nyata: gateway mati diam-diam ~8 hari setelah Docker Desktop restart, karena tidak ada restart policy — container lain di infra yang sama juga banyak yang kena masalah serupa, bukan cuma gateway ini) |

---

## Insiden yang Pernah Kejadian (Log Pembelajaran)

Bagian ini nyatet kejadian nyata yang menunjukkan sebuah poin audit
BENERAN berdampak (bukan cuma teori) — supaya tidak dianggap "ah,
kayaknya nggak akan kejadian":

- **F5 (restart policy)** — gateway sempat mati total dari ~23 Agustus
  sampai ~31 Agustus TANPA ada yang sadar, karena container tidak
  otomatis nyala lagi setelah Docker Desktop restart. Selama itu,
  check-in RFID/QR dari device fisik (kalau ada yang mencoba) akan
  gagal total — bukan error yang "kelihatan", device cuma akan gagal
  konek begitu saja. Ini alasan kenapa **B3/B4 (health check
  endpoint)** di atas juga penting — tanpa health check, tidak ada cara
  otomatis mendeteksi "gateway ini mati" selain ada orang yang kebetulan
  coba pakai dan lapor.
- **H2 (integration test ke Postgres)** — bug `INSERT` 11-kolom-vs-10-
  nilai di `aggregationQuery` lolos `go build`/`go vet` bersih, baru
  ketahuan saat dieksekusi ke Postgres beneran. Bukti langsung kenapa
  "sudah `go build` hijau" tidak pernah cukup sebagai bukti kebenaran
  query SQL.

---

## G. Privasi Data (Siswa adalah Anak di Bawah Umur)

Kategori ini khusus karena sebagian besar data yang diproses (nama,
foto, lokasi GPS saat guru absen di dekat siswa, potensi data wajah
nanti) menyangkut anak-anak — standar kehati-hatiannya perlu lebih
tinggi dari sistem umum.

| # | Yang dicek | Standar kesiapan | Status sekarang |
|---|---|---|---|
| G1 | Kebijakan privasi/consent orang tua | Ada pemberitahuan/persetujuan ke orang tua/wali soal data apa yang dikumpulkan (foto, waktu kehadiran) dan buat apa | 🔴 Di luar cakupan teknis gateway ini — ini keputusan sekolah/EduZone, bukan sesuatu yang bisa "dicek di kode" |
| G2 | Hak hapus data (right to be forgotten) | Ada mekanisme kalau siswa keluar/lulus, datanya bisa dihapus/dianonimkan sesuai kebijakan yang berlaku | 🔴 Belum ada endpoint/proses buat ini |
| G3 | Foto siswa (`people_ref.photo_url`) | URL foto yang disimpan mengarah ke storage yang aksesnya terkontrol (bukan link publik yang bisa ditebak sembarang orang) | 🟢 `photo_url` dari Laravel HANYA bisa diakses dari jaringan Docker internal (bukan domain publik) — gateway proxy fotonya lewat endpoint sendiri, tidak pernah meneruskan URL Laravel mentah ke browser |
| G4 | Endpoint `GET /media/photo/{person_id}` tanpa autentikasi | Idealnya ada lapisan proteksi selain "UUID susah ditebak" — mengingat ini foto anak di bawah umur | 🟡 **Keputusan sadar** (9 Sept 2026): `<img src>` tidak bisa kirim header auth, jadi proteksinya cuma UUID + pembatasan jaringan NPM. Diterima sebagai risiko untuk sekarang, sama seperti A1 (JWT shared-secret) — perlu ditinjau ulang kalau kebutuhan keamanan foto berubah (opsi: token sementara di URL, signed URL) |

---

## H. Testing Coverage

| # | Yang dicek | Standar kesiapan | Status sekarang |
|---|---|---|---|
| H1 | Unit test | Logic murni (validasi, rate limit, watermark, dll) punya test, lolos `go test ./...` | 🟢 20+ test ada dan lolos |
| H2 | Integration test terhadap Postgres beneran | Ada test otomatis (bukan manual) yang benar-benar connect ke Postgres — men-tes query SQL yang sebenarnya (INSERT/UPDATE/JOIN), bukan cuma compile | 🔴 **BELUM ADA SAMA SEKALI** — inilah kenapa bug `INSERT` 11-kolom-vs-10-nilai bisa lolos `go build` dan baru ketahuan pas testing manual (lihat README Troubleshooting) |
| H3 | End-to-end test alur penuh | Ada test yang mensimulasikan alur nyata: device check-in → agregasi → (nanti) sync balik ke Laravel, dijalankan otomatis | 🔴 Belum ada, semua end-to-end sejauh ini manual oleh pengguna |
| H4 | Test untuk endpoint yang butuh JWT | `checkin/teacher`, `attendance/daily`, `enrollment/credentials` | 🔴 Belum ada test sama sekali (konsisten dengan status di `docs/kesiapan-testing-data-asli.md` — endpoint ini belum pernah dieksekusi sekali pun) |

---

## Ringkasan: Yang PALING Kritis Sebelum Production

### Sudah diperbaiki

- ~~**A7 — Docker container jalan sebagai root.**~~ ✅ Fixed
- ~~**B1/B2 — Tidak ada graceful shutdown & HTTP timeout.**~~ ✅ Fixed
- ~~**B5 — Connection pool database tidak diatur.**~~ ✅ Fixed
- ~~**F5 — Tidak ada restart policy.**~~ ✅ Fixed (ditemukan dari
  insiden nyata — lihat "Insiden yang Pernah Kejadian" di atas)

### Masih tersisa, diurutkan dari yang paling berisiko

1. **H2 — Tidak ada integration test ke Postgres beneran.** Terbukti
   sudah pernah menyebabkan bug production-grade lolos `go build` (bug
   agregasi SQL kemarin).
2. **D1/D2 — Belum ada strategi backup & migration terversi.** Begitu
   ada data production sungguhan, drop-recreate database (cara
   sekarang) tidak boleh dipakai lagi — dan tanpa backup, kesalahan
   apapun bisa berarti kehilangan data absen permanen.
3. **B3/B4 — Belum ada health check endpoint.** Insiden restart policy
   di atas baru ketahuan karena kebetulan dicoba dipakai — dengan
   health check + monitoring, kematian service seperti itu bisa
   terdeteksi otomatis dalam hitungan menit, bukan hitungan hari.
4. **E1 — Belum ada load testing sama sekali.** Semua asumsi "harusnya
   tahan banyak device sekaligus" masih teori dari desain (transaksi +
   lock, index), belum dibuktikan. Angka connection pool (B5) dan
   timeout (B2) yang baru diisi juga masih perkiraan awal, belum
   divalidasi dengan beban nyata.
5. **A1 — JWT masih shared-secret.** Sudah diketahui risikonya,
   keputusan buat terima risiko ini (atau memperbaikinya) harus
   dilakukan SADAR, bukan lupa.
