package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer() *Server {
	s := &Server{
		ListenAddr: ":0",
		Token:      "test-token",
		downstream: make(map[string]map[string]DownstreamStatus),
	}
	return s
}

func TestHandleHealthz(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	s.handleHealthz(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", resp["status"])
	}
}

func TestHandleHeartbeat_Success(t *testing.T) {
	s := newTestServer()
	body := `{"agent_id":"coredns-probe-hz-0","system":"probe","goal_version":"v3.16.0","current_version":"v3.16.0","status":"ok","downstream":{"coredns":{"status":"up","latency_ms":5}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/heartbeat", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.handleHeartbeat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify downstream stored in memory.
	s.downstreamMu.RLock()
	ds, ok := s.downstream["coredns-probe-hz-0"]
	s.downstreamMu.RUnlock()
	if !ok {
		t.Fatal("downstream not stored")
	}
	if ds["coredns"].Status != "up" {
		t.Fatalf("expected downstream coredns status=up, got %s", ds["coredns"].Status)
	}
}

func TestHandleHeartbeat_MissingFields(t *testing.T) {
	s := newTestServer()
	body := `{"system":"probe"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/heartbeat", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.handleHeartbeat(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleHeartbeat_WrongMethod(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/heartbeat", nil)
	w := httptest.NewRecorder()
	s.handleHeartbeat(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHandleAgentsStatus_Unauthorized(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/status", nil)
	w := httptest.NewRecorder()
	s.handleAgentsStatus(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleXDBTarget_Unauthorized(t *testing.T) {
	s := newTestServer()
	body := `{"system":"probe","version":"v3.16.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/xdb/target", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.handleXDBTarget(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleXDBTarget_NoNacos(t *testing.T) {
	s := newTestServer()
	body := `{"system":"probe","version":"v3.16.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/xdb/target", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.handleXDBTarget(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (no nacos), got %d", w.Code)
	}
}

func TestAuthorized(t *testing.T) {
	s := newTestServer()

	tests := []struct {
		name   string
		header string
		want   bool
	}{
		{"valid token", "Bearer test-token", true},
		{"wrong token", "Bearer wrong", false},
		{"no header", "", false},
		{"no bearer prefix", "Basic test-token", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			got := s.authorized(req)
			if got != tt.want {
				t.Fatalf("authorized()=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestAuthorized_EmptyToken(t *testing.T) {
	s := &Server{Token: ""}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if !s.authorized(req) {
		t.Fatal("empty token should allow all requests")
	}
}
