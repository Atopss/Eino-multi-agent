package server

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"eino/agent"
	"eino/auth"
	"eino/config"
	"eino/db"
	"eino/rag"
	"eino/skills"

	"github.com/cloudwego/eino/schema"
)

// arkHTTPClient 用于后端直接调用 Ark API（如图片理解）。
// 带整体超时，避免上游无响应时请求无限挂起、长时间占用会话锁导致该会话卡死。
var arkHTTPClient = &http.Client{Timeout: 180 * time.Second}

type Server struct {
	agents         map[string]*agent.Agent
	configs        map[string]config.AgentConfig
	runtime        config.RuntimeConfig
	rag            *rag.RAGManager
	manager        *agent.AgentManager
	orchestrator   *agent.Orchestrator
	sessions       *agent.SessionManager
	skillMgr       *skills.SkillManager
	srv            *http.Server
	allowedOrigins []string
	chatLocks      sync.Map
	chatLocksTicks atomic.Int64 // 节流计数器，用于触发孤儿锁清理
	mu             sync.RWMutex

	// 生产级加固
	db        *sql.DB
	userStore *auth.UserStore
	authSecret string
	authMode  string // "local" 或 "jwt"，驱动 AuthMiddleware 行为
	limiter   *auth.RateLimiter

	// 配置热加载：stopCh 用于优雅关闭轮询 goroutine，避免长期运行泄漏。
	stopCh chan struct{}
}

// configWatchInterval 配置热加载轮询周期；配置属低频变更，3s 足够且开销极小。
const configWatchInterval = 3 * time.Second

// configWatchSettle 检测到变更后静置时间，规避编辑器临时文件/原子写入导致的多次触发。
const configWatchSettle = 500 * time.Millisecond

// startConfigWatcher 启动一个轻量轮询 goroutine，监听 config.json / agents.json 的
// 修改时间（mtime）；任一文件变更即按去抖策略在后台调用已有的 rebuild()（自带 RWMutex 保护，并发安全）。
// 无需引入 fsnotify 等第三方依赖；调用 Stop() 可关闭该 goroutine。
func (s *Server) startConfigWatcher() {
	s.stopCh = make(chan struct{})
	files := []string{"config.json", "agents.json"}
	// resolveAll 每次重新解析路径，以覆盖"文件原本不存在、后续才生成"的情况。
	resolveAll := func() map[string]time.Time {
		m := make(map[string]time.Time)
		for _, f := range files {
			p := resolvePreferredPath(f)
			if p == "" {
				continue
			}
			if fi, err := os.Stat(p); err == nil {
				m[p] = fi.ModTime()
			}
		}
		return m
	}
	last := resolveAll()
	go func() {
		ticker := time.NewTicker(configWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
			}
			cur := resolveAll()
			changed := false
			for p, t := range cur {
				if prev, ok := last[p]; !ok || !prev.Equal(t) {
					changed = true
					last[p] = t
				}
			}
			if !changed {
				continue
			}
			// 去抖：静置后再确认一次，过滤原子写入/临时文件导致的抖动。
			time.Sleep(configWatchSettle)
			cur2 := resolveAll()
			still := false
			for p, t := range cur2 {
				if prev, ok := last[p]; !ok || !prev.Equal(t) {
					still = true
					last[p] = t
				}
			}
			if still {
				log.Printf("[config-watch] 检测到配置文件变更，触发 rebuild()")
				s.rebuild()
			}
		}
	}()
}

// Stop 优雅关闭 Server 及其后台 goroutine（配置热加载轮询）。
func (s *Server) Stop() {
	if s.stopCh != nil {
		select {
		case <-s.stopCh:
			// 已关闭，避免重复 close 引发 panic
		default:
			close(s.stopCh)
		}
	}
	if s.srv != nil {
		_ = s.srv.Close()
	}
}

// chatLocksGCPeriod 控制孤儿锁清理的触发节流：每新增该数量的会话锁，执行一次清理。
const chatLocksGCPeriod = 64

// gcOrphanChatLocks 删除已不存在于会话列表中的孤儿锁，
// 防止 chatLocks 长期运行只增不减导致内存泄漏。
// 调用方应通过 chatLocksTicks 节流控制触发频率。
func (s *Server) gcOrphanChatLocks() {
	active := make(map[string]struct{})
	for _, id := range s.sessions.ListSessions() {
		active[id] = struct{}{}
	}
	s.chatLocks.Range(func(k, _ interface{}) bool {
		if _, ok := active[k.(string)]; !ok {
			s.chatLocks.Delete(k)
		}
		return true
	})
}

func New() *Server {
	s := &Server{
		agents:   make(map[string]*agent.Agent),
		configs:  make(map[string]config.AgentConfig),
		manager:  agent.NewAgentManager(),
		sessions: agent.NewSessionManager(),
	}
	s.orchestrator = agent.NewOrchestrator(s.manager)
	s.loadRuntimeConfig()
	s.initDB()
	s.wireAgentTools()
	s.applyRuntimeLimits()
	s.loadFromEnv()
	s.ensureDefaultAgent()
	s.initRAG()
	localDataDir := filepath.Join(".", "data")
	s.skillMgr = skills.NewSkillManager(localDataDir)
	s.skillMgr.LoadAll()
	s.sessions.SetDataDir(filepath.Join(".", "data", "sessions"))
	s.sessions.LoadAll()
	s.mu.RLock()
	for _, a := range s.agents {
		if s.rag != nil {
			a.SetRAG(s.rag)
		}
		if s.skillMgr != nil {
			a.SetSkillManager(s.skillMgr)
		}
	}
	s.mu.RUnlock()
	if s.rag != nil {
		s.manager.SetRAG(s.rag)
	}
	s.startConfigWatcher()
	return s
}

// initDB 打开 SQLite、建表、迁移存量会话。
// 本地无登录模式下不再强制 JWT_SECRET（鉴权交由反向代理承担）。
func (s *Server) initDB() {
	if s.runtime.JWTSecret == "" {
		log.Println("提示：未配置 JWT_SECRET，已切换为本地无登录模式（适用于本机自用）")
	}
	d, err := db.Open(s.runtime.SQLitePath)
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	if err := db.Migrate(d); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	s.db = d
	s.userStore = auth.NewUserStore(d)
	// 首次启动（用户表为空）时引导一个初始管理员，保证 jwt 模式下有可登录账号。
	s.userStore.EnsureAdmin()
	db.ImportLegacySessions(d, filepath.Join(".", "data", "sessions"))
	s.sessions.SetDB(d)
	s.authSecret = s.runtime.JWTSecret
	s.authMode = s.runtime.AuthMode
	// jwt 模式必须配合 JWT_SECRET，否则退化为 local 并告警，避免空签名 token。
	if s.authMode == auth.AuthModeJWT && s.authSecret == "" {
		log.Println("警告：AUTH_MODE=jwt 但未配置 JWT_SECRET，已回退为本地无登录模式")
		s.authMode = auth.AuthModeLocal
	}
	s.limiter = auth.NewRateLimiter(s.runtime.RateLimitRPS, s.runtime.RateLimitBurst)
	log.Printf("数据库已就绪: %s（鉴权模式=%s）", s.runtime.SQLitePath, s.authMode)
}

// loginTTL 返回登录签发 Token 的有效期（来自 TokenTTLHours，默认 24h）。
func (s *Server) loginTTL() time.Duration {
	h := s.runtime.TokenTTLHours
	if h <= 0 {
		h = 24
	}
	return time.Duration(h) * time.Hour
}

func (s *Server) wireAgentTools() {
	timeout := s.runtime.ComputerCommandTimeout
	if timeout <= 0 {
		timeout = 15
	}
	agent.SetComputerPolicy(agent.ComputerPolicy{
		Enabled:         s.runtime.ComputerToolsEnabled,
		AllowedRoots:    s.runtime.ComputerAllowedRoots,
		AllowCommands:   s.runtime.ComputerAllowCommands,
		AllowedCommands: s.runtime.ComputerAllowedCommands,
		CommandTimeout:  time.Duration(timeout) * time.Second,
		RequireApproval: s.runtime.ComputerRequireApproval,
		DesktopEnabled:  s.runtime.ComputerDesktopEnabled,
		DaemonPort:      s.runtime.ComputerDaemonPort,
	})
}

func (s *Server) loadRuntimeConfig() {
	cfg, err := config.LoadRuntimeConfig(".")
	if err != nil {
		log.Printf("Failed to load data/config.json: %v", err)
	}
	s.runtime = cfg
	s.allowedOrigins = parseCORSOrigins(os.Getenv("CORS_ALLOW_ORIGINS"))
}

// parseCORSOrigins 解析 CORS 白名单（逗号分隔）。为空时退化为 ["*"] 以兼容本地 file:// 打开。
func parseCORSOrigins(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{"*"}
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return []string{"*"}
	}
	return result
}

// Start 使用独立的 http.Server（替代全局默认 mux），
// 统一设置读/写/空闲超时，并返回，便于 main 在收到信号时优雅关闭。
func (s *Server) Start(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.buildMux(),
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      300 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	s.srv = srv
	log.Printf("Eino 智能体 API 服务正在监听 %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown 平滑关闭：在 ctx 超时内等待活跃请求完成。
func (s *Server) Shutdown(ctx context.Context) error {
	if s.limiter != nil {
		s.limiter.Stop()
	}
	if s.srv == nil {
		db.Close(s.db)
		return nil
	}
	err := s.srv.Shutdown(ctx)
	db.Close(s.db)
	return err
}

func (s *Server) applyRuntimeLimits() {
	if s.runtime.GCPercent > 0 {
		debug.SetGCPercent(s.runtime.GCPercent)
	}
	if s.runtime.MemoryLimitMB > 0 {
		limit := int64(s.runtime.MemoryLimitMB) * 1024 * 1024
		debug.SetMemoryLimit(limit)
		log.Printf("Go memory limit set to %d MB", s.runtime.MemoryLimitMB)
	}
}

func (s *Server) loadFromEnv() {
	if s.loadAgentsFromConfigFile() {
		return
	}

	deepseekAPIKey := os.Getenv("DEEPSEEK_API_KEY")
	provider := s.runtime.PrimaryProvider()
	if provider.APIKey == "" && deepseekAPIKey == "" {
		log.Println("WARNING: No API Key configured in .env")
		return
	}
	for i := 1; i <= 10; i++ {
		name := os.Getenv(fmt.Sprintf("AGENT_NAME_%d", i))
		prompt := os.Getenv(fmt.Sprintf("AGENT_PROMPT_%d", i))
		if name == "" {
			continue
		}
		apiKey, modelID, pType, baseURL, ok := s.resolveAgentCredentials(name, "", "")
		if !ok {
			continue
		}
		if prompt == "" {
			prompt = "You are " + name + ", a professional AI assistant."
		}
		needTools := true
		if v := os.Getenv(fmt.Sprintf("AGENT_NEED_TOOLS_%d", i)); v != "" {
			if b, err := strconv.ParseBool(v); err == nil {
				needTools = b
			}
		}
		cfg := config.AgentConfig{
			Name:           name,
			ModelID:        modelID,
			APIKey:         apiKey,
			SystemPrompt:   prompt,
			NeedTools:      needTools,
			ProviderName:   "",
			ProviderType:   pType,
			BaseURL:        baseURL,
		}
		s.addAgentFromConfig(cfg)
	}
}

// ensureDefaultAgent 保证内置主控智能体（config.DefaultAgentName）始终存在：
//   - 若已存在（用户已在 agents.json 中配置同名项），保持原样；
//   - 若不存在且有可用商家凭据，则注入内置默认配置（Locked=true）并写回持久化；
//   - 若没有任何商家凭据（与普通智能体一样）则跳过，待用户配置 Key 后再注入。
// 该智能体不可被删除、不可被改名，并在 supervisor 编排中优先担任协调者。
func (s *Server) ensureDefaultAgent() {
	// 向后兼容：旧版本持久化了名为 "Eino（主控）" 的内置主控，
	// 改名后将其整体迁移为新名（保留全部字段与 Locked 标记），避免残留或重复。
	s.mu.Lock()
	if old, ok := s.configs["Eino（主控）"]; ok {
		if _, exists := s.configs[config.DefaultAgentName]; !exists {
			old.Name = config.DefaultAgentName
			old.Locked = true
			delete(s.configs, "Eino（主控）")
			s.configs[config.DefaultAgentName] = old
			s.mu.Unlock()
			s.saveAgentsToConfig()
			log.Printf("内置主控智能体已迁移为 %s", config.DefaultAgentName)
			return
		}
		// 新旧名同时存在（异常）：删除旧的锁定项，保留新名。
		delete(s.configs, "Eino（主控）")
		s.mu.Unlock()
		s.saveAgentsToConfig()
		return
	}
	_, exists := s.configs[config.DefaultAgentName]
	s.mu.Unlock()
	if exists {
		return
	}
	apiKey, modelID, pType, baseURL, ok := s.resolveAgentCredentials(config.DefaultAgentName, "", "")
	cfg := config.AgentConfig{
		Name:         config.DefaultAgentName,
		ModelID:      modelID,
		APIKey:       apiKey,
		SystemPrompt: config.DefaultAgentSystemPrompt(),
		NeedTools:    true,
		ProviderName: "",
		ProviderType: pType,
		BaseURL:     baseURL,
		Locked:       true,
	}
	if !ok {
		log.Printf("内置主控智能体 %s 暂无有效凭据，将在配置保存后等待凭据就绪", config.DefaultAgentName)
	}
	if s.addAgentFromConfig(cfg) {
		s.saveAgentsToConfig()
		log.Printf("内置主控智能体 %s 已注入", config.DefaultAgentName)
	} else {
		// SDK 拒绝空 APIKey 创建模型→Agent 实例化失败。
		// 但仍要把配置写入 s.configs + 持久化，使 UI 列表可见且不可删除。
		// 用户配置好 API Key 后，rebuild() 会重新创建带有效凭据的 Agent。
		s.mu.Lock()
		s.configs[config.DefaultAgentName] = cfg
		s.mu.Unlock()
		s.saveAgentsToConfig()
		log.Printf("内置主控智能体 %s 配置已持久化（待配置 API Key 后可对话）", config.DefaultAgentName)
	}
}

func (s *Server) loadAgentsFromConfigFile() bool {
	path := s.findAgentsConfigPath()
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Failed to read agent config %s: %v", path, err)
		return false
	}
	data = []byte(strings.TrimPrefix(string(data), "\uFEFF"))
	var raw map[string]struct {
		Name          string `json:"name"`
		ModelID       string `json:"modelID"`
		SystemPrompt  string `json:"systemPrompt"`
		NeedTools     *bool  `json:"needTools"`
		Provider      string `json:"provider"`
		Locked        bool   `json:"locked"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		log.Printf("Failed to parse agent config %s: %v", path, err)
		return false
	}
	loaded := 0
	for key, item := range raw {
		name := item.Name
		if name == "" {
			name = key
		}
		apiKey, modelID, pType, baseURL, ok := s.resolveAgentCredentials(name, item.ModelID, item.Provider)
		if !ok {
			// 内置锁定智能体（如主控智能体）即使凭据未就绪也要保留配置，
			// 否则 UI 中会丢失该条目，让用户误以为可删除或不生效。
			if item.Locked {
				log.Printf("内置智能体 %s 凭据暂未就绪，保留配置以待后续", name)
				cfg := config.AgentConfig{
					Name:         name,
					SystemPrompt: item.SystemPrompt,
					NeedTools:    item.NeedTools == nil || *item.NeedTools,
					Locked:       true,
				}
				s.configs[name] = cfg
				loaded++
			} else {
				log.Printf("Skip agent %s: no usable provider credential", name)
			}
			continue
		}
		systemPrompt := item.SystemPrompt
		if systemPrompt == "" {
			systemPrompt = "You are " + name + ", a professional AI assistant."
		}
		// 工具默认开启（符合"工具是标配"的直觉），除非在配置中显式设为 false。
		needTools := true
		if item.NeedTools != nil {
			needTools = *item.NeedTools
		}
		cfg := config.AgentConfig{
			Name:           name,
			ModelID:        modelID,
			APIKey:         apiKey,
			SystemPrompt:   systemPrompt,
			NeedTools:      needTools,
			ProviderName:   item.Provider,
			ProviderType:   pType,
			BaseURL:        baseURL,
			Locked:         item.Locked,
		}
		if s.addAgentFromConfig(cfg) {
			loaded++
		}
	}
	if loaded > 0 {
		log.Printf("Loaded %d agents from %s", loaded, path)
		return true
	}
	return false
}

func (s *Server) findAgentsConfigPath() string {
	return resolvePreferredPath("agents.json")
}

// resolvePreferredPath 在 ./data/ 和 ../data/ 中查找指定文件，优先选数据更完整的目录。
// 当工作目录变化（如 eino/ vs 根目录）时，防止 fork 出两套互不同步的 data 目录。
func resolvePreferredPath(filename string) string {
	type candidate struct {
		path  string
		score int
	}
	var best candidate
	// 先检查 ../data/（根目录），再检查 ./data/。平局时根目录优先，避免工作目录变化
	// 导致在 eino/ 子目录启动时 fork 出独立的数据副本。
	for _, base := range []string{"..", "."} {
		p := filepath.Join(base, "data", filename)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		dir := filepath.Dir(p)
		score := 0
		for _, f := range []string{"config.json", "eino.db", "sessions"} {
			if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
				score++
			}
		}
		if abs, absErr := filepath.Abs(p); absErr == nil {
			p = abs
		}
		if score > best.score || (score == best.score && best.path == "") {
			best = candidate{p, score}
		}
	}
	if best.path != "" {
		return best.path
	}
	return filepath.Join(".", "data", filename)
}

// resolveProvider 按名称查找已配置（含 Key）的商家。
func (s *Server) resolveProvider(name string) (config.ProviderConfig, bool) {
	for _, p := range s.runtime.Providers {
		if p.Name == name && p.APIKey != "" {
			return p, true
		}
	}
	return config.ProviderConfig{}, false
}

// resolveAgentCredentials 解析出构建某个 Agent 所需的凭据。
// 返回 (apiKey, modelID, providerType, baseURL, ok)。
// 优先使用显式指定的商家名（providerName）；否则回退到按名称/模型匹配与默认主商家。
func (s *Server) resolveAgentCredentials(name, modelID, providerName string) (string, string, string, string, bool) {
	if providerName != "" {
		if p, ok := s.resolveProvider(providerName); ok {
			m := modelID
			if m == "" {
				m = firstNonEmpty(p.ChatModel, p.EndpointID)
			}
			if m == "" {
				return "", "", "", "", false
			}
			return p.APIKey, m, p.Type, p.BaseURL, true
		}
	}
	if strings.Contains(strings.ToLower(name), "deepseek") {
		if apiKey := os.Getenv("DEEPSEEK_API_KEY"); apiKey != "" {
			if modelID == "" {
				modelID = "deepseek-chat"
			}
			return apiKey, modelID, "openai", "https://api.deepseek.com", true
		}
	}
	for _, provider := range s.runtime.Providers {
		candidateModel := provider.EndpointID
		if candidateModel == "" {
			candidateModel = provider.ChatModel
		}
		if candidateModel == "" {
			continue
		}
		if modelID == "" || modelID == provider.EndpointID || modelID == provider.ChatModel || modelID == candidateModel {
			if provider.APIKey != "" {
				return provider.APIKey, candidateModel, provider.Type, provider.BaseURL, true
			}
		}
	}
	provider := s.runtime.PrimaryProvider()
	candidateModel := provider.EndpointID
	if candidateModel == "" {
		candidateModel = provider.ChatModel
	}
	if provider.APIKey != "" && candidateModel != "" {
		if modelID == "" {
			modelID = candidateModel
		}
		return provider.APIKey, modelID, provider.Type, provider.BaseURL, true
	}
	return "", "", "", "", false
}

// resolveModelOverride 根据前端传入的全局所选模型，解析出对应的 API Key / 类型 / BaseURL。
// 返回 *config.AgentConfig 供 Agent 动态构建模型；model 为空表示使用智能体内置默认模型（返回 nil）。
// 找不到对应商家配置（如该模型所在商家未配 Key）时返回明确错误，便于前端给出中文提示。
func (s *Server) resolveModelOverride(req chatRequest, agentName string) (*config.AgentConfig, error) {
	if req.Model == "" {
		return nil, nil
	}
	apiKey, modelID, pType, baseURL, ok := s.resolveAgentCredentials(agentName, req.Model, req.Provider)
	if !ok {
		return nil, fmt.Errorf("未找到模型 %q 对应的 API Key，请到「设置 → 模型服务」中配置该模型所在的商家", req.Model)
	}
	return &config.AgentConfig{
		Name:         agentName,
		ModelID:      modelID,
		APIKey:       apiKey,
		ProviderType: pType,
		BaseURL:      baseURL,
	}, nil
}

// friendlyModelError 把模型调用返回的原始错误映射为面向用户的中文提示，
// 覆盖常见失败：Key 无效/过期、欠费/限流(402/429)、模型名不存在(404)、超时、网络不可达。
func friendlyModelError(err error) string {
	if err == nil {
		return "未知错误"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "401") || strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "invalid api key") || strings.Contains(msg, "invalid_authentication") ||
		strings.Contains(msg, "authentication") || strings.Contains(msg, "api key") && strings.Contains(msg, "invalid"):
		return "API Key 无效或已过期，请到「设置 → 模型服务」检查该商家的 Key。"
	case strings.Contains(msg, "402") || strings.Contains(msg, "insufficient") ||
		strings.Contains(msg, "quota") || strings.Contains(msg, "balance") || strings.Contains(msg, "欠费") ||
		strings.Contains(msg, "额度"):
		return "账户余额不足或已被限流（可能欠费），请充值或检查该商家的调用额度。"
	case strings.Contains(msg, "429") || strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "rate_limit") || strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "限流") || strings.Contains(msg, "频率"):
		return "请求过于频繁被限流，请稍后重试（或降低发送频率）。"
	case strings.Contains(msg, "404") || strings.Contains(msg, "not found") ||
		strings.Contains(msg, "does not exist") || strings.Contains(msg, "不存在") ||
		strings.Contains(msg, "model") && (strings.Contains(msg, "not exist") || strings.Contains(msg, "not found")):
		return "模型名不存在或不可在该商家使用，请检查所选模型名是否正确。"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline") ||
		strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "context deadline") ||
		strings.Contains(msg, "超时"):
		return "请求模型超时，请检查网络或稍后重试。"
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "dns") || strings.Contains(msg, "no route") ||
		strings.Contains(msg, "连接"):
		return "无法连接模型服务，请检查网络或该商家的 BaseURL 配置是否正确。"
	default:
		return "模型调用失败：" + err.Error()
	}
}

func (s *Server) addAgentFromConfig(cfg config.AgentConfig) bool {
	a, err := agent.New(cfg)
	if err != nil {
		log.Printf("Failed to create Agent %s: %v", cfg.Name, err)
		return false
	}
	if s.rag != nil {
		a.SetRAG(s.rag)
	}
	if s.skillMgr != nil {
		a.SetSkillManager(s.skillMgr)
	}
	s.configs[cfg.Name] = cfg
	s.agents[cfg.Name] = a
	if err := s.manager.AddAgent(cfg); err != nil {
		log.Printf("Failed to register Agent %s in manager: %v", cfg.Name, err)
	}
	log.Printf("Agent %s created", cfg.Name)
	return true
}

func (s *Server) initRAG() {
	provider := s.runtime.PrimaryProvider()
	embeddingEP := s.runtime.EmbeddingEP
	ragDataDir := s.runtime.RAGDataDir
	localOnly := provider.APIKey == "" || embeddingEP == ""
	if localOnly {
		// 未配置 embedding API Key/endpoint 时仍以本地模式初始化 RAG：
		// 使用本地轻量向量（维度固定、可检索，语义较弱），保证"留空也能跑、不崩溃"。
		log.Println("RAG 以本地模式(local-only)初始化：未配置 embedding API Key/endpoint，使用本地轻量向量；配置后自动升级为远程向量")
	} else {
		log.Printf("RAG embedding using endpoint: %s", embeddingEP)
	}
	r, err := rag.NewRAGManager(provider.APIKey, embeddingEP, ragDataDir, rag.Options{
		MaxChunks:            s.runtime.RAGMaxChunks,
		MaxChunksPerDocument: s.runtime.RAGMaxChunksPerDocument,
		ChunkSize:            s.runtime.RAGChunkSize,
		ChunkOverlap:         s.runtime.RAGChunkOverlap,
		MaxContextChars:      s.runtime.RAGMaxContextChars,
		MaxDocumentChars:     s.runtime.RAGMaxDocumentChars,
		MaxFileBytes:         int64(s.runtime.RAGMaxFileMB) * 1024 * 1024,
		EmbeddingAPIMode:     s.embeddingAPIMode(),
	})
	if err != nil {
		log.Printf("Failed to create RAG: %v", err)
		return
	}
	s.rag = r
	log.Printf("RAG initialized (storage: %s, local-only=%v)", ragDataDir, localOnly)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		provider := s.runtime.PrimaryProvider()
		jsonOK(w, map[string]interface{}{
			"provider": map[string]string{
				"name":       provider.Name,
				"apiKey":     "",
				"chatModel":  provider.ChatModel,
				"endpointId": provider.EndpointID,
				"apiBase":    "https://ark.cn-beijing.volces.com/api/v3",
			},
			"embedding": map[string]string{
				"model":    s.runtime.EmbeddingModel,
				"endpoint": s.runtime.EmbeddingEP,
				"apiBase":  "https://ark.cn-beijing.volces.com/api/v3/embeddings",
			},
			"embeddingStatus": s.embeddingStatus(),
			"rag": map[string]interface{}{
				"dataDir":              s.runtime.RAGDataDir,
				"maxChunks":            s.runtime.RAGMaxChunks,
				"maxChunksPerDocument": s.runtime.RAGMaxChunksPerDocument,
				"chunkSize":            s.runtime.RAGChunkSize,
				"chunkOverlap":         s.runtime.RAGChunkOverlap,
				"maxContextChars":      s.runtime.RAGMaxContextChars,
				"maxDocumentChars":     s.runtime.RAGMaxDocumentChars,
				"maxFileMB":            s.runtime.RAGMaxFileMB,
				"maxUploadMB":          s.runtime.RAGMaxUploadMB,
				"chatTopK":             s.runtime.RAGChatTopK,
				"maxPerSource":         s.runtime.RAGMaxPerSource,
				"minScore":             s.runtime.RAGMinScore,
				"sourceFilter":         s.runtime.RAGSourceFilter,
				"strictContextOnly":    s.runtime.RAGStrictContextOnly,
				"neighborChunks":       s.runtime.RAGNeighborChunks,
			},
			"runtime": map[string]int{
				"memoryLimitMB": s.runtime.MemoryLimitMB,
				"gcPercent":     s.runtime.GCPercent,
			},
			"computer": map[string]interface{}{
				"enabled":           s.runtime.ComputerToolsEnabled,
				"allowedRoots":      s.runtime.ComputerAllowedRoots,
				"allowCommands":     s.runtime.ComputerAllowCommands,
				"allowedCommands":   s.runtime.ComputerAllowedCommands,
				"commandTimeoutSec": s.runtime.ComputerCommandTimeout,
				"requireApproval":   s.runtime.ComputerRequireApproval,
				"desktopEnabled":    s.runtime.ComputerDesktopEnabled,
				"daemonPort":        s.runtime.ComputerDaemonPort,
			},
			"storage": "local-json",
			"effective": map[string]interface{}{
				"configPath":          s.runtime.ConfigPath,
				"envLoaded":           os.Getenv("ARK_API_KEY") != "" || os.Getenv("ARK_ENDPOINT") != "" || os.Getenv("EMBEDDING_EP") != "",
				"providerSource":      configSource(provider.APIKey, os.Getenv("ARK_API_KEY")),
				"chatEndpoint":        provider.EndpointID,
				"chatModel":           provider.ChatModel,
				"chatCallID":          firstNonEmpty(provider.EndpointID, provider.ChatModel),
				"embeddingEndpoint":   s.runtime.EmbeddingEP,
				"embeddingModel":      s.runtime.EmbeddingModel,
				"embeddingCallID":     firstNonEmpty(s.runtime.EmbeddingEP, s.runtime.EmbeddingModel),
				"embeddingWarning":    s.embeddingWarning(),
				"ragDataDir":          s.runtime.RAGDataDir,
				"apiKeyConfigured":    provider.APIKey != "",
				"embeddingConfigured": s.runtime.EmbeddingEP != "",
			},
		})
	case http.MethodPost:
		var req struct {
			Provider  config.ProviderConfig `json:"provider"`
			Embedding struct {
				Model    string `json:"model"`
				Endpoint string `json:"endpoint"`
			} `json:"embedding"`
			RAG struct {
				DataDir              string  `json:"dataDir"`
				MaxChunks            int     `json:"maxChunks"`
				MaxChunksPerDocument int     `json:"maxChunksPerDocument"`
				ChunkSize            int     `json:"chunkSize"`
				ChunkOverlap         int     `json:"chunkOverlap"`
				MaxContextChars      int     `json:"maxContextChars"`
				MaxDocumentChars     int     `json:"maxDocumentChars"`
				MaxFileMB            int     `json:"maxFileMB"`
				MaxUploadMB          int     `json:"maxUploadMB"`
				ChatTopK             int     `json:"chatTopK"`
				MaxPerSource         int     `json:"maxPerSource"`
				MinScore             float64 `json:"minScore"`
				SourceFilter         string  `json:"sourceFilter"`
				StrictContextOnly    bool    `json:"strictContextOnly"`
				NeighborChunks       int     `json:"neighborChunks"`
			} `json:"rag"`
			Runtime struct {
				MemoryLimitMB int `json:"memoryLimitMB"`
				GCPercent     int `json:"gcPercent"`
			} `json:"runtime"`
			Computer *struct {
				Enabled           bool     `json:"enabled"`
				AllowedRoots      []string `json:"allowedRoots"`
				AllowCommands     bool     `json:"allowCommands"`
				AllowedCommands   []string `json:"allowedCommands"`
				CommandTimeoutSec int      `json:"commandTimeoutSec"`
				RequireApproval   bool     `json:"requireApproval"`
				DesktopEnabled    bool     `json:"desktopEnabled"`
				DaemonPort        int      `json:"daemonPort"`
			} `json:"computer"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "Bad request", http.StatusBadRequest)
			return
		}

		cfg := s.runtime
		// 复制一份，避免直接修改正在使用的 s.runtime.Providers（rebuild 会重新加载）。
		preserved := make([]config.ProviderConfig, len(cfg.Providers))
		copy(preserved, cfg.Providers)
		if len(preserved) == 0 {
			preserved = append(preserved, config.ProviderConfig{Name: "ByteDance Ark", Type: "ark"})
		}
		provider := req.Provider
		current := cfg.PrimaryProvider()
		// 仅更新主商家（Providers[0]），保留其余已配置的商家，避免保存偏好时清空多商家列表。
		primary := &preserved[0]
		if provider.Name != "" {
			primary.Name = provider.Name
		}
		// 归一化主商家名，保证 .env 键名稳定为 ARK_API_KEY（兼容旧配置 "ByteDance Ark"）。
		if primary.Name == "" || primary.Name == "ByteDance Ark" {
			primary.Name = "Ark"
		}
		if provider.ChatModel != "" {
			primary.ChatModel = provider.ChatModel
		}
		if provider.EndpointID != "" {
			primary.EndpointID = provider.EndpointID
		}
		submittedAPIKey := strings.TrimSpace(provider.APIKey)
		envKey := config.EnvKeyForProvider(primary.Name)
		if submittedAPIKey != "" {
			if err := s.saveEnvValues(map[string]string{envKey: submittedAPIKey}); err != nil {
				jsonError(w, "Failed to save API Key to .env: "+err.Error(), http.StatusInternalServerError)
				return
			}
			os.Setenv(envKey, submittedAPIKey)
			primary.APIKey = submittedAPIKey
		} else if envVal := os.Getenv(envKey); envVal != "" {
			primary.APIKey = envVal
		} else if current.APIKey != "" {
			primary.APIKey = current.APIKey
		}
		cfg.Providers = preserved
		if req.Embedding.Model != "" {
			cfg.EmbeddingModel = req.Embedding.Model
		}
		if req.Embedding.Endpoint != "" {
			cfg.EmbeddingEP = req.Embedding.Endpoint
		}
		if req.RAG.DataDir != "" {
			cfg.RAGDataDir = req.RAG.DataDir
		}
		if req.RAG.MaxChunks > 0 {
			cfg.RAGMaxChunks = req.RAG.MaxChunks
		}
		if req.RAG.MaxChunksPerDocument > 0 {
			cfg.RAGMaxChunksPerDocument = req.RAG.MaxChunksPerDocument
		}
		if req.RAG.ChunkSize > 0 {
			cfg.RAGChunkSize = req.RAG.ChunkSize
		}
		if req.RAG.ChunkOverlap >= 0 {
			cfg.RAGChunkOverlap = req.RAG.ChunkOverlap
		}
		if req.RAG.MaxContextChars > 0 {
			cfg.RAGMaxContextChars = req.RAG.MaxContextChars
		}
		if req.RAG.MaxDocumentChars > 0 {
			cfg.RAGMaxDocumentChars = req.RAG.MaxDocumentChars
		}
		if req.RAG.MaxFileMB > 0 {
			cfg.RAGMaxFileMB = req.RAG.MaxFileMB
		}
		if req.RAG.MaxUploadMB > 0 {
			cfg.RAGMaxUploadMB = req.RAG.MaxUploadMB
		}
		if req.RAG.ChatTopK > 0 {
			cfg.RAGChatTopK = req.RAG.ChatTopK
		}
		if req.RAG.MaxPerSource >= 0 {
			cfg.RAGMaxPerSource = req.RAG.MaxPerSource
		}
		if req.RAG.MinScore >= 0 {
			cfg.RAGMinScore = req.RAG.MinScore
		}
		cfg.RAGSourceFilter = req.RAG.SourceFilter
		cfg.RAGStrictContextOnly = req.RAG.StrictContextOnly
		if req.RAG.NeighborChunks >= 0 {
			cfg.RAGNeighborChunks = req.RAG.NeighborChunks
		}
		if req.Runtime.MemoryLimitMB > 0 {
			cfg.MemoryLimitMB = req.Runtime.MemoryLimitMB
		}
		if req.Runtime.GCPercent > 0 {
			cfg.GCPercent = req.Runtime.GCPercent
		}
		if req.Computer != nil {
			cfg.ComputerToolsEnabled = req.Computer.Enabled
			if req.Computer.AllowedRoots != nil {
				cfg.ComputerAllowedRoots = compactStrings(req.Computer.AllowedRoots)
			}
			cfg.ComputerAllowCommands = req.Computer.AllowCommands
			if req.Computer.AllowedCommands != nil {
				cfg.ComputerAllowedCommands = compactStrings(req.Computer.AllowedCommands)
			}
			if req.Computer.CommandTimeoutSec > 0 {
				cfg.ComputerCommandTimeout = req.Computer.CommandTimeoutSec
			}
			cfg.ComputerRequireApproval = req.Computer.RequireApproval
			cfg.ComputerDesktopEnabled = req.Computer.DesktopEnabled
			if req.Computer.DaemonPort > 0 {
				cfg.ComputerDaemonPort = req.Computer.DaemonPort
			}
		}
		if err := config.SaveRuntimeConfig(cfg); err != nil {
			jsonError(w, "Failed to save settings: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.rebuild()
		jsonOK(w, map[string]string{"message": "Settings saved"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) rebuild() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents = make(map[string]*agent.Agent)
	s.configs = make(map[string]config.AgentConfig)
	s.rag = nil
	s.manager = agent.NewAgentManager()
	// 原子地重建 orchestrator，使其指向新的 manager/agents；
	// handler 在 RLock 下读取该指针，故并发请求只会看到旧或新之一，不会出现撕裂读。
	s.orchestrator = agent.NewOrchestrator(s.manager)
	s.loadRuntimeConfig()
	s.wireAgentTools()
	s.applyRuntimeLimits()
	s.loadFromEnv()
	s.ensureDefaultAgent()
	s.initRAG()
	for _, a := range s.agents {
		if s.rag != nil {
			a.SetRAG(s.rag)
		}
		if s.skillMgr != nil {
			a.SetSkillManager(s.skillMgr)
		}
	}
	if s.rag != nil {
		s.manager.SetRAG(s.rag)
	}
}

func (s *Server) chatWithImage(ctx context.Context, cfg config.AgentConfig, text, imageBase64 string) (string, error) {
	imgData := imageBase64
	if strings.HasPrefix(imgData, "data:") {
		parts := strings.Split(imgData, ",")
		if len(parts) == 2 {
			imgData = parts[1]
		}
	}
	body := map[string]interface{}{
		"model": cfg.ModelID,
		"messages": []map[string]interface{}{
			{"role": "system", "content": cfg.SystemPrompt},
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "text", "text": text},
					{
						"type": "image_url",
						"image_url": map[string]string{
							"url": "data:image/jpeg;base64," + imgData,
						},
					},
				},
			},
		},
	}
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST",
		chatEndpoint(cfg),
		strings.NewReader(string(jsonBody)),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	resp, err := arkHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("模型服务调用失败: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("模型服务返回错误 (%d): %s", resp.StatusCode, string(respBody))
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("Failed to parse response: %v", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("No result from model")
	}
	return result.Choices[0].Message.Content, nil
}

// chatEndpoint 按商家类型选择模型推理接入点：ark 走火山方舟，openai 兼容走配置 BaseURL。
func chatEndpoint(cfg config.AgentConfig) string {
	if cfg.ProviderType == "ark" {
		return "https://ark.cn-beijing.volces.com/api/v3/chat/completions"
	}
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	return strings.TrimRight(base, "/") + "/chat/completions"
}

// chatWithAttachments 处理带结构化附件（图片/文件）的非流式对话。
// 图片以多模态 UserInputMultiContent 形式发给模型，文本/二进制文件已内嵌在 message 文本中。
func (s *Server) chatWithAttachments(ctx context.Context, cfg config.AgentConfig, userMsg *schema.Message) (string, error) {
	body := map[string]interface{}{
		"model": cfg.ModelID,
		"messages": []map[string]interface{}{
			{"role": "system", "content": cfg.SystemPrompt},
			{"role": "user", "multi_content": toMultiContentJSON(userMsg)},
		},
	}
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST",
		chatEndpoint(cfg),
		strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	resp, err := arkHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("模型服务调用失败: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("模型服务返回错误 (%d): %s", resp.StatusCode, string(respBody))
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("Failed to parse response: %v", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("No result from model")
	}
	return result.Choices[0].Message.Content, nil
}

// toMultiContentJSON 把 schema.Message 的多模态输入转换为 Ark 兼容的 JSON 片段。
func toMultiContentJSON(msg *schema.Message) []map[string]interface{} {
	parts := make([]map[string]interface{}, 0)
	if msg.Content != "" {
		parts = append(parts, map[string]interface{}{
			"type": "text",
			"text": msg.Content,
		})
	}
	for _, p := range msg.UserInputMultiContent {
		if p.Type == schema.ChatMessagePartTypeImageURL && p.Image != nil {
			url := ""
			if p.Image.Base64Data != nil {
				url = "data:" + p.Image.MIMEType + ";base64," + *p.Image.Base64Data
			} else if p.Image.URL != nil {
				url = *p.Image.URL
			}
			parts = append(parts, map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]string{"url": url},
			})
		}
	}
	return parts
}

func (s *Server) resolveRAGRequestOptions(topK, maxPerSource int, minScore float64, sourceFilter string, sourceFiles []string, strictOnly bool) (int, rag.SearchOptions, bool) {
	if topK <= 0 {

		topK = s.runtime.RAGChatTopK
	}
	if topK <= 0 {
		topK = 5
	}
	if topK > 20 {
		topK = 20
	}
	if maxPerSource < 0 {
		maxPerSource = 0
	}
	if sourceFilter == "" {
		sourceFilter = s.runtime.RAGSourceFilter
	}
	if minScore <= 0 {
		minScore = s.runtime.RAGMinScore
	}
	return topK, rag.SearchOptions{
		SourceFiles:    compactStrings(sourceFiles),
		SourceQuery:    sourceFilter,
		MaxPerSource:   maxPerSource,
		MinScore:       minScore,
		NeighborChunks: s.runtime.RAGNeighborChunks,
	}, strictOnly || s.runtime.RAGStrictContextOnly
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func configSource(value, envValue string) string {
	if value == "" {
		return "未配置"
	}
	if envValue != "" && value == envValue {
		return ".env"
	}
	return "data/config.json"
}

func (s *Server) saveEnvValues(values map[string]string) error {
	envPath := s.envFilePath()
	lines := []string{}
	if data, err := os.ReadFile(envPath); err == nil {
		lines = strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	}
	seen := make(map[string]bool)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "=") {
			continue
		}
		key := strings.TrimSpace(strings.SplitN(trimmed, "=", 2)[0])
		if value, ok := values[key]; ok {
			lines[i] = key + "=" + value
			seen[key] = true
		}
	}
	for key, value := range values {
		if !seen[key] {
			if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
				lines = append(lines, "")
			}
			lines = append(lines, key+"="+value)
		}
	}
	if err := os.MkdirAll(filepath.Dir(envPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0600)
}

// envFilePath 解析 .env 的实际落盘路径。
// 关键：不能用 os.Executable()（go run 下指向 %TEMP%/go-build 临时目录），
// 否则设置页写入的 API Key 会落到临时目录而永远不被加载。
// 改为基于当前工作目录解析，并优先命中已经存在的 .env（兼容从项目根或 eino/ 启动）。
func (s *Server) envFilePath() string {
	if cwd, err := os.Getwd(); err == nil {
		candidates := []string{
			filepath.Join(cwd, ".env"),
			filepath.Join(cwd, "eino", ".env"),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		return filepath.Join(cwd, ".env")
	}
	return filepath.Join(".", ".env")
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	type agentInfo struct {
		Name          string `json:"name"`
		Model         string `json:"model"`
		SystemPrompt  string `json:"systemPrompt"`
		NeedTools     bool   `json:"needTools"`
		Locked        bool   `json:"locked"`
	}
	list := make([]agentInfo, 0)
	hasDefault := false
	s.mu.RLock()
	for _, cfg := range s.configs {
		list = append(list, agentInfo{Name: cfg.Name, Model: cfg.ModelID, SystemPrompt: cfg.SystemPrompt, NeedTools: cfg.NeedTools, Locked: cfg.Locked})
		if cfg.Name == config.DefaultAgentName && cfg.Locked {
			hasDefault = true
		}
	}
	s.mu.RUnlock()
	// 主控智能体是系统内置的、不可删除的协调者，必须始终可见。
	// 极端情况下（如 rebuild 期间凭据暂未就绪）configs 中可能缺失，此处强制补充。
	if !hasDefault {
		list = append([]agentInfo{{
			Name:         config.DefaultAgentName,
			Model:        "(待配置 API Key)",
			SystemPrompt: config.DefaultAgentSystemPrompt(),
			NeedTools:    true,
			Locked:       true,
		}}, list...)
		// 同步回写，确保持久化与内存一致
		cfg := config.AgentConfig{
			Name:         config.DefaultAgentName,
			SystemPrompt: config.DefaultAgentSystemPrompt(),
			NeedTools:    true,
			Locked:       true,
		}
		s.mu.Lock()
		s.configs[config.DefaultAgentName] = cfg
		s.mu.Unlock()
		s.saveAgentsToConfig()
	}
	jsonOK(w, list)
}

// requestOwner 从已鉴权的请求上下文中取出当前登录用户 ID。
// 用于把 RAG 文档与检索按用户隔离，避免不同用户互相看到对方的私有资料（含公司机密）。
func (s *Server) requestOwner(r *http.Request) string {
	if u, ok := auth.UserFromContext(r.Context()); ok && u != nil {
		return u.UserID
	}
	return ""
}

func (s *Server) handleRagUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.rag == nil {
		jsonError(w, "RAG not initialized", http.StatusInternalServerError)
		return
	}
	var req struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	if s.runtime.RAGMaxUploadMB > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, int64(s.runtime.RAGMaxUploadMB)*1024*1024)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Bad request", http.StatusBadRequest)
		return
	}
	if req.ID == "" || req.Content == "" {
		jsonError(w, "id and content are required", http.StatusBadRequest)
		return
	}
	ragDataDir := s.ragOriginalsDir()
	if err := os.MkdirAll(ragDataDir, 0755); err != nil {
		jsonError(w, "Failed to create RAG data dir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	filePath := filepath.Join(ragDataDir, filepath.Base(req.ID))
	if err := os.WriteFile(filePath, []byte(req.Content), 0644); err != nil {
		log.Printf("Failed to save file: %v", err)
	} else {
		log.Printf("File saved: %s", filePath)
	}
	owner := s.requestOwner(r)
	ctx := context.Background()
	err := s.rag.AddText(ctx, filepath.Base(req.ID), req.Content, map[string]string{"source": filePath, "type": "file", "ext": filepath.Ext(req.ID)}, owner)
	if err != nil {
		jsonError(w, "Failed to add document: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]interface{}{
		"message":     "Upload successful",
		"count":       s.rag.Count(),
		"chunkCount":  s.rag.Count(),
		"sourceCount": s.rag.SourceFileCount(),
		"file":        filePath,
	})
}

func (s *Server) handleRagUploadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.rag == nil {
		jsonError(w, "RAG not initialized", http.StatusInternalServerError)
		return
	}
	if s.runtime.RAGMaxUploadMB > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, int64(s.runtime.RAGMaxUploadMB)*1024*1024)
	}
	if err := r.ParseMultipartForm(int64(s.runtime.RAGMaxUploadMB) * 1024 * 1024); err != nil {
		jsonError(w, "Bad upload: "+err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		jsonError(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ragDataDir := s.ragOriginalsDir()
	if err := os.MkdirAll(ragDataDir, 0755); err != nil {
		jsonError(w, "Failed to create RAG data dir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	filePath := filepath.Join(ragDataDir, filepath.Base(header.Filename))
	out, err := os.Create(filePath)
	if err != nil {
		jsonError(w, "Failed to save file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		jsonError(w, "Failed to save file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	out.Close()

	owner := s.requestOwner(r)
	ctx := context.Background()
	if err := s.rag.AddFile(ctx, filePath, owner); err != nil {
		failedPath := s.moveRAGFileToFailed(filePath)
		if failedPath != "" {
			jsonError(w, "Failed to index file: "+err.Error()+"; moved to failed: "+failedPath, http.StatusInternalServerError)
			return
		}
		jsonError(w, "Failed to index file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]interface{}{
		"message":     "Upload successful",
		"count":       s.rag.Count(),
		"chunkCount":  s.rag.Count(),
		"sourceCount": s.rag.SourceFileCount(),
		"file":        filePath,
	})
}

func (s *Server) handleRagCount(w http.ResponseWriter, r *http.Request) {
	if s.rag == nil {
		jsonOK(w, map[string]interface{}{"count": 0, "chunkCount": 0, "sourceCount": 0, "initialized": false})
		return
	}
	jsonOK(w, map[string]interface{}{
		"count":       s.rag.Count(),
		"chunkCount":  s.rag.Count(),
		"sourceCount": s.rag.SourceFileCount(),
		"initialized": true,
	})
}

func (s *Server) handleRagStatus(w http.ResponseWriter, r *http.Request) {
	s.ensureRAGLayoutDirs()
	status := map[string]interface{}{
		"initialized":          s.rag != nil,
		"count":                0,
		"dataDir":              s.runtime.RAGDataDir,
		"originalsDir":         s.ragOriginalsDir(),
		"indexesDir":           s.ragIndexesDir(),
		"failedDir":            s.ragFailedDir(),
		"tmpDir":               s.ragTmpDir(),
		"exportsDir":           s.ragExportsDir(),
		"indexCount":           s.ragIndexCount(),
		"embeddingEP":          s.runtime.EmbeddingEP,
		"embeddingModel":       s.runtime.EmbeddingModel,
		"embeddingStatus":      s.embeddingStatus(),
		"embeddingWarning":     s.embeddingWarning(),
		"maxChunks":            s.runtime.RAGMaxChunks,
		"maxChunksPerDocument": s.runtime.RAGMaxChunksPerDocument,
		"chunkSize":            s.runtime.RAGChunkSize,
		"chunkOverlap":         s.runtime.RAGChunkOverlap,
		"maxContextChars":      s.runtime.RAGMaxContextChars,
		"maxDocumentChars":     s.runtime.RAGMaxDocumentChars,
		"maxFileMB":            s.runtime.RAGMaxFileMB,
		"maxUploadMB":          s.runtime.RAGMaxUploadMB,
		"neighborChunks":       s.runtime.RAGNeighborChunks,
		"sourceFiles":          s.listRAGSourceFiles(),
		"sourceFileDetails":    s.listRAGSourceFileDetails(),
		"failedFiles":          s.listRAGFailedFiles(),
	}
	if s.rag != nil {
		status["count"] = s.rag.Count()
		status["chunkCount"] = s.rag.Count()
		status["sourceCount"] = s.rag.SourceFileCount()
	}
	jsonOK(w, status)
}

func (s *Server) listRAGSourceFiles() []string {
	details := s.listRAGSourceFileDetails()
	files := make([]string, 0, len(details))
	for _, detail := range details {
		name, _ := detail["name"].(string)
		files = append(files, name)
	}
	return files
}

func (s *Server) listRAGSourceFileDetails() []map[string]interface{} {
	dir := s.ragOriginalsDir()
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	supported := map[string]bool{
		".txt": true, ".md": true, ".docx": true, ".pdf": true,
		".xlsx": true, ".xls": true, ".pptx": true, ".ppt": true,
		".csv": true, ".xml": true, ".html": true, ".log": true, ".json": true,
	}
	files := make([]map[string]interface{}, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if !supported[ext] {
			continue
		}
		path := filepath.Join(dir, name)
		indexPath := filepath.Join(s.ragIndexesDir(), name+".json")
		_, indexErr := os.Stat(indexPath)
		info, _ := entry.Info()
		problem := ""
		if strings.EqualFold(ext, ".docx") && !isValidDocx(path) {
			problem = "damaged docx, re-upload original"
		}
		detail := map[string]interface{}{
			"name":      name,
			"path":      path,
			"ext":       ext,
			"indexed":   indexErr == nil,
			"indexPath": indexPath,
			"problem":   problem,
		}
		if info != nil {
			detail["sizeBytes"] = info.Size()
			detail["modifiedAt"] = info.ModTime().Format(time.RFC3339)
		}
		files = append(files, detail)
	}
	return files
}

func (s *Server) listRAGFailedFiles() []map[string]interface{} {
	dir := s.ragFailedDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	files := make([]map[string]interface{}, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, _ := entry.Info()
		detail := map[string]interface{}{
			"name": entry.Name(),
			"path": filepath.Join(dir, entry.Name()),
		}
		if info != nil {
			detail["sizeBytes"] = info.Size()
			detail["modifiedAt"] = info.ModTime().Format(time.RFC3339)
		}
		files = append(files, detail)
	}
	return files
}

func (s *Server) ensureRAGLayoutDirs() {
	for _, dir := range []string{s.ragOriginalsDir(), s.ragIndexesDir(), s.ragFailedDir(), s.ragTmpDir(), s.ragExportsDir()} {
		if dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				log.Printf("create RAG layout dir %s failed: %v", dir, err)
			}
		}
	}
}

func isValidDocx(path string) bool {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return false
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name == "word/document.xml" {
			return true
		}
	}
	return false
}

func (s *Server) ragOriginalsDir() string {
	if s.rag != nil {
		return s.rag.OriginalsDir()
	}
	dir := s.runtime.RAGDataDir
	if dir == "" {
		dir = filepath.Join(".", "data")
	}
	return filepath.Join(dir, "originals")
}

func (s *Server) ragIndexesDir() string {
	if s.rag != nil {
		return s.rag.IndexesDir()
	}
	dir := s.runtime.RAGDataDir
	if dir == "" {
		dir = filepath.Join(".", "data")
	}
	return filepath.Join(dir, "indexes")
}

func (s *Server) ragFailedDir() string {
	if s.rag != nil {
		return s.rag.FailedDir()
	}
	dir := s.runtime.RAGDataDir
	if dir == "" {
		dir = filepath.Join(".", "data")
	}
	return filepath.Join(dir, "failed")
}

func (s *Server) ragTmpDir() string {
	if s.rag != nil {
		return s.rag.TmpDir()
	}
	dir := s.runtime.RAGDataDir
	if dir == "" {
		dir = filepath.Join(".", "data")
	}
	return filepath.Join(dir, "tmp")
}

func (s *Server) ragExportsDir() string {
	if s.rag != nil {
		return s.rag.ExportsDir()
	}
	dir := s.runtime.RAGDataDir
	if dir == "" {
		dir = filepath.Join(".", "data")
	}
	return filepath.Join(dir, "exports")
}

func (s *Server) moveRAGFileToFailed(filePath string) string {
	if filePath == "" {
		return ""
	}
	failedDir := s.ragFailedDir()
	if err := os.MkdirAll(failedDir, 0755); err != nil {
		log.Printf("create failed dir failed: %v", err)
		return ""
	}
	name := filepath.Base(filePath)
	target := filepath.Join(failedDir, name)
	if _, err := os.Stat(target); err == nil {
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		target = filepath.Join(failedDir, base+"-"+time.Now().Format("20060102150405")+ext)
	}
	if err := os.Rename(filePath, target); err != nil {
		log.Printf("move failed RAG file %s to %s failed: %v", filePath, target, err)
		return ""
	}
	return target
}

func (s *Server) ragIndexCount() int {
	entries, err := os.ReadDir(s.ragIndexesDir())
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			count++
		}
	}
	return count
}

func (s *Server) handleRagSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.rag == nil {
		jsonError(w, "RAG not initialized", http.StatusInternalServerError)
		return
	}
	var req struct {
		Query        string   `json:"query"`
		TopK         int      `json:"topK"`
		SourceFiles  []string `json:"sourceFiles"`
		SourceFilter string   `json:"sourceFilter"`
		MaxPerSource int      `json:"maxPerSource"`
		MinScore     float64  `json:"minScore"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Bad request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		jsonError(w, "query required", http.StatusBadRequest)
		return
	}
	if req.TopK <= 0 || req.TopK > 20 {
		req.TopK = 5
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, ragOptions, _ := s.resolveRAGRequestOptions(req.TopK, req.MaxPerSource, req.MinScore, req.SourceFilter, req.SourceFiles, false)
	ragOptions.Owner = s.requestOwner(r)
	results, err := s.rag.SearchWithOptions(ctx, req.Query, req.TopK, ragOptions)
	if err != nil {
		jsonError(w, "Search failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	type result struct {
		ID             string            `json:"id"`
		FileName       string            `json:"fileName"`
		Source         string            `json:"source"`
		ChunkIndex     int               `json:"chunkIndex"`
		Chunk          string            `json:"chunk"`
		Score          float64           `json:"score"`
		MatchType      string            `json:"matchType"`
		NeighborOffset int               `json:"neighborOffset"`
		ParentID       string            `json:"parentId"`
		Metadata       map[string]string `json:"metadata"`
	}
	resp := make([]result, 0, len(results))
	for _, item := range results {
		matchType := item.MatchType
		if matchType == "" {
			matchType = "hit"
		}
		resp = append(resp, result{
			ID:             item.Document.ID,
			FileName:       rag.SourceFileName(item.Document),
			Source:         rag.SourcePath(item.Document),
			ChunkIndex:     rag.ChunkIndex(item.Document),
			Chunk:          rag.CleanTextForQuery(item.Document.Chunk, req.Query),
			Score:          item.Score,
			MatchType:      matchType,
			NeighborOffset: item.NeighborOffset,
			ParentID:       item.ParentID,
			Metadata:       item.Document.Metadata,
		})
	}
	jsonOK(w, map[string]interface{}{
		"results": resp,
		"count":   len(resp),
	})
}

// handleProviders 管理"模型提供商"列表。
// GET：返回已配置商家（不含明文 Key）+ 内置商家预设。
// POST：整体保存商家列表；每个商家的 Key 写入 .env（<NAME>_API_KEY），不落盘到 config.json。
func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		type providerEntry struct {
			Name       string   `json:"name"`
			Type       string   `json:"type"`
			BaseURL    string   `json:"baseURL"`
			ChatModel  string   `json:"chatModel"`
			EndpointID string   `json:"endpointId,omitempty"`
			Models     []string `json:"models,omitempty"`
			HasKey     bool     `json:"hasKey"`
		}
		configured := make([]providerEntry, 0, len(s.runtime.Providers))
		for _, p := range s.runtime.Providers {
			configured = append(configured, providerEntry{
				Name:       p.Name,
				Type:       p.Type,
				BaseURL:    p.BaseURL,
				ChatModel:  p.ChatModel,
				EndpointID: p.EndpointID,
				Models:     p.Models,
				HasKey:     p.APIKey != "",
			})
		}
		jsonOK(w, map[string]interface{}{
			"providers": configured,
			"presets":  config.ProviderPresets(),
		})
	case http.MethodPost:
		var req struct {
			Providers []config.ProviderConfig `json:"providers"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "Bad request", http.StatusBadRequest)
			return
		}
		// 校验 + 规范化
		cleaned := make([]config.ProviderConfig, 0, len(req.Providers))
		envUpdates := map[string]string{}
		seen := map[string]bool{}
		for _, p := range req.Providers {
			name := strings.TrimSpace(p.Name)
			if name == "" {
				continue
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			p.Name = name
			if p.Type != "ark" {
				p.Type = "openai"
			}
			key := strings.TrimSpace(p.APIKey)
			p.APIKey = key
			if key != "" {
				envUpdates[config.EnvKeyForProvider(name)] = key
			}
			cleaned = append(cleaned, p)
		}
		if len(envUpdates) > 0 {
			if err := s.saveEnvValues(envUpdates); err != nil {
				jsonError(w, "Failed to save API Keys to .env: "+err.Error(), http.StatusInternalServerError)
				return
			}
			for k, v := range envUpdates {
				os.Setenv(k, v)
			}
		}
		cfg := s.runtime
		preserved := make([]config.ProviderConfig, len(cleaned))
		copy(preserved, cleaned)
		cfg.Providers = preserved
		if err := config.SaveRuntimeConfig(cfg); err != nil {
			jsonError(w, "Failed to save providers: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.rebuild()
		jsonOK(w, map[string]string{"message": "Providers saved"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleDiscoverModels 调中转站/网关的 /models 端点拉取可用模型列表。
// 前端在编辑商家弹窗中一键获取模型名，无需手动翻阅文档。
func (s *Server) handleDiscoverModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		BaseURL string `json:"baseURL"`
		APIKey  string `json:"apiKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "请求体解析失败："+err.Error(), http.StatusBadRequest)
		return
	}
	baseURL := strings.TrimRight(body.BaseURL, "/")
	if baseURL == "" {
		jsonError(w, "gateway地址不能为空", http.StatusBadRequest)
		return
	}

	// 构造 /models 地址：大多数 OpenAI 兼容网关在 base/v1/models 或 base/models
	candidates := []string{}
	if strings.HasSuffix(baseURL, "/v1") {
		candidates = append(candidates, baseURL+"/models")
	} else {
		candidates = append(candidates, baseURL+"/v1/models", baseURL+"/models")
	}

	var lastErr error
	for _, url := range candidates {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		req.Header.Set("Accept", "application/json")
		if body.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+body.APIKey)
		}
		resp, err := arkHTTPClient.Do(req)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
			continue
		}

		// OpenAI /v1/models 响应格式：{"data":[{"id":"gpt-4o",...}]}
		var result struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			lastErr = fmt.Errorf("模型列表解析失败：%w", err)
			continue
		}

		ids := make([]string, 0, len(result.Data))
		seen := map[string]bool{}
		for _, m := range result.Data {
			id := strings.TrimSpace(m.ID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			lastErr = fmt.Errorf("网关返回了空模型列表")
			continue
		}
		jsonOK(w, map[string]interface{}{"models": ids, "baseURL": url})
		return
	}

	jsonError(w, "无法从网关拉取模型列表："+lastErr.Error(), http.StatusBadGateway)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	type modelOption struct {
		Value      string `json:"value"`
		Label      string `json:"label"`
		Kind       string `json:"kind"`
		Provider   string `json:"provider"`
		Note       string `json:"note"`
		Configured bool `json:"configured"`
		Free       bool `json:"free"`
	}
	chatModels := []string{"deepseek-chat"}
	chatOptions := []modelOption{}
	seen := map[string]bool{} // 按 Value 去重（已配置商家 / 预设模型可能重叠）
	// 商家是否已配置（有 API Key）
	providerConfigured := func(name string) bool {
		for _, p := range s.runtime.Providers {
			if p.Name == name && p.APIKey != "" {
				return true
			}
		}
		return false
	}
	addOption := func(o modelOption) {
		if o.Value == "" || seen[o.Value] {
			return
		}
		seen[o.Value] = true
		chatOptions = append(chatOptions, o)
		chatModels = append(chatModels, o.Value)
	}
	for _, provider := range s.runtime.Providers {
		providerName := provider.Name
		if providerName == "" {
			providerName = "模型供应商"
		}
		if provider.EndpointID != "" {
			label := provider.EndpointID
			if provider.ChatModel != "" {
				label = provider.ChatModel + " / " + provider.EndpointID
			}
			addOption(modelOption{
				Value:      provider.EndpointID,
				Label:      label,
				Kind:       "endpoint",
				Provider:   providerName,
				Note:       "已配置",
				Configured: true,
			})
		}
		if provider.ChatModel != "" && provider.ChatModel != provider.EndpointID {
			addOption(modelOption{
				Value:      provider.ChatModel,
				Label:      provider.ChatModel,
				Kind:       "model",
				Provider:   providerName,
				Note:       "已配置",
				Configured: true,
			})
		}
		// 已配置商家：若曾"拉取并保存"过模型清单（provider.Models），
		// 则把清单内全部模型都暴露到聊天模型选择器，这样用户可随时切换，无需反复添加商家。
		for _, m := range provider.Models {
			if m == "" || m == provider.EndpointID || m == provider.ChatModel {
				continue
			}
			addOption(modelOption{
				Value:      m,
				Label:      m,
				Kind:       "model",
				Provider:   providerName,
				Note:       "已配置",
				Configured: true,
			})
		}
	}
	// 并入内置商家预设的模型清单：未配置的商家其模型标注"未配置"，前端置灰。
	for _, preset := range config.ProviderPresets() {
		pname := preset.Name
		if pname == "" {
			pname = "模型供应商"
		}
		conf := providerConfigured(preset.Name)
		isFree := strings.Contains(pname, "免费") || strings.Contains(pname, "Free")
		note := "未配置"
		if isFree {
			note = "免费"
		} else if conf {
			note = "已配置"
		}
		for _, m := range preset.Models {
			if m == "" {
				continue
			}
			addOption(modelOption{
				Value:      m,
				Label:      m,
				Kind:       "preset",
				Provider:   pname,
				Note:       note,
				Configured: conf,
				Free:       isFree,
			})
		}
	}
	if len(chatOptions) == 0 {
		addOption(modelOption{
			Value:      "deepseek-chat",
			Label:      "deepseek-chat",
			Kind:       "model",
			Provider:   "DeepSeek",
			Note:       "未配置",
			Configured: false,
			Free:       false,
		})
	}
	embeddingModels := []string{}
	embeddingOptions := []modelOption{}
	if s.runtime.EmbeddingEP != "" {
		embeddingModels = append(embeddingModels, s.runtime.EmbeddingEP)
		label := s.runtime.EmbeddingEP
		if s.runtime.EmbeddingModel != "" {
			label = s.runtime.EmbeddingModel + " / " + s.runtime.EmbeddingEP
		}
		embeddingOptions = append(embeddingOptions, modelOption{
			Value:    s.runtime.EmbeddingEP,
			Label:    label,
			Kind:     "endpoint",
			Provider: "火山方舟",
			Note:     "RAG 向量检索接入点",
		})
	}
	if s.runtime.EmbeddingModel != "" && s.runtime.EmbeddingModel != s.runtime.EmbeddingEP {
		embeddingModels = append(embeddingModels, s.runtime.EmbeddingModel)
	}
	if len(embeddingModels) == 0 {
		embeddingModels = []string{"doubao-embedding"}
	}
	jsonOK(w, map[string]interface{}{
		"chatModels":       chatModels,
		"embeddingModels":  embeddingModels,
		"chatOptions":      chatOptions,
		"embeddingOptions": embeddingOptions,
	})
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	toolList, err := agent.GetAllTools(true) // 展示协调者视角的完整工具集
	if err != nil {
		jsonError(w, "Failed to load tools: "+err.Error(), http.StatusInternalServerError)
		return
	}
	type toolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	result := make([]toolInfo, 0, len(toolList))
	ctx := context.Background()
	for _, t := range toolList {
		info, err := t.Info(ctx)
		if err != nil {
			continue
		}
		result = append(result, toolInfo{Name: info.Name, Description: info.Desc})
	}
	jsonOK(w, map[string]interface{}{"tools": result})
}

func (s *Server) handleRuntimeMemory(w http.ResponseWriter, r *http.Request) {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	jsonOK(w, map[string]interface{}{
		"allocMB":        bytesToMB(stats.Alloc),
		"totalAllocMB":   bytesToMB(stats.TotalAlloc),
		"sysMB":          bytesToMB(stats.Sys),
		"heapAllocMB":    bytesToMB(stats.HeapAlloc),
		"heapInuseMB":    bytesToMB(stats.HeapInuse),
		"heapIdleMB":     bytesToMB(stats.HeapIdle),
		"heapReleasedMB": bytesToMB(stats.HeapReleased),
		"numGC":          stats.NumGC,
		"memoryLimitMB":  s.runtime.MemoryLimitMB,
		"gcPercent":      s.runtime.GCPercent,
		"ragChunks":      s.ragCount(),
	})
}

func (s *Server) handleRuntimeGC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	runtime.GC()
	debug.FreeOSMemory()
	s.handleRuntimeMemory(w, r)
}

func (s *Server) ragCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.rag == nil {
		return 0
	}
	return s.rag.Count()
}

func (s *Server) embeddingStatus() rag.EmbeddingStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.rag == nil {
		return rag.EmbeddingStatus{
			ModelID:              s.runtime.EmbeddingEP,
			APIBase:              embeddingAPIBase(s.embeddingAPIMode()),
			APIMode:              s.embeddingAPIMode(),
			LastMode:             "not_initialized",
			LocalFallbackEnabled: os.Getenv("RAG_LOCAL_EMBEDDING_FALLBACK") != "false",
		}
	}
	return s.rag.EmbeddingStatus()
}

func (s *Server) embeddingAPIMode() string {
	name := strings.ToLower(s.runtime.EmbeddingModel + " " + s.runtime.EmbeddingEP)
	if strings.Contains(name, "vision") || strings.Contains(name, "multimodal") {
		return "multimodal"
	}
	return "auto"
}

func embeddingAPIBase(mode string) string {
	if mode == "multimodal" {
		return "https://ark.cn-beijing.volces.com/api/v3/embeddings/multimodal"
	}
	return "https://ark.cn-beijing.volces.com/api/v3/embeddings"
}

func (s *Server) embeddingWarning() string {
	name := strings.ToLower(s.runtime.EmbeddingModel + " " + s.runtime.EmbeddingEP)
	if strings.Contains(name, "vision") {
		return "当前配置是 vision 多模态向量模型；后端会按官方示例直接使用 multimodal embeddings，纯文字 RAG 可以使用。"
	}
	return ""
}

func bytesToMB(v uint64) float64 {
	return float64(v) / 1024 / 1024
}

func (s *Server) handleTestEmbedding(w http.ResponseWriter, r *http.Request) {
	provider := s.runtime.PrimaryProvider()
	if provider.APIKey == "" {
		jsonError(w, "API Key not configured", http.StatusBadRequest)
		return
	}
	ctx := context.Background()
	testText := "test"
	embeddingEP := s.runtime.EmbeddingEP
	var lastErr error
	if embeddingEP != "" {
		log.Printf("Trying endpoint: %s", embeddingEP)
		emb, err := rag.NewEmbedder(provider.APIKey, embeddingEP)
		if err == nil {
			emb.SetAPIMode(s.embeddingAPIMode())
			emb.SetLocalFallback(false)
			_, err = emb.EmbedText(ctx, testText)
			if err == nil {
				jsonOK(w, map[string]interface{}{
					"success": true, "method": "endpoint", "value": embeddingEP,
					"message": "Embedding test successful!",
					"status":  emb.Status(),
				})
				return
			}
			lastErr = err
			log.Printf("Endpoint call failed: %v", err)
		} else {
			lastErr = err
			log.Printf("Endpoint create failed: %v", err)
		}
	}
	if lastErr != nil {
		jsonError(w, fmt.Sprintf("Embedding test failed. Endpoint=%s. Reason=%v", embeddingEP, lastErr), http.StatusInternalServerError)
		return
	}
	jsonError(w, fmt.Sprintf("Embedding test failed. Endpoint=%s", embeddingEP), http.StatusInternalServerError)
}

func (s *Server) handleRagScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.rag == nil {
		jsonError(w, "RAG not initialized", http.StatusInternalServerError)
		return
	}
	var req struct {
		DirPath string `json:"dirPath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DirPath == "" {
		jsonError(w, "dirPath required", http.StatusBadRequest)
		return
	}
	ctx := context.Background()
	s.ensureRAGLayoutDirs()
	// 管理员扫描目录导入的是"公共知识库"，owner 留空表示所有人可见。
	count, err := s.rag.Rebuild(ctx, req.DirPath, "")
	if err != nil {
		jsonError(w, "Scan failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]interface{}{
		"message":     "Scan complete",
		"files":       count,
		"count":       s.rag.Count(),
		"chunkCount":  s.rag.Count(),
		"sourceCount": s.rag.SourceFileCount(),
	})
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	dirPath := r.URL.Query().Get("path")
	if dirPath == "" {
		dirPath = "."
	}
	if runtime.GOOS == "windows" {
		if len(dirPath) == 2 && dirPath[1] == ':' {
			dirPath = dirPath + `\`
		}
		dirPath = strings.ReplaceAll(dirPath, "/", `\`)
	}
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		jsonError(w, "Cannot read directory: "+err.Error(), http.StatusBadRequest)
		return
	}
	type dirEntry struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"isDir"`
	}
	var dirs []dirEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		childPath := filepath.Join(dirPath, e.Name())
		dirs = append(dirs, dirEntry{Name: e.Name(), Path: childPath, IsDir: true})
	}
	parent := filepath.Dir(dirPath)
	if parent != dirPath {
		dirs = append([]dirEntry{{Name: "..", Path: parent, IsDir: true}}, dirs...)
	}
	jsonOK(w, map[string]interface{}{
		"current": dirPath,
		"dirs":    dirs,
	})
}

func (s *Server) handleAgentCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		OldName      string `json:"oldName"`
		Name         string `json:"name"`
		ModelID      string `json:"modelID"`
		Provider     string `json:"provider"`
		SystemPrompt string `json:"systemPrompt"`
		Role         string `json:"role"`
		Task         string `json:"task"`
		NeedTools    *bool  `json:"needTools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Bad request", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		jsonError(w, "Name required", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	_, exists := s.agents[req.Name]
	_, cfgExists := s.configs[req.Name]
	s.mu.RUnlock()
	if exists || cfgExists {
		jsonError(w, "Agent name already exists", http.StatusBadRequest)
		return
	}
	apiKey, modelID, pType, baseURL, ok := s.resolveAgentCredentials(req.Name, req.ModelID, req.Provider)
	if !ok {
		jsonError(w, "Configure provider API key first", http.StatusBadRequest)
		return
	}
	systemPrompt := req.SystemPrompt
	if systemPrompt == "" && req.Role != "" {
		systemPrompt = "You are " + req.Name + ", " + req.Role + ".\n\n"
		if req.Task != "" {
			systemPrompt += "Your main task: " + req.Task + "\n\n"
		}
		systemPrompt += "Answer based on your role and task."
	}
	if systemPrompt == "" {
		systemPrompt = "You are " + req.Name + ", a professional AI assistant."
	}
	needTools := true
	if req.NeedTools != nil {
		needTools = *req.NeedTools
	}
	cfg := config.AgentConfig{
		Name:           req.Name,
		ModelID:        modelID,
		APIKey:         apiKey,
		SystemPrompt:   systemPrompt,
		NeedTools:      needTools,
		ProviderName:   req.Provider,
		ProviderType:   pType,
		BaseURL:        baseURL,
	}
	s.mu.Lock()
	if !s.addAgentFromConfig(cfg) {
		s.mu.Unlock()
		jsonError(w, "Failed to create agent", http.StatusInternalServerError)
		return
	}
	s.mu.Unlock()
	s.saveAgentsToConfig()
	log.Printf("Agent %s created", req.Name)
	jsonOK(w, map[string]interface{}{
		"name":    req.Name,
		"message": "Agent created",
	})
}

func (s *Server) handleAgentDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Bad request", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		jsonError(w, "Name required", http.StatusBadRequest)
		return
	}
	// 主控智能体名称硬保护：无论如何都不能删除系统内置协调者。
	if req.Name == config.DefaultAgentName {
		jsonError(w, "主控智能体是系统内置协调者，不可删除", http.StatusForbidden)
		return
	}
	s.mu.RLock()
	locked := s.configs[req.Name].Locked
	s.mu.RUnlock()
	if locked {
		jsonError(w, "内置智能体不可删除", http.StatusForbidden)
		return
	}
	s.mu.Lock()
	delete(s.agents, req.Name)
	delete(s.configs, req.Name)
	s.manager.RemoveAgent(req.Name)
	s.mu.Unlock()
	s.saveAgentsToConfig()
	log.Printf("Agent %s deleted", req.Name)
	jsonOK(w, map[string]string{"message": "Agent deleted"})
}

func (s *Server) handleAgentUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		OldName      string `json:"oldName"`
		Name         string `json:"name"`
		ModelID      string `json:"modelID"`
		Provider     string `json:"provider"`
		SystemPrompt string `json:"systemPrompt"`
		Role         string `json:"role"`
		Task         string `json:"task"`
		NeedTools    *bool  `json:"needTools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Bad request", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		jsonError(w, "Name required", http.StatusBadRequest)
		return
	}
	oldName := req.OldName
	if oldName == "" {
		oldName = req.Name
	}
	s.mu.RLock()
	cfg, exists := s.configs[oldName]
	s.mu.RUnlock()
	if !exists {
		jsonError(w, "Agent not found", http.StatusNotFound)
		return
	}
	if oldName != req.Name && cfg.Locked {
		jsonError(w, "内置智能体不可改名", http.StatusForbidden)
		return
	}
	if oldName != req.Name {
		s.mu.RLock()
		_, duplicate := s.configs[req.Name]
		s.mu.RUnlock()
		if duplicate {
			jsonError(w, "Agent name already exists", http.StatusBadRequest)
			return
		}
	}
	targetModelID := cfg.ModelID
	if req.ModelID != "" {
		targetModelID = req.ModelID
	}
	apiKey, modelID, pType, baseURL, ok := s.resolveAgentCredentials(req.Name, targetModelID, req.Provider)
	if !ok {
		jsonError(w, "Configure provider API key first", http.StatusBadRequest)
		return
	}
	cfg.Name = req.Name
	cfg.APIKey = apiKey
	cfg.ModelID = modelID
	cfg.ProviderName = req.Provider
	cfg.ProviderType = pType
	cfg.BaseURL = baseURL
	if req.SystemPrompt != "" {
		cfg.SystemPrompt = req.SystemPrompt
	} else if req.Role != "" {
		cfg.SystemPrompt = "You are " + req.Name + ", " + req.Role + ".\n\n"
		if req.Task != "" {
			cfg.SystemPrompt += "Your main task: " + req.Task + "\n\n"
		}
		cfg.SystemPrompt += "Answer based on your role and task."
	}
	if req.NeedTools != nil {
		cfg.NeedTools = *req.NeedTools
	}
	s.mu.Lock()
	if oldName != req.Name {
		delete(s.agents, oldName)
		delete(s.configs, oldName)
		s.manager.RemoveAgent(oldName)
	}
	if !s.addAgentFromConfig(cfg) {
		s.mu.Unlock()
		jsonError(w, "Failed to update agent", http.StatusInternalServerError)
		return
	}
	s.mu.Unlock()
	s.saveAgentsToConfig()
	log.Printf("Agent %s updated to %s", oldName, req.Name)
	jsonOK(w, map[string]string{"message": "Agent updated"})
}

func (s *Server) saveAgentsToConfig() {
	data := make(map[string]interface{})
	s.mu.RLock()
	for name, cfg := range s.configs {
		data[name] = map[string]interface{}{
			"name":         name,
			"modelID":      cfg.ModelID,
			"systemPrompt": cfg.SystemPrompt,
			"needTools":    cfg.NeedTools,
			"provider":     cfg.ProviderName,
			"locked":       cfg.Locked,
		}
	}
	s.mu.RUnlock()
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal config: %v", err)
		return
	}
	configPath := s.findAgentsConfigPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		log.Printf("Failed to create config dir: %v", err)
		return
	}
	if err := os.WriteFile(configPath, jsonData, 0644); err != nil {
		log.Printf("Failed to save config: %v", err)
		return
	}
	log.Printf("Agent config saved to %s", configPath)
}

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	agentName := r.URL.Query().Get("agent")
	if agentName == "" {
		agents := s.skillMgr.ListAgents()
		jsonOK(w, map[string]interface{}{"agents": agents})
		return
	}
	skillList := s.skillMgr.GetAgentSkills(agentName)
	type skillInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	result := make([]skillInfo, 0, len(skillList))
	for _, skill := range skillList {
		result = append(result, skillInfo{Name: skill.Name, Description: skill.Description})
	}
	jsonOK(w, map[string]interface{}{"agent": agentName, "skills": result})
}

func (s *Server) handleSkillAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Agent   string `json:"agent"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Bad request", http.StatusBadRequest)
		return
	}
	if req.Agent == "" || req.Name == "" || req.Content == "" {
		jsonError(w, "agent, name, content required", http.StatusBadRequest)
		return
	}
	if err := s.skillMgr.AddSkill(req.Agent, req.Name, req.Content); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.skillMgr.Reload()
	s.mu.RLock()
	for _, a := range s.agents {
		if a.GetName() == req.Agent {
			a.SetSkillManager(s.skillMgr)
		}
	}
	s.mu.RUnlock()
	jsonOK(w, map[string]string{"message": "Skill added"})
}

func (s *Server) handleSkillDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Agent string `json:"agent"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Bad request", http.StatusBadRequest)
		return
	}
	if req.Agent == "" || req.Name == "" {
		jsonError(w, "agent, name required", http.StatusBadRequest)
		return
	}
	if err := s.skillMgr.DeleteSkill(req.Agent, req.Name); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.skillMgr.Reload()
	jsonOK(w, map[string]string{"message": "Skill deleted"})
}


func (s *Server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Bad request", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		jsonError(w, "sessionId required", http.StatusBadRequest)
		return
	}
	key := s.sessionKey(r, req.SessionID)
	_ = s.sessions.CreateSession(key)
	jsonOK(w, map[string]interface{}{
		"sessionId": req.SessionID,
		"message":   "Session created",
	})
}

func (s *Server) handleSessionMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID string `json:"sessionId"`
		Agent     string `json:"agent"`
		Role      string `json:"role"`
		Content   string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Bad request", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" || req.Content == "" {
		jsonError(w, "sessionId and content required", http.StatusBadRequest)
		return
	}
	key := s.sessionKey(r, req.SessionID)
	// 用与会话 chat 流相同的 per-session 锁，避免与 handleChat 的
	// "读历史→运行→写回"并发导致消息顺序 / agents 列表交错。
	lock := s.sessionLock(key)
	lock.Lock()
	defer lock.Unlock()
	session, ok := s.sessions.GetSession(key)
	if !ok {
		session = s.sessions.CreateSession(key)
	}
	var msg *schema.Message
	if req.Role == "assistant" {
		msg = &schema.Message{Role: schema.Assistant, Content: req.Content}
	} else {
		msg = schema.UserMessage(req.Content)
	}
	if err := s.sessions.AddMessage(key, msg); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if req.Agent != "" {
		seen := false
		for _, name := range session.Agents {
			if name == req.Agent {
				seen = true
				break
			}
		}
		if !seen && len(session.Agents) < 50 {
			session.Agents = append(session.Agents, req.Agent)
		}
	}
	jsonOK(w, map[string]string{"message": "Message added"})
}

func (s *Server) handleSessionHistory(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		jsonError(w, "sessionId required", http.StatusBadRequest)
		return
	}
	key := s.sessionKey(r, sessionID)
	msgs, err := s.sessions.GetStoredMessages(key)
	if err != nil {
		jsonError(w, err.Error(), http.StatusNotFound)
		return
	}
	type attJSON struct {
		Name string `json:"name"`
		Data string `json:"data"`
		Kind string `json:"kind"`
		Size int64  `json:"size"`
		Mime string `json:"mime"`
	}
	type msgJSON struct {
		Role       string    `json:"role"`
		Content    string    `json:"content"`
		Attachments []attJSON `json:"attachments,omitempty"`
	}
	result := make([]msgJSON, 0, len(msgs))
	for _, m := range msgs {
		mj := msgJSON{Role: m.Role, Content: m.Content}
		if len(m.Attachments) > 0 {
			atts := make([]attJSON, 0, len(m.Attachments))
			for _, a := range m.Attachments {
				atts = append(atts, attJSON{
					Name: a.Name, Data: a.Data, Kind: a.Kind, Size: a.Size, Mime: a.Mime,
				})
			}
			mj.Attachments = atts
		}
		result = append(result, mj)
	}
	jsonOK(w, map[string]interface{}{"sessionId": sessionID, "messages": result})
}

func (s *Server) handleSessionSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Bad request", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		jsonError(w, "sessionId required", http.StatusBadRequest)
		return
	}
	key := s.sessionKey(r, req.SessionID)
	if err := s.sessions.Save(key); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"message": "Session saved"})
}

func (s *Server) handleSessionList(w http.ResponseWriter, r *http.Request) {
	userID := ""
	if u, ok := auth.UserFromContext(r.Context()); ok && u != nil {
		userID = u.UserID
	}
	sessions := s.sessions.ListUserSessions(userID)
	jsonOK(w, map[string]interface{}{"sessions": sessions})
}

func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Bad request", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		jsonError(w, "sessionId required", http.StatusBadRequest)
		return
	}
	key := s.sessionKey(r, req.SessionID)
	if err := s.sessions.DeleteSession(key); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 同步回收该会话的会话锁，避免 chatLocks 仅增不减导致长期运行内存泄漏。
	s.chatLocks.Delete(key)
	jsonOK(w, map[string]string{"message": "Session deleted"})
}

var _ = schema.UserMessage

func (s *Server) handleScreenshotGet(w http.ResponseWriter, r *http.Request) {
	// 从 /api/screenshot/shot-1 中提取 key
	key := strings.TrimPrefix(r.URL.Path, "/api/screenshot/")
	if key == "" {
		jsonError(w, "screenshot key required", http.StatusBadRequest)
		return
	}
	entry, ok := agent.GetScreenshot(key)
	if !ok {
		jsonError(w, "screenshot not found or expired", http.StatusNotFound)
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(entry.Base64)
	if err != nil {
		jsonError(w, "invalid screenshot data", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(decoded)
}
