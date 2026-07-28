package sync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchSchools_Pagination(t *testing.T) {
	var requestedPages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPages = append(requestedPages, r.URL.Query().Get("page"))

		page := r.URL.Query().Get("page")
		w.Header().Set("Content-Type", "application/json")
		if page == "1" {
			// Halaman penuh (perPage record) -> client harus lanjut ke halaman 2.
			batch := make([]SchoolRecord, perPage)
			for i := range batch {
				batch[i] = SchoolRecord{SchoolID: "school-1", Name: "Sekolah Test", UpdatedAt: time.Now()}
			}
			json.NewEncoder(w).Encode(batch)
			return
		}
		// Halaman ke-2: lebih pendek dari perPage -> tanda halaman terakhir.
		json.NewEncoder(w).Encode([]SchoolRecord{
			{SchoolID: "school-2", Name: "Sekolah Test 2", UpdatedAt: time.Now()},
		})
	}))
	defer server.Close()

	client := NewLaravelClient(server.URL, "test-token")
	records, err := client.FetchSchools(context.Background(), nil)
	if err != nil {
		t.Fatalf("FetchSchools gagal: %v", err)
	}

	if len(records) != perPage+1 {
		t.Fatalf("mau %d record (2 halaman), dapat %d", perPage+1, len(records))
	}
	if len(requestedPages) != 2 {
		t.Fatalf("mau 2 kali request (2 halaman), dapat %d: %v", len(requestedPages), requestedPages)
	}
}

func TestFetchSchools_SendsAuthHeaderAndSinceParam(t *testing.T) {
	var gotToken, gotSince string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Sync-Token")
		gotSince = r.URL.Query().Get("updated_since")
		json.NewEncoder(w).Encode([]SchoolRecord{})
	}))
	defer server.Close()

	client := NewLaravelClient(server.URL, "rahasia-123")
	since := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	if _, err := client.FetchSchools(context.Background(), &since); err != nil {
		t.Fatalf("FetchSchools gagal: %v", err)
	}

	if gotToken != "rahasia-123" {
		t.Errorf("header X-Sync-Token mau %q, dapat %q", "rahasia-123", gotToken)
	}
	if gotSince != since.Format(time.RFC3339) {
		t.Errorf("query updated_since mau %q, dapat %q", since.Format(time.RFC3339), gotSince)
	}
}

func TestFetchSchools_NoSinceParamWhenNil(t *testing.T) {
	var sawSinceParam bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("updated_since") {
			sawSinceParam = true
		}
		json.NewEncoder(w).Encode([]SchoolRecord{})
	}))
	defer server.Close()

	client := NewLaravelClient(server.URL, "test-token")
	if _, err := client.FetchSchools(context.Background(), nil); err != nil {
		t.Fatalf("FetchSchools gagal: %v", err)
	}

	if sawSinceParam {
		t.Error("updated_since seharusnya TIDAK dikirim ketika since=nil (artinya: ambil semua data)")
	}
}

func TestFetchPeople_ErrorOnNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewLaravelClient(server.URL, "test-token")
	_, err := client.FetchPeople(context.Background(), nil)
	if err == nil {
		t.Fatal("mau error ketika Laravel membalas 500, malah sukses")
	}
}

func TestFetchSchedules_SingleShortPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]ScheduleRecord{
			{ScheduleID: "sched-1", SubjectName: "Matematika", UpdatedAt: time.Now()},
		})
	}))
	defer server.Close()

	client := NewLaravelClient(server.URL, "test-token")
	records, err := client.FetchSchedules(context.Background(), nil)
	if err != nil {
		t.Fatalf("FetchSchedules gagal: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("mau 1 record, dapat %d", len(records))
	}
}
