package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/nacos-group/nacos-sdk-go/v2/vo"

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

	resp := map[string]any{}

	// Get current live version from Nacos subnet_map_meta.
	if s.NacosClient != nil && s.Cfg != nil {
		metaDataID := s.Cfg.Nacos.DataID + "_meta"
		content, err := s.NacosClient.GetConfig(vo.ConfigParam{
			DataId: metaDataID,
			Group:  s.Cfg.Nacos.Group,
		})
		if err == nil && strings.TrimSpace(content) != "" {
			var meta struct {
				Version   string `json:"version"`
				UpdatedAt string `json:"updated_at"`
			}
			if json.Unmarshal([]byte(content), &meta) == nil {
				resp["current_version"] = meta.Version
				resp["current_updated_at"] = meta.UpdatedAt
			}
		}
	}

	// List known versions from reconcile_task table (done/failed).
	if s.Store != nil {
		versions, err := s.Store.ListReconcileVersions(context.Background())
		if err != nil {
			log.Printf("[api] list reconcile versions: %v", err)
		} else {
			resp["versions"] = versions
		}
	}

	if resp["versions"] == nil {
		resp["versions"] = []string{}
	}

	writeJSON(w, http.StatusOK, resp)
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
