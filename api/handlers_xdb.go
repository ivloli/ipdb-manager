package api

import (
	"encoding/json"
	"log"
	"net/http"

	"ipdb-manager/goal"
)

func (s *Server) handleXDBVersions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	// TODO: list known versions from Nacos or local state
	writeJSON(w, http.StatusOK, map[string]any{"versions": []string{}})
}

func (s *Server) handleXDBTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	if s.NacosClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "nacos not available"})
		return
	}

	var req struct {
		System  string `json:"system"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if req.System == "" || req.Version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "system and version are required"})
		return
	}

	if err := goal.PublishGoal(s.NacosClient, req.System, req.Version); err != nil {
		log.Printf("[api] publish goal failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "publish goal failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "system": req.System, "version": req.Version})
}

func (s *Server) handleXDBRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	if s.NacosClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "nacos not available"})
		return
	}

	var req struct {
		System  string `json:"system"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if req.System == "" || req.Version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "system and version are required"})
		return
	}

	if err := goal.PublishGoal(s.NacosClient, req.System, req.Version); err != nil {
		log.Printf("[api] rollback goal failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "rollback failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "system": req.System, "version": req.Version})
}
