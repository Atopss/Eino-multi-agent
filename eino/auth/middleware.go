package auth

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const bearerPrefix = "Bearer "

// AuthMiddleware 在本地无登录模式下不再校验令牌，直接注入一个固定的匿名用户
// （"local"），使下游（会话归属、RAG owner、权限等）逻辑保持单用户隔离，无需改造。
// 若将来需要暴露到公网，应由反向代理（nginx basic auth / Cloudflare 等）承担鉴权。
func AuthMiddleware(secret string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := WithUser(r.Context(), "local", "local")
		next(w, r.WithContext(ctx))
	}
}

func extractToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, bearerPrefix) {
		return strings.TrimSpace(strings.TrimPrefix(h, bearerPrefix))
	}
	// 注意：不再支持 ?token= 查询参数传递令牌。
	// 前端所有请求（含 SSE streamChat）均通过 Authorization: Bearer 头发送，
	// 暴露令牌于 URL 易被日志记录/Referer 泄露，故禁用。
	return ""
}

// LoginHandler 校验凭据并签发 JWT。
func LoginHandler(store *UserStore, secret string, ttl time.Duration) http.HandlerFunc {
	type req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body req
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, "bad request", http.StatusBadRequest)
			return
		}
		user, err := store.Verify(strings.TrimSpace(body.Username), body.Password)
		if err != nil {
			// 统一返回“账号或密码错误”，不泄露具体原因
			jsonError(w, "invalid username or password", http.StatusUnauthorized)
			return
		}
		token, err := SignToken(user.UserID, user.Username, secret, ttl)
		if err != nil {
			log.Printf("签发 token 失败: %v", err)
			jsonError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		jsonOK(w, map[string]interface{}{
			"token":     token,
			"username":  user.Username,
			"expiresIn": int(ttl.Seconds()),
		})
	}
}

// RateLimiter 基于令牌桶的轻量限流器（按 key 维度，如 userID / IP）。
type bucket struct {
	tokens float64
	last   time.Time
	mu     sync.Mutex
}

type RateLimiter struct {
	rps     float64
	burst   float64
	buckets sync.Map
	stop    chan struct{}
}

func NewRateLimiter(rps, burst int) *RateLimiter {
	if rps <= 0 {
		rps = 20
	}
	if burst <= 0 {
		burst = 40
	}
	rl := &RateLimiter{
		rps:   float64(rps),
		burst: float64(burst),
		stop:  make(chan struct{}),
	}
	go rl.cleanup()
	return rl
}

// Allow 取走一个令牌，足够则返回 true。
func (rl *RateLimiter) Allow(key string) bool {
	v, _ := rl.buckets.LoadOrStore(key, &bucket{tokens: rl.burst, last: time.Now()})
	b := v.(*bucket)
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * rl.rps
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stop:
			return
		case <-ticker.C:
			rl.buckets.Range(func(k, v interface{}) bool {
				b := v.(*bucket)
				b.mu.Lock()
				stale := time.Since(b.last) > 30*time.Minute
				b.mu.Unlock()
				if stale {
					rl.buckets.Delete(k)
				}
				return true
			})
		}
	}
}

func (rl *RateLimiter) Stop() {
	select {
	case <-rl.stop:
	default:
		close(rl.stop)
	}
}

// RateLimitMiddleware 按 userID（优先）或来源 IP 限流。
func RateLimitMiddleware(rl *RateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !rl.Allow(clientKey(r)) {
			jsonError(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

func clientKey(r *http.Request) string {
	if u, ok := UserFromContext(r.Context()); ok && u != nil {
		return "u:" + u.UserID
	}
	ip := r.RemoteAddr
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip = strings.Split(fwd, ",")[0]
	}
	return "ip:" + strings.TrimSpace(ip)
}

func jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
