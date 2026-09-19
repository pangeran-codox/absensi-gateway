package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// perPage adalah ukuran halaman saat menjemput data dari Laravel. 500
// dipilih sebagai kompromi — cukup besar supaya jarang perlu banyak
// halaman untuk sekolah berukuran wajar, cukup kecil supaya 1 response
// tidak terlalu besar untuk endpoint yang belum tentu di-paginate secara
// efisien di sisi Laravel.
const perPage = 500

// LaravelClient menjemput data schools/people/schedules dari API Laravel.
// Autentikasi pakai header X-Sync-Token (shared secret KHUSUS untuk
// komunikasi server-to-server ini — SENGAJA BUKAN JWT_SECRET yang dipakai
// untuk token guru, supaya 2 keperluan berbeda punya "kunci" berbeda:
// kalau salah satu bocor, yang lain tidak ikut kena).
type LaravelClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewLaravelClient(baseURL, token string) *LaravelClient {
	return &LaravelClient{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// FetchSchools menjemput SEMUA sekolah yang updated_at-nya lebih baru
// dari `since` (nil berarti: belum pernah sync, ambil semua).
func (c *LaravelClient) FetchSchools(ctx context.Context, since *time.Time) ([]SchoolRecord, error) {
	var all []SchoolRecord
	page := 1
	for {
		var batch []SchoolRecord
		if err := c.fetchPage(ctx, "/api/internal/sync/schools", since, page, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < perPage {
			return all, nil
		}
		page++
	}
}

// FetchPeople menjemput SEMUA siswa/guru/staff yang updated_at-nya lebih
// baru dari `since`.
func (c *LaravelClient) FetchPeople(ctx context.Context, since *time.Time) ([]PersonRecord, error) {
	var all []PersonRecord
	page := 1
	for {
		var batch []PersonRecord
		if err := c.fetchPage(ctx, "/api/internal/sync/people", since, page, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < perPage {
			return all, nil
		}
		page++
	}
}

// FetchSchedules menjemput SEMUA jadwal yang updated_at-nya lebih baru
// dari `since`.
func (c *LaravelClient) FetchSchedules(ctx context.Context, since *time.Time) ([]ScheduleRecord, error) {
	var all []ScheduleRecord
	page := 1
	for {
		var batch []ScheduleRecord
		if err := c.fetchPage(ctx, "/api/internal/sync/schedules", since, page, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < perPage {
			return all, nil
		}
		page++
	}
}

// fetchPage mengambil 1 halaman dari endpoint Laravel dan men-decode JSON
// array-nya ke dst (pointer ke slice). Dipanggil berulang oleh
// Fetch<Resource> di atas sampai halaman yang dikembalikan lebih pendek
// dari perPage (tandanya sudah halaman terakhir).
func (c *LaravelClient) fetchPage(ctx context.Context, path string, since *time.Time, page int, dst interface{}) error {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("URL Laravel sync tidak valid: %w", err)
	}

	q := u.Query()
	q.Set("page", strconv.Itoa(page))
	q.Set("per_page", strconv.Itoa(perPage))
	if since != nil {
		q.Set("updated_since", since.Format(time.RFC3339))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("gagal menyusun request: %w", err)
	}
	req.Header.Set("X-Sync-Token", c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gagal menghubungi Laravel (%s): %w", u.String(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Laravel membalas status %d untuk %s", resp.StatusCode, u.String())
	}

	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("gagal membaca response JSON dari %s: %w", path, err)
	}
	return nil
}
