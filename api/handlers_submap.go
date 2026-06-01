package api

import (
	"net/http"
)

func (s *Server) handleSubmapPublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	if s.Watcher == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "watcher not ready"})
		return
	}

	started := s.Watcher.TryStartBackground("submap_publish")
	if !started {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "reconcile already running"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "ok", "trigger": "submap_publish"})
}

func (s *Server) handleSubmapRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	// TODO: implement submap rollback logic (revert subnet_map_meta to previous version)
	writeJSON(w, http.StatusNotImplemented, map[string]any{"error": "not implemented yet"})
}
