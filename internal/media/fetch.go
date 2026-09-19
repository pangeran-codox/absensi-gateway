package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxPhotoBytes membatasi ukuran foto yang diterima dari Laravel — jaga-
// jaga kalau photo_url ternyata mengarah ke sesuatu yang jauh lebih besar
// dari foto profil wajar (rusak, salah konfigurasi, atau disalahgunakan)
// supaya tidak menghabiskan memori gateway.
//
// Var (bukan const) supaya unit test bisa mengecilkan nilainya sementara
// — tidak realistis membuat file test 10 MB+ hanya untuk menguji kasus
// "kelebihan ukuran".
var maxPhotoBytes int64 = 10 * 1024 * 1024 // 10 MB

// fetchClient PUNYA timeout eksplisit — foto yang lambat/macet diambil
// dari Laravel tidak boleh menahan request kiosk/browser tanpa batas.
var fetchClient = &http.Client{Timeout: 10 * time.Second}

// FetchPhoto mengambil foto dari photoURL (nilai people_ref.photo_url,
// URL lengkap yang HANYA bisa diakses dari jaringan Docker internal yang
// sama — lihat spesifikasi tim Laravel). Dipanggil server-to-server dari
// gateway, BUKAN diteruskan langsung ke browser.
func FetchPhoto(ctx context.Context, photoURL string) (data []byte, contentType string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, photoURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("gagal menyusun request foto: %w", err)
	}

	resp, err := fetchClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("gagal menghubungi %s: %w", photoURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("Laravel membalas status %d untuk %s", resp.StatusCode, photoURL)
	}

	// io.LimitReader: berhenti membaca setelah maxPhotoBytes, TIDAK
	// mengembalikan error otomatis — dicek manual di bawah supaya bisa
	// membedakan "foto pas segede itu" vs "kepotong karena kebesaran".
	limited := io.LimitReader(resp.Body, maxPhotoBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", fmt.Errorf("gagal membaca isi foto: %w", err)
	}
	if int64(len(body)) > maxPhotoBytes {
		return nil, "", fmt.Errorf("foto dari %s melebihi batas %d bytes", photoURL, maxPhotoBytes)
	}

	contentType = resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return body, contentType, nil
}
