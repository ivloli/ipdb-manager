package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"ipdb-manager/store"
)

type heartbeatRequest struct {
	AgentID        string                       `json:"agent_id"`
	System         string                       `json:"system"`
	GoalVersion    string                       `json:"goal_version"`
	CurrentVersion string                       `json:"current_version"`
	Status         string                       `json:"status"`
	LastError      string                       `json:"last_error"`
	Downstream     map[string]DownstreamStatus  `json:"downstream"`
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	var req heartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if req.AgentID == "" || req.System == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "agent_id and system are required"})
		return
	}

	if s.Store != nil {
		ns := store.NodeStatus{
			AgentID:        req.AgentID,
			System:         req.System,
			GoalVersion:    req.GoalVersion,
			CurrentVersion: req.CurrentVersion,
			Status:         req.Status,
			LastError:      req.LastError,
		}
		if err := s.Store.UpsertNodeStatus(r.Context(), ns); err != nil {
			log.Printf("[api] heartbeat upsert failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal error"})
			return
		}
	}

	if len(req.Downstream) > 0 {
		s.downstreamMu.Lock()
		s.downstream[req.AgentID] = req.Downstream
		s.downstreamMu.Unlock()
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

type agentStatusResponse struct {
	AgentID        string                      `json:"agent_id"`
	System         string                      `json:"system"`
	LastSeenAt     time.Time                   `json:"last_seen_at"`
	GoalVersion    string                      `json:"goal_version"`
	CurrentVersion string                      `json:"current_version"`
	Status         string                      `json:"status"`
	LastError      string                      `json:"last_error"`
	Online         bool                        `json:"online"`
	Downstream     map[string]DownstreamStatus `json:"downstream,omitempty"`
}

func (s *Server) handleAgentsStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	if s.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "store not available"})
		return
	}

	system := r.URL.Query().Get("system")
	nodes, err := s.Store.ListNodeStatus(r.Context(), system)
	if err != nil {
		log.Printf("[api] list agents status failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal error"})
		return
	}

	s.downstreamMu.RLock()
	defer s.downstreamMu.RUnlock()

	now := time.Now()
	results := make([]agentStatusResponse, 0, len(nodes))
	for _, ns := range nodes {
		resp := agentStatusResponse{
			AgentID:        ns.AgentID,
			System:         ns.System,
			LastSeenAt:     ns.LastSeenAt,
			GoalVersion:    ns.GoalVersion,
			CurrentVersion: ns.CurrentVersion,
			Status:         ns.Status,
			LastError:      ns.LastError,
			Online:         now.Sub(ns.LastSeenAt) < 90*time.Second,
			Downstream:     s.downstream[ns.AgentID],
		}
		results = append(results, resp)
	}
	writeJSON(w, http.StatusOK, results)
}
