package agent

import (
	"context"

	"eino/config"

	arkmodel "github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

func createModel(cfg config.AgentConfig) (model.ChatModel, error) {
	ctx := context.Background()
	// 按商家类型选择 SDK。默认 "ark" 以保留向后兼容（老配置无 Type 字段）。
	pType := cfg.ProviderType
	if pType == "" {
		pType = "ark"
	}
	switch pType {
	case "ark":
		return arkmodel.NewChatModel(ctx, &arkmodel.ChatModelConfig{
			APIKey: cfg.APIKey,
			Model:  cfg.ModelID,
		})
	default:
		// openai 兼容协议：Ark、DeepSeek、OpenAI、Qwen、OpenRouter、Ollama 等
		// 绝大多数商家都提供 OpenAI 兼容入口，只需 BaseURL 区分。
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		return openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:  cfg.APIKey,
			Model:   cfg.ModelID,
			BaseURL: baseURL,
		})
	}
}
