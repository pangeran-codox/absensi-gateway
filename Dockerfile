# Tahap 1: build binary Go
FROM golang:1.22-alpine AS builder

WORKDIR /build

# Copy go.mod/go.sum dulu terpisah supaya layer download dependency di-cache
# Docker — kalau cuma source code yang berubah (bukan dependency), build
# ulang jadi jauh lebih cepat karena tidak perlu download ulang.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0: paksa static binary murni Go (driver postgres yang dipakai,
# lib/pq, memang pure Go — tidak butuh cgo). Binary static bisa jalan di
# image runtime paling minimal (di sini: alpine tanpa perlu library C tambahan).
RUN CGO_ENABLED=0 GOOS=linux go build -o /build/absensi-gateway .

# Tahap 2: image runtime, sengaja seminim mungkin (bukan bawa toolchain Go)
FROM alpine:3.20

# CATATAN: sengaja TIDAK install paket "tzdata" di sini. Binary Go-nya
# sudah membawa database zoneinfo IANA sendiri lewat `import _ "time/tzdata"`
# di main.go, jadi TZ=Asia/Jakarta tetap valid tanpa paket OS ini.
# "ca-certificates" juga belum dibutuhkan selama gateway ini cuma konek ke
# Postgres (sslmode=disable, bukan HTTPS). Kalau nanti gateway ini perlu
# manggil layanan lain lewat HTTPS (mis. worker face-recognition), baru
# tambahkan lagi baris `RUN apk add --no-cache ca-certificates` di sini.

WORKDIR /app

# --chown penting: tanpa ini, file yang di-copy tetap dimiliki root,
# dan user "absensi" di bawah bisa gagal menjalankannya (permission denied).
RUN addgroup -S absensi && adduser -S -G absensi absensi
COPY --from=builder --chown=absensi:absensi /build/absensi-gateway .

# Folder cache foto (lihat internal/media) HARUS dibuat & di-chown
# di sini, SEBELUM `USER absensi` di bawah — kalau tidak, user
# non-root tidak akan punya izin bikin folder ini sendiri saat
# runtime (root-level directory defaultnya cuma writable oleh root).
RUN mkdir -p /data/photo-cache && chown -R absensi:absensi /data

# Jalankan sebagai user biasa, BUKAN root — supaya kalau suatu saat ada
# celah keamanan yang berhasil dieksploitasi dari dalam container, akses
# penyerang tetap terbatas (tidak otomatis dapat hak admin penuh di
# dalam container).
USER absensi

EXPOSE 8080
ENTRYPOINT ["./absensi-gateway"]
