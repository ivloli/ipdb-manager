package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

func (s *Server) handleReconcileTag(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleReconcileTagPost(w, r)
	case http.MethodGet:
		s.handleReconcileTagGet(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}

func (s *Server) handleReconcileTagPost(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	if s.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "store not available"})
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

	ctx := context.Background()

	// Check existing task state.
	existing, err := s.Store.GetReconcileTask(ctx, req.Version)
	if err != nil {
		log.Printf("[api] get reconcile task: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal error"})
		return
	}

	if existing != nil {
		switch existing.Status {
		case "running":
			writeJSON(w, http.StatusAccepted, map[string]any{
				"status":     "running",
				"version":    existing.Version,
				"pod_id":     existing.PodID,
				"started_at": existing.StartedAt,
			})
			return
		case "done":
			if existing.FinishedAt != nil && time.Since(*existing.FinishedAt) < time.Hour {
				writeJSON(w, http.StatusOK, map[string]any{
					"status":      "done",
					"version":     existing.Version,
					"finished_at": existing.FinishedAt,
					"result":      json.RawMessage(existing.Result),
				})
				return
			}
		}
	}

	// Try to acquire the task via optimistic lock.
	acquired, err := s.Store.TryAcquireReconcileTask(ctx, req.Version, s.PodID)
	if err != nil {
		log.Printf("[api] acquire reconcile task: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal error"})
		return
	}
	if !acquired {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":  "running",
			"version": req.Version,
			"message": "task acquired by another pod",
		})
		return
	}

	// Launch async reconcile.
	go s.runReconcileByTag(req.Version)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "accepted",
		"version": req.Version,
		"pod_id":  s.PodID,
	})
}

func (s *Server) handleReconcileTagGet(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	if s.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "store not available"})
		return
	}

	version := r.URL.Query().Get("version")
	if version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "version query param required"})
		return
	}

	task, err := s.Store.GetReconcileTask(context.Background(), version)
	if err != nil {
		log.Printf("[api] get reconcile task: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal error"})
		return
	}
	if task == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no task found for this version"})
		return
	}

	resp := map[string]any{
		"version":    task.Version,
		"status":     task.Status,
		"pod_id":     task.PodID,
		"started_at": task.StartedAt,
		"updated_at": task.UpdatedAt,
	}
	if task.FinishedAt != nil {
		resp["finished_at"] = task.FinishedAt
	}
	if task.Result != nil {
		resp["result"] = json.RawMessage(task.Result)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleXDBStatus(w http.ResponseWriter, r *http.Request) {
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

	task, err := s.Store.GetLatestReconcileTask(context.Background())
	if err != nil {
		log.Printf("[api] get latest reconcile task: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal error"})
		return
	}

	resp := map[string]any{
		"pod_id": s.PodID,
	}
	if task != nil {
		resp["latest_reconcile"] = map[string]any{
			"version":     task.Version,
			"status":      task.Status,
			"started_at":  task.StartedAt,
			"finished_at": task.FinishedAt,
			"updated_at":  task.UpdatedAt,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) runReconcileByTag(version string) {
	ctx := context.Background()

	// Heartbeat goroutine to keep the lock alive.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_ = s.Store.HeartbeatReconcileTask(ctx, version, s.PodID)
			}
		}
	}()

	result := s.Watcher.ReconcileByTag(version)
	close(done)

	resultJSON, _ := json.Marshal(result)

	if result.Error != "" {
		if err := s.Store.FailReconcileTask(ctx, version, s.PodID, resultJSON); err != nil {
			log.Printf("[api] fail reconcile task %s: %v", version, err)
		}
		log.Printf("[api] reconcile-tag=%s failed: %s", version, result.Error)
		return
	}

	if err := s.Store.CompleteReconcileTask(ctx, version, s.PodID, resultJSON); err != nil {
		log.Printf("[api] complete reconcile task %s: %v", version, err)
	}
	log.Printf("[api] reconcile-tag=%s completed", version)
}
