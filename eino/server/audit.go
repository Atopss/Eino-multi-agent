package server

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"eino/auth"
)

// AuditEntry 是 audit_log 表的一行在 API 响应中的映射（字段驼峰化，便于前端消费）。
type AuditEntry struct {
	ID     int64  `json:"id"`
	UserID string `json:"userId"`
	Action string `json:"action"`
	Target string `json:"target"`
	Detail string `json:"detail"`
	IP     string `json:"ip"`
	TS     string `json:"ts"`
}

// AuditStore 负责操作审计日志的写入与分页查询，与用户库同库以保证一致性。
type AuditStore struct {
	db *sql.DB
}

// NewAuditStore 用主库初始化审计存储。
func NewAuditStore(db *sql.DB) *AuditStore {
	return &AuditStore{db: db}
}

// Record 写入一条审计记录。ts 由数据库默认值（CURRENT_TIMESTAMP）自动填充。
func (a *AuditStore) Record(userID, action, target, detail, ip string) error {
	_, err := a.db.Exec(
		`INSERT INTO audit_log(user_id, action, target, detail, ip) VALUES(?,?,?,?,?)`,
		userID, action, target, detail, ip,
	)
	return err
}

// List 分页返回审计日志（按时间倒序），并返回总数 total 供前端做分页。
// limit 上限 200，超出按 50 处理，避免一次拉取过多。
func (a *AuditStore) List(limit, offset int) ([]AuditEntry, int, error) {
	var total int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := a.db.Query(
		`SELECT id, user_id, action, target, detail, ip, ts
		   FROM audit_log
		  ORDER BY ts DESC, id DESC
		  LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]AuditEntry, 0, limit)
	for rows.Next() {
		var e AuditEntry
		var ts string
		if err := rows.Scan(&e.ID, &e.UserID, &e.Action, &e.Target, &e.Detail, &e.IP, &ts); err != nil {
			return nil, 0, err
		}
		e.TS = ts
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// clientIP 从请求中提取客户端 IP（优先 X-Forwarded-For，回退 RemoteAddr）。
// server 包内独立实现，避免跨包依赖 auth 的未导出辅助函数。
func clientIP(r *http.Request) string {
	ip := r.RemoteAddr
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip = strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	return ip
}

// audit 是当前登录用户的便捷审计写入：自动从 context 取 user_id、从请求取 IP。
// 用于已通过 AuthMiddleware 的受保护/管理员 handler；登录注册等未注入用户的场景请用 onAudit 回调。
func (s *Server) audit(r *http.Request, action, target, detail string) {
	if s.auditStore == nil {
		return
	}
	userID := ""
	if u, ok := auth.UserFromContext(r.Context()); ok && u != nil {
		userID = u.UserID
	}
	if err := s.auditStore.Record(userID, action, target, detail, clientIP(r)); err != nil {
		log.Printf("审计记录写入失败 action=%s: %v", action, err)
	}
}

// handleAudit 返回审计日志（分页），仅管理员可访问。
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if s.auditStore == nil {
		jsonError(w, "audit not available", http.StatusInternalServerError)
		return
	}
	limit, offset := 50, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			offset = n
		}
	}
	entries, total, err := s.auditStore.List(limit, offset)
	if err != nil {
		log.Printf("查询审计日志失败: %v", err)
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]interface{}{
		"entries": entries,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
		"now":     time.Now().Format(time.RFC3339),
	})
}
