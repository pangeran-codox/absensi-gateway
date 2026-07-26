package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"absensi-gateway/internal/middleware"
)

func TestGetFaceJobResult_RejectsOtherDevice(t *testing.T) {
	h := NewDeviceHandler(nil)
	h.jobStore["job_milik_device_a"] = FaceJobResult{
		Status:   "done",
		Result:   "matched",
		deviceID: "device-A",
		schoolID: "school-1",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/checkin/device/jobs/job_milik_device_a", nil)
	req.SetPathValue("job_id", "job_milik_device_a")
	// Device YANG BEDA (device-B) mencoba mengambil job milik device-A.
	ctx := context.WithValue(req.Context(), middleware.CtxDeviceID, "device-B")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.GetFaceJobResult(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("device lain harusnya dapat 404, malah dapat %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("gagal parse response: %v", err)
	}
	if body["reason"] != "job_not_found" {
		t.Fatalf("reason harusnya job_not_found, dapat: %v", body["reason"])
	}
}

func TestGetFaceJobResult_AllowsOwningDevice(t *testing.T) {
	h := NewDeviceHandler(nil)
	h.jobStore["job_milik_device_a"] = FaceJobResult{
		Status:   "done",
		Result:   "matched",
		deviceID: "device-A",
		schoolID: "school-1",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/checkin/device/jobs/job_milik_device_a", nil)
	req.SetPathValue("job_id", "job_milik_device_a")
	// Device YANG SAMA (device-A) mengambil job miliknya sendiri.
	ctx := context.WithValue(req.Context(), middleware.CtxDeviceID, "device-A")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.GetFaceJobResult(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("device pemilik harusnya dapat 200, malah dapat %d: %s", rec.Code, rec.Body.String())
	}

	var body FaceJobResult
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("gagal parse response: %v", err)
	}
	if body.Status != "done" || body.Result != "matched" {
		t.Fatalf("isi response tidak sesuai, dapat: %+v", body)
	}
}

func TestGetFaceJobResult_UnknownJobID(t *testing.T) {
	h := NewDeviceHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/checkin/device/jobs/job_ngasal", nil)
	req.SetPathValue("job_id", "job_ngasal")
	ctx := context.WithValue(req.Context(), middleware.CtxDeviceID, "device-A")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.GetFaceJobResult(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("job_id ngasal harusnya dapat 404, malah dapat %d", rec.Code)
	}
}
