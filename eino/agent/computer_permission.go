package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type ComputerPermissionRequest struct {
	ID        string               `json:"id"`
	Action    string               `json:"action"`
	Path      string               `json:"path,omitempty"`
	Command   string               `json:"command,omitempty"`
	Args      []string             `json:"args,omitempty"`
	WorkDir   string               `json:"workDir,omitempty"`
	Risk      string               `json:"risk"`
	Status    string               `json:"status"`
	Reason    string               `json:"reason"`
	CreatedAt time.Time            `json:"createdAt"`
	ExpiresAt time.Time            `json:"expiresAt"`
	Input     *ComputerActionInput `json:"input,omitempty"`
}

type ComputerPermissionStore struct {
	mu       sync.Mutex
	pending  map[string]*ComputerPermissionRequest
	approved map[string]time.Time
	nextID   int64
	ttl      time.Duration
}

func NewComputerPermissionStore() *ComputerPermissionStore {
	return &ComputerPermissionStore{
		pending:  make(map[string]*ComputerPermissionRequest),
		approved: make(map[string]time.Time),
		ttl:      5 * time.Minute,
	}
}

var computerPermissions = NewComputerPermissionStore()

func PendingComputerPermissions() []ComputerPermissionRequest {
	return computerPermissions.Pending()
}

func ResolveComputerPermission(id, decision string) (ComputerPermissionRequest, error) {
	return computerPermissions.Resolve(id, decision)
}

func requireComputerPermission(input *ComputerActionInput, policy ComputerPolicy) (bool, string, error) {
	if !policy.RequireApproval {
		return true, "", nil
	}
	if input == nil {
		return false, "", fmt.Errorf("input is required")
	}
	if computerPermissions.ConsumeApproval(input) {
		return true, "", nil
	}
	req := computerPermissions.Create(input)
	return false, req.ID, nil
}

func (s *ComputerPermissionStore) Create(input *ComputerActionInput) ComputerPermissionRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(time.Now())
	key := permissionKey(input)
	for _, req := range s.pending {
		if req.Status == "pending" && req.ExpiresAt.After(time.Now()) && permissionKey(req.Input) == key {
			return *req
		}
	}
	s.nextID++
	now := time.Now()
	req := &ComputerPermissionRequest{
		ID:        fmt.Sprintf("perm-%d", s.nextID),
		Action:    strings.TrimSpace(input.Action),
		Path:      strings.TrimSpace(input.Path),
		Command:   strings.TrimSpace(input.Command),
		Args:      append([]string(nil), input.Args...),
		WorkDir:   strings.TrimSpace(input.WorkDir),
		Risk:      permissionRisk(input.Action),
		Status:    "pending",
		Reason:    "智能体请求执行电脑操作，需要你确认后才会继续。",
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
		Input:     cloneComputerInput(input),
	}
	s.pending[req.ID] = req
	return *req
}

func (s *ComputerPermissionStore) Pending() []ComputerPermissionRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(time.Now())
	result := make([]ComputerPermissionRequest, 0, len(s.pending))
	for _, req := range s.pending {
		if req.Status == "pending" {
			copied := *req
			copied.Input = nil
			result = append(result, copied)
		}
	}
	return result
}

func (s *ComputerPermissionStore) Resolve(id, decision string) (ComputerPermissionRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(time.Now())
	req, ok := s.pending[id]
	if !ok {
		return ComputerPermissionRequest{}, fmt.Errorf("permission request not found")
	}
	decision = strings.TrimSpace(strings.ToLower(decision))
	switch decision {
	case "approve", "approved", "allow":
		req.Status = "approved"
		s.approved[permissionKey(req.Input)] = time.Now().Add(s.ttl)
	case "deny", "denied", "reject":
		req.Status = "denied"
	default:
		return ComputerPermissionRequest{}, fmt.Errorf("unsupported decision %q", decision)
	}
	copied := *req
	copied.Input = nil
	delete(s.pending, id)
	return copied, nil
}

func (s *ComputerPermissionStore) ConsumeApproval(input *ComputerActionInput) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.cleanupLocked(now)
	key := permissionKey(input)
	expiresAt, ok := s.approved[key]
	if !ok || expiresAt.Before(now) {
		return false
	}
	delete(s.approved, key)
	return true
}

func (s *ComputerPermissionStore) cleanupLocked(now time.Time) {
	for id, req := range s.pending {
		if req.ExpiresAt.Before(now) {
			delete(s.pending, id)
		}
	}
	for key, expiresAt := range s.approved {
		if expiresAt.Before(now) {
			delete(s.approved, key)
		}
	}
}

func permissionKey(input *ComputerActionInput) string {
	if input == nil {
		return ""
	}
	return strings.Join([]string{
		strings.TrimSpace(input.Action),
		strings.TrimSpace(input.Path),
		strings.TrimSpace(input.Content),
		strings.TrimSpace(input.Command),
		strings.Join(input.Args, "\x00"),
		strings.TrimSpace(input.WorkDir),
	}, "\x1f")
}

func permissionRisk(action string) string {
	switch strings.TrimSpace(action) {
	case "list_dir", "read_file", "screenshot", "screen_size":
		return "read"
	case "write_file", "open_path", "move", "scroll":
		return "write"
	case "click", "double_click", "drag", "type_text", "press_key":
		return "input"
	case "run_command":
		return "command"
	default:
		return "unknown"
	}
}

func cloneComputerInput(input *ComputerActionInput) *ComputerActionInput {
	if input == nil {
		return nil
	}
	return &ComputerActionInput{
		Action:  input.Action,
		Path:    input.Path,
		Content: input.Content,
		Command: input.Command,
		Args:    append([]string(nil), input.Args...),
		WorkDir: input.WorkDir,
	}
}
