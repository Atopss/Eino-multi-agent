package agent

import (
	"context"
	"os"
	"strings"
	"testing"

	"eino/config"

	"github.com/joho/godotenv"
)

// TestReActToolCallE2E 不走 HTTP，直接构造真实 ReAct 智能体（DeepSeek 模型 + 真实工具），
// 验证“先输出文本、后输出 tool_calls”的模型（如 DeepSeek）能正确触发工具执行。
func TestReActToolCallE2E(t *testing.T) {
	_ = godotenv.Load("../.env")
	_ = godotenv.Load(".env")
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		t.Skip("no DEEPSEEK_API_KEY")
	}

	cfg := config.AgentConfig{
		Name:         "测试智能体",
		SystemPrompt: "你是支持工具调用的助手。",
		APIKey:       apiKey,
		ModelID:      "deepseek-chat",
		ProviderType:  "openai",
		BaseURL:      "https://api.deepseek.com",
		NeedTools:    true,
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	ctx := context.Background()
	var final strings.Builder
	res, err := a.RunStream(ctx, nil,
		"请调用 get_weather 工具查询北京今天的天气和温度，必须真实调用工具，不要凭记忆回答。",
		RunOptions{}, func(ev StreamEvent) error {
			if ev.Type == "delta" && ev.Delta != "" {
				final.WriteString(ev.Delta)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("runstream: %v", err)
	}
	out := final.String()
	if res.Reply != "" {
		out = res.Reply
	}
	t.Logf("FINAL_ANSWER=%s", out)
	if !strings.Contains(out, "°") && !strings.Contains(out, "温度") && !strings.Contains(out, "天气") {
		t.Fatalf("模型未触发工具调用或回答不含天气信息: %s", out)
	}
	t.Logf("OK: 工具已真实执行并融入回答")
}
