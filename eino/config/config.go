package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ProviderConfig struct {
	Name       string `json:"name"`
	APIKey     string `json:"apiKey,omitempty"`
	ChatModel  string `json:"chatModel"`
	EndpointID string `json:"endpointId"`
	// Type 决定使用哪个 SDK 构建模型："ark" 走火山方舟 SDK；"openai" 走 OpenAI 兼容 SDK。
	// 默认 "ark"（保留向后兼容）。
	Type string `json:"type"`
	// BaseURL 为 OpenAI 兼容地址；ark 类型可留空（由 ark SDK 决定接入点）。
	BaseURL string `json:"baseURL"`
	// Models 是该商家可选模型名清单，仅用于前端"模型名"下拉候选；
	// 不影响模型构建逻辑（仍由 ChatModel/EndpointID 决定实际模型）。空表示前端回退为手填。
	Models []string `json:"models,omitempty"`
}

type RuntimeConfig struct {
	Providers               []ProviderConfig `json:"providers"`
	EmbeddingModel          string           `json:"embeddingModel"`
	EmbeddingEP             string           `json:"embeddingEP"`
	RAGDataDir              string           `json:"ragDataDir"`
	RAGMaxChunks            int              `json:"ragMaxChunks"`
	RAGMaxChunksPerDocument int              `json:"ragMaxChunksPerDocument"`
	RAGChunkSize            int              `json:"ragChunkSize"`
	RAGChunkOverlap         int              `json:"ragChunkOverlap"`
	RAGMaxContextChars      int              `json:"ragMaxContextChars"`
	RAGMaxDocumentChars     int              `json:"ragMaxDocumentChars"`
	RAGMaxFileMB            int              `json:"ragMaxFileMB"`
	RAGMaxUploadMB          int              `json:"ragMaxUploadMB"`
	RAGChatTopK             int              `json:"ragChatTopK"`
	RAGMaxPerSource         int              `json:"ragMaxPerSource"`
	RAGMinScore             float64          `json:"ragMinScore"`
	RAGSourceFilter         string           `json:"ragSourceFilter"`
	RAGStrictContextOnly    bool             `json:"ragStrictContextOnly"`
	RAGNeighborChunks       int              `json:"ragNeighborChunks"`
	MemoryLimitMB           int              `json:"memoryLimitMB"`
	GCPercent               int              `json:"gcPercent"`
	ComputerToolsEnabled    bool             `json:"computerToolsEnabled"`
	ComputerAllowedRoots    []string         `json:"computerAllowedRoots"`
	ComputerAllowCommands   bool             `json:"computerAllowCommands"`
	ComputerAllowedCommands []string         `json:"computerAllowedCommands"`
	ComputerCommandTimeout  int              `json:"computerCommandTimeoutSec"`
	ComputerRequireApproval bool             `json:"computerRequireApproval"`
	ComputerDesktopEnabled  bool             `json:"computerDesktopEnabled"`
	ComputerDaemonPort      int              `json:"computerDaemonPort"`
	ConfigPath              string           `json:"-"`

	// 生产级加固配置（不持久化到 config.json，仅来自环境变量）
	JWTSecret          string `json:"-"`
	StreamTimeoutSec   int    `json:"-"`
	MaxSessionHistory  int    `json:"-"` // 喂给模型的会话历史条数上限（默认 60 ≈ 30 轮），防上下文无限膨胀
	RateLimitRPS       int    `json:"-"`
	RateLimitBurst     int    `json:"-"`
	// 鉴权模式："" 或 "local" = 本机自用（注入固定匿名用户，免登录）；"jwt" = 校验 Bearer Token 的真实多用户模式。
	AuthMode      string `json:"-"`
	TokenTTLHours int    `json:"-"` // Token 有效期（小时），仅 jwt 模式生效，默认 24
	// 每日配额（仅 jwt 模式下对普通用户生效；local 与管理员豁免）。
	// 与全局 RPS 限流是两回事：配额限制“单用户一天的总用量”，限流防突发洪峰。
	QuotaDailyRequests int `json:"-"` // 单用户每日最大请求数，默认 500
	QuotaDailyTokens   int `json:"-"` // 单用户每日最大 Token 估算数（输入+输出字节/4），默认 200000
	SQLitePath         string `json:"sqlitePath,omitempty"`

	// 传输安全（TLS）：仅当 TLSCertFile 与 TLSKeyFile 同时配置时才启用 HTTPS，
	// 否则退化为明文 HTTP（适用于本机自用或前面有反向代理终止 TLS 的场景）。
	// 证书/私钥路径来自环境变量，不持久化到 config.json。
	TLSCertFile string `json:"-"`
	TLSKeyFile  string `json:"-"`
}

// AgentConfig 定义单个 Agent 的完整配置
// 每个 Agent 可以有自己的 API Key 和模型，互不影响
type AgentConfig struct {
	Name         string // Agent 的名字，用于显示和切换
	SystemPrompt string // 系统提示词，定义 Agent 的角色和行为
	APIKey       string // 这个 Agent 专用的 API Key
	ModelID      string // 这个 Agent 专用的模型 ID
	NeedTools    bool   // 是否需要工具（true = 有工具能力，false = 纯聊天）
	// 以下字段把 Agent 关联到某个"模型提供商"，构建模型时据此选择 SDK 与接入地址。
	// 不持久化 Key（运行时由 Provider 解析），但记录商家名/类型/BaseURL 以便重建时重新解析。
	ProviderName string `json:"provider,omitempty"`
	ProviderType string `json:"providerType,omitempty"` // "ark" | "openai"
	BaseURL      string `json:"baseURL,omitempty"`     // 对应商家 BaseURL
	Locked       bool   `json:"locked,omitempty"`     // 内置智能体：不可删除、不可改名
}

// DefaultAgentName 是系统内置主控智能体的固定名称。
// 它在启动时由 Server.ensureDefaultAgent 自动注入，始终存在、不可被用户删除或改名，
// 并在多智能体 supervisor 编排中优先担任协调者，从而“控制”其它智能体。
const DefaultAgentName = "主控智能体"

// DefaultAgentSystemPrompt 内置主控智能体的系统提示词：定位为统筹/调度其它智能体的协调者。
func DefaultAgentSystemPrompt() string {
	return "你是本地智能体系统的内置主控（Coordinator）。" +
		"你的职责是统筹、调度并监督系统中的其它智能体：当任务复杂时，将其拆解为子任务，" +
		"委派给最合适的子智能体，汇总它们的结果后给出面向用户的最终答复；必要时你也可以亲自调用工具完成任务。" +
		"你以用户利益为先，回答须清晰、准确、可追溯来源。"
}

// Agent 配置现在统一来自 data/config.json 或环境变量（见 loadFromEnv / loadAgentsFromConfigFile），
// 不再内置写死的 Agent 列表。

func LoadRuntimeConfig(baseDir string) (RuntimeConfig, error) {
	cfg := RuntimeConfig{
		RAGDataDir:              filepath.Join(".", "data"),
		RAGMaxChunks:            2000,
		RAGMaxChunksPerDocument: 400,
		RAGChunkSize:            500,
		RAGChunkOverlap:         50,
		RAGMaxContextChars:      12000,
		RAGMaxDocumentChars:     200000,
		RAGMaxFileMB:            20,
		RAGMaxUploadMB:          10,
		RAGChatTopK:             5,
		RAGMaxPerSource:         2,
		RAGMinScore:             0,
		RAGStrictContextOnly:    false,
		RAGNeighborChunks:       2,
		GCPercent:               80,
		ComputerCommandTimeout:  15,
		ComputerRequireApproval: true,
		ComputerDaemonPort:      9876,
		StreamTimeoutSec:        240,
		MaxSessionHistory:       60,
		RateLimitRPS:           20,
		RateLimitBurst:          40,
		AuthMode:                "local",
		TokenTTLHours:           24,
		QuotaDailyRequests:      500,
		QuotaDailyTokens:        200000,
		SQLitePath:              filepath.Join(".", "data", "eino.db"),
	}

	configPath := findConfigPath(baseDir)
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return cfg, err
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, err
		}
		cfg.ConfigPath = configPath
	}

	applyEnvFallbacks(&cfg)
	return cfg, nil
}

func SaveRuntimeConfig(cfg RuntimeConfig) error {
	configPath := cfg.ConfigPath
	if configPath == "" {
		configPath = findConfigPath(".")
	}
	if configPath == "" {
		configPath = filepath.Join("..", "data", "config.json")
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	for i := range cfg.Providers {
		cfg.Providers[i].APIKey = ""
	}
	cfg.ConfigPath = ""
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

func findConfigPath(baseDir string) string {
	type candidate struct {
		path  string
		score int
	}
	var best candidate
	// 先检查 ../data/（根目录），再检查 ./data/。平局时根目录优先。
	for _, base := range []string{filepath.Join(baseDir, ".."), baseDir} {
		p := filepath.Join(base, "data", "config.json")
		if _, err := os.Stat(p); err != nil {
			continue
		}
		dir := filepath.Dir(p)
		score := 0
		for _, f := range []string{"agents.json", "eino.db", "sessions"} {
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
	return ""
}

func applyEnvFallbacks(cfg *RuntimeConfig) {
	arkAPIKey := os.Getenv("ARK_API_KEY")
	arkEndpoint := os.Getenv("ARK_ENDPOINT")
	if len(cfg.Providers) == 0 && (arkAPIKey != "" || arkEndpoint != "") {
		cfg.Providers = append(cfg.Providers, ProviderConfig{
			Name:       "Ark",
			APIKey:     arkAPIKey,
			EndpointID: arkEndpoint,
			ChatModel:  arkEndpoint,
		})
	} else if len(cfg.Providers) > 0 {
		if arkAPIKey != "" {
			cfg.Providers[0].APIKey = arkAPIKey
		}
		if cfg.Providers[0].EndpointID == "" {
			cfg.Providers[0].EndpointID = arkEndpoint
		}
		if cfg.Providers[0].ChatModel == "" {
			cfg.Providers[0].ChatModel = cfg.Providers[0].EndpointID
		}
	}

	// 当 .env 中配置了 DeepSeek Key 时，将其作为首选提供方。
	// 这样在 Ark 默认模型被限流（Safe Experience Mode）时，系统仍可正常对话与调用工具。
	// 如需切回 Ark，只需移除 DEEPSEEK_API_KEY 或在火山引擎控制台解除 doubao 限流即可。
	if dsKey := os.Getenv("DEEPSEEK_API_KEY"); dsKey != "" {
		cfg.Providers = append([]ProviderConfig{{
			Name:      "DeepSeek",
			APIKey:    dsKey,
			ChatModel: "deepseek-chat",
		}}, cfg.Providers...)
	}

	if cfg.EmbeddingEP == "" {
		cfg.EmbeddingEP = os.Getenv("EMBEDDING_EP")
	}
	if cfg.RAGDataDir == "" {
		cfg.RAGDataDir = os.Getenv("RAG_DATA_DIR")
	}
	if cfg.RAGDataDir == "" {
		cfg.RAGDataDir = filepath.Join(".", "data")
	}

	applyIntEnv(&cfg.RAGMaxChunks, "RAG_MAX_CHUNKS")
	applyIntEnv(&cfg.RAGMaxChunksPerDocument, "RAG_MAX_CHUNKS_PER_DOCUMENT")
	applyIntEnv(&cfg.RAGChunkSize, "RAG_CHUNK_SIZE")
	applyIntEnv(&cfg.RAGChunkOverlap, "RAG_CHUNK_OVERLAP")
	applyIntEnv(&cfg.RAGMaxContextChars, "RAG_MAX_CONTEXT_CHARS")
	applyIntEnv(&cfg.RAGMaxDocumentChars, "RAG_MAX_DOCUMENT_CHARS")
	applyIntEnv(&cfg.RAGMaxFileMB, "RAG_MAX_FILE_MB")
	applyIntEnv(&cfg.RAGMaxUploadMB, "RAG_MAX_UPLOAD_MB")
	applyIntEnv(&cfg.RAGChatTopK, "RAG_CHAT_TOP_K")
	applyIntEnv(&cfg.RAGMaxPerSource, "RAG_MAX_PER_SOURCE")
	applyIntEnv(&cfg.RAGNeighborChunks, "RAG_NEIGHBOR_CHUNKS")
	applyIntEnv(&cfg.MemoryLimitMB, "MEMORY_LIMIT_MB")
	applyIntEnv(&cfg.GCPercent, "GC_PERCENT")
	applyBoolEnv(&cfg.ComputerToolsEnabled, "COMPUTER_TOOLS_ENABLED")
	applyBoolEnv(&cfg.ComputerAllowCommands, "COMPUTER_ALLOW_COMMANDS")
	applyBoolEnv(&cfg.ComputerRequireApproval, "COMPUTER_REQUIRE_APPROVAL")
	applyBoolEnv(&cfg.ComputerDesktopEnabled, "COMPUTER_DESKTOP_ENABLED")
	applyIntEnv(&cfg.ComputerDaemonPort, "COMPUTER_DAEMON_PORT")
	applyIntEnv(&cfg.ComputerCommandTimeout, "COMPUTER_COMMAND_TIMEOUT_SEC")
	if cfg.RAGSourceFilter == "" {
		cfg.RAGSourceFilter = os.Getenv("RAG_SOURCE_FILTER")
	}

	if cfg.JWTSecret == "" {
		cfg.JWTSecret = os.Getenv("JWT_SECRET")
	}
	if cfg.SQLitePath == "" {
		cfg.SQLitePath = os.Getenv("SQLITE_PATH")
	}
	// 传输安全：仅当证书与私钥同时配置时才启用 HTTPS（ListenAndServeTLS），
	// 否则监听明文 HTTP。证书/私钥路径不写入 config.json，仅来自环境变量。
	if cfg.TLSCertFile == "" {
		cfg.TLSCertFile = os.Getenv("TLS_CERT")
	}
	if cfg.TLSKeyFile == "" {
		cfg.TLSKeyFile = os.Getenv("TLS_KEY")
	}
	applyIntEnv(&cfg.StreamTimeoutSec, "STREAM_TIMEOUT_SEC")
	applyIntEnv(&cfg.MaxSessionHistory, "MAX_SESSION_HISTORY")
	applyIntEnv(&cfg.RateLimitRPS, "RATE_LIMIT_RPS")
	applyIntEnv(&cfg.RateLimitBurst, "RATE_LIMIT_BURST")
	// 鉴权模式：默认 local（免登录）。要启用真实多用户，需显式设 AUTH_MODE=jwt 并配置 JWT_SECRET。
	if cfg.AuthMode == "" {
		cfg.AuthMode = os.Getenv("AUTH_MODE")
	}
	if cfg.AuthMode != "jwt" {
		cfg.AuthMode = "local"
	}
	applyIntEnv(&cfg.TokenTTLHours, "TOKEN_TTL_HOURS")
	// 每日配额：单用户每天的请求数 / Token 估算数上限（仅 jwt 模式普通用户生效）。
	applyIntEnv(&cfg.QuotaDailyRequests, "QUOTA_DAILY_REQUESTS")
	applyIntEnv(&cfg.QuotaDailyTokens, "QUOTA_DAILY_TOKENS")
	// 通用：为 config 中已声明但 Key 为空的商家，从 <NAME>_API_KEY 环境变量回填。
	// 命名约定：商家名转大写、空格转下划线后加 _API_KEY（如 "Ark" -> "ARK_API_KEY"，"OpenAI" -> "OPENAI_API_KEY"）。
	for i := range cfg.Providers {
		if cfg.Providers[i].Type == "" {
			cfg.Providers[i].Type = "ark"
		}
		if cfg.Providers[i].APIKey == "" {
			if k := os.Getenv(EnvKeyForProvider(cfg.Providers[i].Name)); k != "" {
				cfg.Providers[i].APIKey = k
			}
		}
	}
	if len(cfg.ComputerAllowedRoots) == 0 {
		if roots := os.Getenv("COMPUTER_ALLOWED_ROOTS"); roots != "" {
			cfg.ComputerAllowedRoots = splitEnvList(roots)
		}
	}
	if len(cfg.ComputerAllowedCommands) == 0 {
		if commands := os.Getenv("COMPUTER_ALLOWED_COMMANDS"); commands != "" {
			cfg.ComputerAllowedCommands = splitEnvList(commands)
		}
	}
}

func applyIntEnv(target *int, key string) {
	value := os.Getenv(key)
	if value == "" {
		return
	}
	if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
		*target = parsed
	}
}

func applyBoolEnv(target *bool, key string) {
	value := os.Getenv(key)
	if value == "" {
		return
	}
	parsed, err := strconv.ParseBool(value)
	if err == nil {
		*target = parsed
	}
}

func splitEnvList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ';' || r == ','
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// EnvKeyForProvider 把商家名映射为 Env 变量名：大写 + 空格转下划线 + 后缀 _API_KEY。
// 例如 "Ark" -> "ARK_API_KEY"，"OpenAI" -> "OPENAI_API_KEY"。
// 自动剔除中文、括号等非法字符，防止 godotenv 解析失败导致整个 .env 被忽略。
func EnvKeyForProvider(name string) string {
	clean := strings.TrimSpace(name)
	// 去掉括号（半角/全角）
	clean = strings.NewReplacer("(", "", ")", "", "（", "", "）", "").Replace(clean)
	// 只保留 ASCII 字母、数字、空格和下划线
	var buf strings.Builder
	for _, r := range clean {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' || r == '_' {
			buf.WriteRune(r)
		}
	}
	upper := strings.ToUpper(strings.ReplaceAll(buf.String(), " ", "_"))
	// 合并连续下划线并去掉首尾下划线
	for strings.Contains(upper, "__") {
		upper = strings.ReplaceAll(upper, "__", "_")
	}
	upper = strings.Trim(upper, "_")
	if upper == "" {
		return "PROVIDER_API_KEY"
	}
	return upper + "_API_KEY"
}

// ProviderPresets 返回内置的常用商家预设（BaseURL + 默认模型名 + 可选模型清单）。
// 用户在前端选择商家后，模型名从 Models 下拉点选、只需填 API Key 即可，无需关心接入地址。
// 仅保留国际与国内主流品牌；接入更多 OpenAI 兼容商家，在此追加即可（或前端用"自定义"入口）。
func ProviderPresets() []ProviderConfig {
	return []ProviderConfig{
		// ================= 国际 =================
		{Name: "OpenAI", Type: "openai", BaseURL: "https://api.openai.com/v1", ChatModel: "gpt-5.6-sol",
			Models: []string{
				"gpt-5.6-sol",
				"gpt-5.6-terra",
				"gpt-5.6-luna",
				"gpt-5.5",
				"gpt-5.4",
				"gpt-5.4-mini",
				"gpt-5.2",
			}},
		{Name: "Anthropic (Claude)", Type: "openai", BaseURL: "https://api.anthropic.com/v1", ChatModel: "claude-sonnet-4-6",
			Models: []string{
				"claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6",
				"claude-sonnet-4-6", "claude-sonnet-4-5-20250929",
				"claude-haiku-4-5-20251001",
			}},
		{Name: "xAI (Grok)", Type: "openai", BaseURL: "https://api.x.ai/v1", ChatModel: "grok-4-0709",
			Models: []string{
				"grok-4-0709", "grok-code-fast-1",
				"grok-3", "grok-3-mini",
			}},
		// ================= 国内 =================
		{Name: "DeepSeek", Type: "openai", BaseURL: "https://api.deepseek.com", ChatModel: "deepseek-v4-flash",
			Models: []string{"deepseek-v4-flash", "deepseek-v4-pro"}},
		{Name: "智谱AI (GLM)", Type: "openai", BaseURL: "https://open.bigmodel.cn/api/paas/v4", ChatModel: "glm-5",
			Models: []string{
				"glm-5",
				"glm-4-plus", "glm-4-air-250414", "glm-4-flashx-250414", "glm-4-flash-250414",
			}},
		{Name: "月之暗面 (Kimi)", Type: "openai", BaseURL: "https://api.moonshot.cn/v1", ChatModel: "kimi-k3",
			Models: []string{
				"kimi-k3",
				"kimi-k2.7-code", "kimi-k2.7-code-highspeed",
				"kimi-k2.6", "kimi-k2.5",
			}},
		{Name: "阿里云百炼 (Qwen)", Type: "openai", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", ChatModel: "qwen3.7-max",
			Models: []string{
				"qwen3.7-max", "qwen3.7-plus", "qwen3.6-flash",
				"qwen-max", "qwen-plus", "qwen-turbo",
			}},
		{Name: "Ark (火山方舟)", Type: "ark", ChatModel: "doubao-seed-2.1-pro",
			Models: []string{
				"doubao-seed-2.1-pro", "doubao-seed-2.1-turbo",
				"doubao-pro-32k", "doubao-pro-256k", "doubao-lite-32k",
			}},
		// ================= 免费推理平台 =================
		{Name: "OpenCode Zen (免费)", Type: "openai", BaseURL: "https://opencode.ai/zen/v1", ChatModel: "big-pickle",
			Models: []string{
				"big-pickle",
				"deepseek-v4-flash-free",
				"mimo-v2.5-free",
				"north-mini-code-free",
				"nemotron-3-ultra-free",
			}},
		{Name: "OpenRouter 免费层", Type: "openai", BaseURL: "https://openrouter.ai/api/v1", ChatModel: "deepseek/deepseek-v4-flash:free",
			Models: []string{
				"deepseek/deepseek-v4-flash:free",
				"nvidia/nemotron-3-ultra-55b:free",
				"google/gemini-2.5-flash:free",
				"qwen/qwen3.7-flash:free",
				"meta-llama/llama-4-scout:free",
			}},
	}
}

func (c RuntimeConfig) PrimaryProvider() ProviderConfig {
	for _, provider := range c.Providers {
		if provider.APIKey != "" && (provider.EndpointID != "" || provider.ChatModel != "") {
			return provider
		}
	}
	if len(c.Providers) > 0 {
		return c.Providers[0]
	}
	return ProviderConfig{}
}
