package api

import (
	"encoding/json"
	"log"
	"net/http"

	"ipdb-manager/store"
)

func (s *Server) handleAgentsPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	if s.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "store not available"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		system := r.URL.Query().Get("system")
		if system == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "system query param is required"})
			return
		}
		p, err := s.Store.GetAgentPolicy(r.Context(), system)
		if err != nil {
			log.Printf("[api] get agent policy failed: %v", err)
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "policy not found"})
			return
		}
		writeJSON(w, http.StatusOK, p)

	case http.MethodPut:
		var req struct {
			System     string `json:"system"`
			AutoUpdate bool   `json:"auto_update"`
			UpdatedBy  string `json:"updated_by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
			return
		}
		if req.System == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "system is required"})
			return
		}
		p := store.AgentPolicy{
			System:     req.System,
			AutoUpdate: req.AutoUpdate,
			UpdatedBy:  req.UpdatedBy,
		}
		if err := s.Store.SetAgentPolicy(r.Context(), p); err != nil {
			log.Printf("[api] set agent policy failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal error"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}

func (s *Server) handleSubmapPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	if s.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "store not available"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		p, err := s.Store.GetSubmapPolicy(r.Context())
		if err != nil {
			log.Printf("[api] get submap policy failed: %v", err)
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "policy not found"})
			return
		}
		writeJSON(w, http.StatusOK, p)

	case http.MethodPut:
		var req struct {
			AutoUpdate bool   `json:"auto_update"`
			UpdatedBy  string `json:"updated_by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
			return
		}
		p := store.SubmapPolicy{
			AutoUpdate: req.AutoUpdate,
			UpdatedBy:  req.UpdatedBy,
		}
		if err := s.Store.SetSubmapPolicy(r.Context(), p); err != nil {
			log.Printf("[api] set submap policy failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal error"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}
