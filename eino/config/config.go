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
	JWTSecret      string `json:"-"`
	StreamTimeoutSec int    `json:"-"`
	RateLimitRPS   int    `json:"-"`
	RateLimitBurst int    `json:"-"`
	SQLitePath     string `json:"sqlitePath,omitempty"`
}

// AgentConfig 定义单个 Agent 的完整配置
// 每个 Agent 可以有自己的 API Key 和模型，互不影响
type AgentConfig struct {
	Name         string // Agent 的名字，用于显示和切换
	SystemPrompt string // 系统提示词，定义 Agent 的角色和行为
	APIKey       string // 这个 Agent 专用的 API Key
	ModelID      string // 这个 Agent 专用的模型 ID
	NeedTools     bool   // 是否需要工具（true = 有工具能力，false = 纯聊天）
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
		RateLimitRPS:           20,
		RateLimitBurst:          40,
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
	candidates := []string{
		filepath.Join(baseDir, "data", "config.json"),
		filepath.Join(baseDir, "..", "data", "config.json"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			abs, absErr := filepath.Abs(p)
			if absErr == nil {
				return abs
			}
			return p
		}
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
	applyIntEnv(&cfg.StreamTimeoutSec, "STREAM_TIMEOUT_SEC")
	applyIntEnv(&cfg.RateLimitRPS, "RATE_LIMIT_RPS")
	applyIntEnv(&cfg.RateLimitBurst, "RATE_LIMIT_BURST")
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
