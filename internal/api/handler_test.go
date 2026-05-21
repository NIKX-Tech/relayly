package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NIKX-Tech/relayly/internal/api"
	"github.com/NIKX-Tech/relayly/internal/database"
	"github.com/NIKX-Tech/relayly/internal/relay"
	"go.uber.org/zap"
)

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	hub := relay.NewHub(zap.NewNop())
	return api.New(db, hub, zap.NewNop(), "test")
}

// ── POST /api/v1/pair ─────────────────────────────────────────────────────────

func TestHandlePair_OK(t *testing.T) {
	h := newTestHandler(t)

	body := bytes.NewBufferString(`{"name":"test-device"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pair", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		DeviceID  string `json:"device_id"`
		PairToken string `json:"pair_token"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.DeviceID == "" {
		t.Error("expected non-empty device_id")
	}
	if resp.PairToken == "" {
		t.Error("expected non-empty pair_token")
	}
}

func TestHandlePair_MissingName(t *testing.T) {
	h := newTestHandler(t)

	body := bytes.NewBufferString(`{"name":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pair", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandlePair_BadJSON(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pair", bytes.NewBufferString(`not-json`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ── GET /api/v1/devices ───────────────────────────────────────────────────────

func TestHandleListDevices_Empty(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var devices []any
	if err := json.NewDecoder(rr.Body).Decode(&devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 0 {
		t.Errorf("expected empty list, got %d", len(devices))
	}
}

func TestHandleListDevices_AfterPair(t *testing.T) {
	h := newTestHandler(t)

	// Register a device first
	body := bytes.NewBufferString(`{"name":"my-device"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pair", body)
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(httptest.NewRecorder(), req)

	// Now list
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var devices []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].Name != "my-device" {
		t.Errorf("unexpected devices: %+v", devices)
	}
}

// ── GET /api/v1/health ────────────────────────────────────────────────────────

func TestHandleHealth(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "ok" {
		t.Errorf("expected status ok, got %q", resp.Status)
	}
	if resp.Version != "test" {
		t.Errorf("expected version test, got %q", resp.Version)
	}
}

// ── CORS ──────────────────────────────────────────────────────────────────────

func TestCORSHeaders(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("expected CORS origin *, got %q", got)
	}
}

func TestCORSPreflight(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/pair", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS, got %d", rr.Code)
	}
}
