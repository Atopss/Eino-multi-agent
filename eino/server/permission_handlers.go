package server

import (
	"encoding/json"
	"net/http"

	"eino/agent"
)

func (s *Server) handlePermissionsPending(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jsonOK(w, map[string]interface{}{
		"permissions": agent.PendingComputerPermissions(),
	})
}

func (s *Server) handlePermissionsResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID       string `json:"id"`
		Decision string `json:"decision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Bad request", http.StatusBadRequest)
		return
	}
	if req.ID == "" || req.Decision == "" {
		jsonError(w, "id and decision required", http.StatusBadRequest)
		return
	}
	result, err := agent.ResolveComputerPermission(req.ID, req.Decision)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r, "perm_resolve", req.ID, "decision="+req.Decision)
	jsonOK(w, map[string]interface{}{
		"permission": result,
	})
}
