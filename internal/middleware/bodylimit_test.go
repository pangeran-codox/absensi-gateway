package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaxBodySize_RejectsOversizedBody(t *testing.T) {
	const limit = 100 // 100 bytes, sengaja kecil biar gampang ditest

	var decodeErr error
	handler := MaxBodySize(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var dst map[string]string
		decodeErr = json.NewDecoder(r.Body).Decode(&dst)
	}))

	// Body sengaja jauh lebih besar dari limit.
	oversized := `{"data":"` + strings.Repeat("x", 500) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(oversized))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if decodeErr == nil {
		t.Fatal("harusnya gagal decode karena body melebihi limit, tapi malah sukses")
	}

	var maxBytesErr *http.MaxBytesError
	if !errors.As(decodeErr, &maxBytesErr) {
		t.Fatalf("errornya bukan *http.MaxBytesError, dapat: %v", decodeErr)
	}
	t.Logf("OK — body ditolak sesuai limit (%d bytes): %v", maxBytesErr.Limit, decodeErr)
}

func TestMaxBodySize_AllowsBodyWithinLimit(t *testing.T) {
	const limit = 1024

	var decodeErr error
	var got map[string]string
	handler := MaxBodySize(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeErr = json.NewDecoder(r.Body).Decode(&got)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"method":"rfid"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if decodeErr != nil {
		t.Fatalf("body kecil harusnya lolos, malah error: %v", decodeErr)
	}
	if got["method"] != "rfid" {
		t.Fatalf("hasil decode tidak sesuai, dapat: %+v", got)
	}
}
