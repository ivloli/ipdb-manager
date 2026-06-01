package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"

	"ipdb-manager/config"
	"ipdb-manager/store"
	"ipdb-manager/watcher"
)

type Server struct {
	ListenAddr  string
	Token       string
	Watcher     *watcher.VersionWatcher
	Store       *store.PGStore
	NacosClient config_client.IConfigClient
	Cfg         *config.Config

	downstreamMu sync.RWMutex
	downstream   map[string]map[string]DownstreamStatus
}

type DownstreamStatus struct {
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
}

func (s *Server) Start() error {
	s.downstream = make(map[string]map[string]DownstreamStatus)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleHealthz)
	mux.HandleFunc("/api/v1/reconcile", s.handleReconcile)
	mux.HandleFunc("/api/v1/heartbeat", s.handleHeartbeat)
	mux.HandleFunc("/api/v1/agents/status", s.handleAgentsStatus)
	mux.HandleFunc("/api/v1/agents/policy", s.handleAgentsPolicy)
	mux.HandleFunc("/api/v1/submap/policy", s.handleSubmapPolicy)
	mux.HandleFunc("/api/v1/xdb/versions", s.handleXDBVersions)
	mux.HandleFunc("/api/v1/xdb/target", s.handleXDBTarget)
	mux.HandleFunc("/api/v1/xdb/rollback", s.handleXDBRollback)
	mux.HandleFunc("/api/v1/submap/publish", s.handleSubmapPublish)
	mux.HandleFunc("/api/v1/submap/rollback", s.handleSubmapRollback)

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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
