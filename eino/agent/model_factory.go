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
	if len(cfg.APIKey) > 3 && cfg.APIKey[:3] == "sk-" {
		return openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:  cfg.APIKey,
			Model:   cfg.ModelID,
			BaseURL: "https://api.deepseek.com",
		})
	}
	return arkmodel.NewChatModel(ctx, &arkmodel.ChatModelConfig{
		APIKey: cfg.APIKey,
		Model:  cfg.ModelID,
	})
}
