package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

func (s *Server) handleSubmapCurrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	if s.NacosClient == nil || s.Cfg == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "nacos not available"})
		return
	}

	type familyStatus struct {
		DataID    string `json:"data_id"`
		Version   string `json:"version"`
		UpdatedAt string `json:"updated_at"`
	}

	families := []struct {
		name   string
		dataID string
	}{
		{"v4", s.Cfg.Nacos.DataID},
		{"v6", s.Cfg.Nacos.DataIDV6},
	}

	result := make(map[string]any, len(families))
	for _, f := range families {
		metaDataID := f.dataID + "_meta"
		content, err := s.NacosClient.GetConfig(vo.ConfigParam{
			DataId: metaDataID,
			Group:  s.Cfg.Nacos.Group,
		})
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "config data not exist") {
				result[f.name] = familyStatus{DataID: f.dataID, Version: "", UpdatedAt: ""}
				continue
			}
			log.Printf("[api] get submap meta %s: %v", metaDataID, err)
			result[f.name] = familyStatus{DataID: f.dataID, Version: "error", UpdatedAt: ""}
			continue
		}
		var meta struct {
			Version   string `json:"version"`
			UpdatedAt string `json:"updated_at"`
		}
		if strings.TrimSpace(content) != "" {
			_ = json.Unmarshal([]byte(content), &meta)
		}
		result[f.name] = familyStatus{DataID: f.dataID, Version: meta.Version, UpdatedAt: meta.UpdatedAt}
	}

	writeJSON(w, http.StatusOK, result)
}

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

	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if req.Version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "version is required"})
		return
	}

	go func() {
		if err := s.Watcher.SyncSubnetMapByTag(req.Version); err != nil {
			log.Printf("[api] submap publish version=%s failed: %v", req.Version, err)
		} else {
			log.Printf("[api] submap publish version=%s completed", req.Version)
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "accepted",
		"version": req.Version,
	})
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
	if s.Watcher == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "watcher not ready"})
		return
	}

	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if req.Version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "version is required"})
		return
	}

	go func() {
		if err := s.Watcher.SyncSubnetMapByTag(req.Version); err != nil {
			log.Printf("[api] submap rollback version=%s failed: %v", req.Version, err)
		} else {
			log.Printf("[api] submap rollback version=%s completed", req.Version)
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "accepted",
		"version": req.Version,
		"action":  "rollback",
	})
}
