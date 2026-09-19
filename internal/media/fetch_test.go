package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchPhoto_Success(t *testing.T) {
	want := []byte("isi foto asli")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(want)
	}))
	defer server.Close()

	data, contentType, err := FetchPhoto(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("FetchPhoto gagal: %v", err)
	}
	if string(data) != string(want) {
		t.Errorf("data tidak sesuai, mau %q dapat %q", want, data)
	}
	if contentType != "image/jpeg" {
		t.Errorf("content type mau image/jpeg, dapat %q", contentType)
	}
}

func TestFetchPhoto_MissingContentType_DefaultsToOctetStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set eksplisit ke string kosong -- server Go otomatis "menebak"
		// Content-Type lewat sniffing isi body kalau header ini benar-benar
		// tidak pernah di-set sama sekali, jadi utk menguji kasus "header
		// kosong" beneran, harus dipaksa begini.
		w.Header().Set("Content-Type", "")
		w.Write([]byte("data tanpa content-type"))
	}))
	defer server.Close()

	_, contentType, err := FetchPhoto(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("FetchPhoto gagal: %v", err)
	}
	if contentType != "application/octet-stream" {
		t.Errorf("content type default mau application/octet-stream, dapat %q", contentType)
	}
}

func TestFetchPhoto_ErrorOnNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, _, err := FetchPhoto(context.Background(), server.URL)
	if err == nil {
		t.Fatal("mau error ketika Laravel membalas 404, malah sukses")
	}
}

func TestFetchPhoto_ErrorWhenTooLarge(t *testing.T) {
	// Perkecil batas sementara -- tidak realistis bikin file test 10MB+.
	original := maxPhotoBytes
	maxPhotoBytes = 10
	defer func() { maxPhotoBytes = original }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 100))) // jauh lebih besar dari batas 10 bytes
	}))
	defer server.Close()

	_, _, err := FetchPhoto(context.Background(), server.URL)
	if err == nil {
		t.Fatal("mau error karena body melebihi batas, malah sukses")
	}
}

func TestFetchPhoto_WithinLimit_Success(t *testing.T) {
	original := maxPhotoBytes
	maxPhotoBytes = 100
	defer func() { maxPhotoBytes = original }()

	want := strings.Repeat("y", 50) // di bawah batas 100 bytes
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(want))
	}))
	defer server.Close()

	data, _, err := FetchPhoto(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("mau sukses karena masih di bawah batas, malah error: %v", err)
	}
	if string(data) != want {
		t.Errorf("data tidak sesuai, mau %d bytes dapat %d bytes", len(want), len(data))
	}
}
