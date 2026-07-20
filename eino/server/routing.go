package server

import (
	"encoding/json"
	"log"
	"net/http"
	"runtime/debug"

	"eino/auth"
)

// maxChatBodyBytes 限制 /api/chat 与 /api/chat/stream 请求体大小，防止异常大请求拖垮服务。
const maxChatBodyBytes = 1 << 20 // 1MB

// buildMux 把所有路由注册到独立的 ServeMux（不再使用全局默认 mux），
// 便于在 http.Server 上统一设置超时与优雅关闭。
func (s *Server) buildMux() http.Handler {
	mux := http.NewServeMux()

	// 公开端点：健康检查。
	mux.HandleFunc("/api/healthz", s.corsMiddleware(s.handleHealthz))

	// 公开端点：登录（签发 JWT）。自身按来源 IP 限流以抵御爆破；jwt 模式下供前端获取令牌。
	mux.HandleFunc("/api/auth/login",
		s.corsMiddleware(auth.RateLimitMiddleware(s.limiter, auth.LoginHandler(s.userStore, s.authSecret, s.loginTTL()))))

	// 受保护端点：鉴权 + 按用户/IP 限流（读类 / 普通操作）。
	protected := func(pattern string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, s.corsMiddleware(auth.AuthMiddleware(s.authMode, s.authSecret, auth.RateLimitMiddleware(s.limiter, h))))
	}
	// 管理员端点：在受保护基础上叠加 AdminGuard（jwt 模式校验 is_admin，local 模式放行）。
	// 注册端点自身也是管理员专属，避免公开注册被滥用。
	adminOnly := func(pattern string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, s.corsMiddleware(auth.AuthMiddleware(s.authMode, s.authSecret, auth.RateLimitMiddleware(s.limiter, auth.AdminGuard(s.userStore, h)))))
	}
	adminOnly("/api/auth/register", auth.RegisterHandler(s.userStore))
	protected("/api/chat", s.handleChat)
	protected("/api/chat/stream", s.handleChatStream)
	protected("/api/agents", s.handleAgents)
	protected("/api/rag/upload", s.handleRagUpload)
	protected("/api/rag/upload-file", s.handleRagUploadFile)
	protected("/api/rag/count", s.handleRagCount)
	protected("/api/rag/search", s.handleRagSearch)
	protected("/api/rag/status", s.handleRagStatus)
	protected("/api/settings", s.handleSettings)
	protected("/api/models", s.handleModels)
	protected("/api/tools", s.handleTools)
	protected("/api/skills", s.handleSkills)
	protected("/api/session/create", s.handleSessionCreate)
	protected("/api/session/message", s.handleSessionMessage)
	protected("/api/session/history", s.handleSessionHistory)
	protected("/api/session/save", s.handleSessionSave)
	protected("/api/session/list", s.handleSessionList)
	protected("/api/session/delete", s.handleSessionDelete)
	protected("/api/runtime/memory", s.handleRuntimeMemory)
	protected("/api/screenshot/", s.handleScreenshotGet)

	// 以下为“能改动服务端状态 / 访问主机资源”的敏感端点，仅管理员可用。
	adminOnly("/api/rag/scan", s.handleRagScan)
	adminOnly("/api/rag/test-embedding", s.handleTestEmbedding)
	adminOnly("/api/agent/create", s.handleAgentCreate)
	adminOnly("/api/agent/delete", s.handleAgentDelete)
	adminOnly("/api/agent/update", s.handleAgentUpdate)
	adminOnly("/api/providers", s.handleProviders)
	adminOnly("/api/providers/discover-models", s.handleDiscoverModels)
	adminOnly("/api/skill/add", s.handleSkillAdd)
	adminOnly("/api/skill/delete", s.handleSkillDelete)
	adminOnly("/api/browse", s.handleBrowse)
	adminOnly("/api/permissions/pending", s.handlePermissionsPending)
	adminOnly("/api/permissions/resolve", s.handlePermissionsResolve)
	adminOnly("/api/runtime/gc", s.handleRuntimeGC)
	return mux
}

// sessionKey 把客户端会话 ID 命名空间化到当前登录用户，
// 实现“同一用户内共享会话、跨用户隔离”，而 SessionManager 方法签名保持不变。
func (s *Server) sessionKey(r *http.Request, clientID string) string {
	if u, ok := auth.UserFromContext(r.Context()); ok && u != nil {
		return u.UserID + "/" + clientID
	}
	return clientID
}

// handleHealthz 健康检查（无需鉴权，供探针/反向代理使用）。
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	code := http.StatusOK
	if s.db == nil {
		status = "db_unavailable"
		code = http.StatusServiceUnavailable
	}
	jsonOK(w, map[string]interface{}{
		"status":   status,
		"service":  "eino",
		"version":  "1.0",
		"agents":   s.manager.Count(),
		"ragReady": s.rag != nil,
	})
	if code != http.StatusOK {
		w.WriteHeader(code)
	}
}

// corsMiddleware 按可配置白名单做跨域控制：
//   - 命中具体白名单 Origin 时，回显该 Origin 并允许携带凭据（Access-Control-Allow-Credentials: true）
//   - 白名单含 "*" 时，按"任意源、无凭据"处理（避免通配符 + 凭据的危险组合）
//   - 未命中则不加 CORS 头，浏览器将自行拒绝跨域请求
// 默认白名单在 Server.New 中读取 CORS_ALLOW_ORIGINS 环境变量；为空时退化为 ["*"]（本地开发便利，但无凭据）。
func (s *Server) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("=== PANIC ===\nPath: %s %s\nReason: %v\n\n%s", r.Method, r.URL.Path, rec, debug.Stack())
				jsonError(w, "Internal server error", http.StatusInternalServerError)
			}
		}()
		if origin := r.Header.Get("Origin"); origin != "" {
			if allow, withCreds := s.corsPolicy(origin); allow != "" {
				w.Header().Set("Access-Control-Allow-Origin", allow)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				if withCreds {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
			}
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

// corsPolicy 根据请求 Origin 与白名单计算应设置的 CORS 响应头。
// 返回值：allowOrigin 为要写入 Access-Control-Allow-Origin 的值（空串表示不通过）；
// withCredentials 表示是否允许携带凭据（仅当命中具体白名单 origin 时为 true）。
// 安全约束：白名单含 "*" 时按"任意源、无凭据"处理，避免通配符与凭据并用的危险组合。
func (s *Server) corsPolicy(origin string) (allowOrigin string, withCredentials bool) {
	// 先尝试命中具体 origin（允许凭据）
	for _, allowed := range s.allowedOrigins {
		if allowed != "*" && allowed == origin {
			return origin, true
		}
	}
	// 再退化到通配（任意源，但不允许凭据）
	for _, allowed := range s.allowedOrigins {
		if allowed == "*" {
			return "*", false
		}
	}
	return "", false
}

// writeInternalError 记录服务端详细错误，但只向客户端返回通用文案，避免泄露 API Key / 端点等敏感信息。
func writeInternalError(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	jsonError(w, "internal server error", http.StatusInternalServerError)
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
