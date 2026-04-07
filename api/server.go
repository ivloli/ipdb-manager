package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"ipdb-manager/watcher"
)

type Server struct {
	ListenAddr string
	Token      string
	Watcher    *watcher.VersionWatcher
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/v1/reconcile", s.handleReconcile)

	log.Printf("[api] listening on %s", s.ListenAddr)
	return http.ListenAndServe(s.ListenAddr, mux)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleReconcile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if s.Watcher == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "watcher not ready"})
		return
	}
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}

	started := s.Watcher.TryStartBackground("manual")
	if !started {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "reconcile already running"})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "ok",
		"trigger": "manual",
	})
}

func (s *Server) authorized(r *http.Request) bool {
	if s.Token == "" {
		return true
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	prefix := "Bearer "
	if !strings.HasPrefix(authz, prefix) {
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authz, prefix))
	return token == s.Token
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
