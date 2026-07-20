package server

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"eino/auth"
)

// QuotaConfig 单用户每日配额（请求数 + Token 估算数）。
// 仅 jwt 模式下的普通（非管理员）用户受此约束；local 模式与 admin 免除。
type QuotaConfig struct {
	DailyRequests int
	DailyTokens   int
}

// QuotaStore 记录与查询“按用户 / 按天”的用量，支撑每日配额。
// 与全局 RPS 限流（auth.RateLimiter）是两件事：
//   - RPS 限流：防突发洪峰，保护后端，按 userID/IP 维度、秒级。
//   - 配额：控制单个用户“一天内”的总使用量，防止被他人/脚本刷爆成本。
type QuotaStore struct {
	db *sql.DB
}

// NewQuotaStore 用主库初始化配额存储（配额表与用户表同库，保证事务一致性）。
func NewQuotaStore(db *sql.DB) *QuotaStore {
	return &QuotaStore{db: db}
}

// todayKey 返回服务器本地日期（YYYY-MM-DD）作为配额周期键。
// 以“自然日”为单位重置，避免按 24h 滑动导致跨日边界配额错位。
func todayKey() string {
	return time.Now().Format("2006-01-02")
}

// Add 原子地累加某用户当天的用量（请求数 + Token 估算数）。
// 使用 INSERT...ON CONFLICT DO UPDATE，并发累加安全（SQLite 串行写，单连接池）。
func (q *QuotaStore) Add(userID, day string, reqInc, tokInc int) error {
	_, err := q.db.Exec(
		`INSERT INTO quota_usage(user_id, day, requests, tokens) VALUES(?,?,?,?)
		 ON CONFLICT(user_id, day) DO UPDATE SET
		   requests = requests + excluded.requests,
		   tokens   = tokens   + excluded.tokens`,
		userID, day, reqInc, tokInc,
	)
	return err
}

// Usage 返回某用户当天的已用量；无记录时返回 (0,0,nil)。
func (q *QuotaStore) Usage(userID, day string) (requests, tokens int, err error) {
	row := q.db.QueryRow(
		`SELECT requests, tokens FROM quota_usage WHERE user_id = ? AND day = ?`,
		userID, day,
	)
	err = row.Scan(&requests, &tokens)
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	return
}

// quotaConfig 返回生效的每日配额（来自配置，含默认值兜底）。
func (s *Server) quotaConfig() QuotaConfig {
	req := s.runtime.QuotaDailyRequests
	tok := s.runtime.QuotaDailyTokens
	if req <= 0 {
		req = 500
	}
	if tok <= 0 {
		tok = 200000
	}
	return QuotaConfig{DailyRequests: req, DailyTokens: tok}
}

// QuotaMiddleware 对普通（非管理员）jwt 用户强制执行“每日配额”，在 handler 前做预检：
// 若当天已用尽请求数或 Token 数，直接 429 拒绝，避免无谓地发起昂贵的模型调用。
// 注意：本中间件只做“开工前”的预检；实际用量在 chat 处理器内（已知输入输出规模）再累加。
func (s *Server) QuotaMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.quotaStore == nil {
			next(w, r)
			return
		}
		u, ok := auth.UserFromContext(r.Context())
		if !ok || u == nil || u.UserID == "local" {
			// local（无登录）模式与未鉴权请求：配额不适用，放行（由 RPS 限流兜底）。
			next(w, r)
			return
		}
		isAdmin, err := s.userStore.IsAdmin(u.UserID)
		if err != nil {
			jsonError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if isAdmin {
			// 管理员豁免配额，便于运维 / 调试。
			next(w, r)
			return
		}
		cfg := s.quotaConfig()
		usedReq, usedTok, err := s.quotaStore.Usage(u.UserID, todayKey())
		if err != nil {
			jsonError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if usedReq >= cfg.DailyRequests || usedTok >= cfg.DailyTokens {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "daily quota exceeded",
				"requests": map[string]int{"used": usedReq, "limit": cfg.DailyRequests},
				"tokens":   map[string]int{"used": usedTok, "limit": cfg.DailyTokens},
			})
			return
		}
		next(w, r)
	}
}

// recordUsage 在 chat 处理成功后累加当前用户的每日配额用量。
// inputChars / outputChars 为输入与输出文本长度（字节），Token 估算 = (input+output)/4，下限 1。
// 之所以放在 handler 内（而非中间件）累加，是因为只有在生成完成后才确切知道输出规模。
func (s *Server) recordUsage(r *http.Request, inputChars, outputChars int) {
	if s.quotaStore == nil {
		return
	}
	u, ok := auth.UserFromContext(r.Context())
	if !ok || u == nil || u.UserID == "local" {
		return
	}
	if isAdmin, err := s.userStore.IsAdmin(u.UserID); err == nil && isAdmin {
		return
	}
	tokens := (inputChars + outputChars) / 4
	if tokens < 1 {
		tokens = 1
	}
	if err := s.quotaStore.Add(u.UserID, todayKey(), 1, tokens); err != nil {
		log.Printf("记录配额用量失败 user=%s: %v", u.UserID, err)
	}
}

// handleQuota 返回当前登录用户当天的配额用量与上限，供前端展示剩余额度。
func (s *Server) handleQuota(w http.ResponseWriter, r *http.Request) {
	if s.quotaStore == nil {
		jsonOK(w, map[string]interface{}{"requests": map[string]int{"used": 0, "limit": 0}, "tokens": map[string]int{"used": 0, "limit": 0}})
		return
	}
	cfg := s.quotaConfig()
	usedReq, usedTok := 0, 0
	if u, ok := auth.UserFromContext(r.Context()); ok && u != nil && u.UserID != "local" {
		usedReq, usedTok, _ = s.quotaStore.Usage(u.UserID, todayKey())
	}
	jsonOK(w, map[string]interface{}{
		"day":      todayKey(),
		"requests": map[string]int{"used": usedReq, "limit": cfg.DailyRequests},
		"tokens":   map[string]int{"used": usedTok, "limit": cfg.DailyTokens},
	})
}

// chatInputChars 估算本轮用户输入文本的 Token 规模（按字符数近似）。
// 覆盖消息正文与图片 / 文件附件的 base64 数据体积（附件会被送进多模态上下文）。
func chatInputChars(req chatRequest) int {
	n := len(req.Message)
	for _, a := range req.Images {
		n += len(a.Data)
	}
	for _, a := range req.Files {
		n += len(a.Data)
	}
	return n
}
