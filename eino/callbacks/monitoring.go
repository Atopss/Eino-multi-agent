package callbacks

import (
	"context"
	"log"
	"time"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/schema"
)

// ============================================================
// callbacks 是什么？
// ============================================================
//
// callbacks 是 Eino 的监控机制，可在 Agent / 组件执行的各个阶段
// 插入自定义逻辑，例如：记录日志（每次调用花了多久）、统计指标、
// 追踪调用链。本文件的 MonitoringHandler 实现了 eino 的
// callbacks.Handler 接口，并在 agent.Agent.Run 中通过
// callbacks.InitCallbacks 注入到 ctx，随组件执行自动触发。

// MonitoringHandler 实现 eino callbacks.Handler 接口。
type MonitoringHandler struct {
	name string
}

// NewMonitoringHandler 创建一个新的监控处理器。
func NewMonitoringHandler(name string) *MonitoringHandler {
	return &MonitoringHandler{name: name}
}

// OnStart 在开始执行某个组件时调用。
func (h *MonitoringHandler) OnStart(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
	log.Printf("[%s] 开始执行: %s (type=%s)", h.name, info.Name, info.Type)
	return context.WithValue(ctx, startTimeKey, time.Now())
}

// OnEnd 在执行结束时调用，这里计算并输出耗时。
func (h *MonitoringHandler) OnEnd(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
	start, _ := ctx.Value(startTimeKey).(time.Time)
	log.Printf("[%s] 执行完成: %s (耗时: %v)", h.name, info.Name, time.Since(start))
	return ctx
}

// OnError 在发生错误时调用。
func (h *MonitoringHandler) OnError(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
	log.Printf("[%s] 执行出错: %s (错误: %v)", h.name, info.Name, err)
	return ctx
}

// OnStartWithStreamInput 流式输入的组件开始回调（此处无需处理）。
func (h *MonitoringHandler) OnStartWithStreamInput(ctx context.Context, info *callbacks.RunInfo, input *schema.StreamReader[callbacks.CallbackInput]) context.Context {
	return ctx
}

// OnEndWithStreamOutput 流式输出的组件结束回调（此处无需处理）。
func (h *MonitoringHandler) OnEndWithStreamOutput(ctx context.Context, info *callbacks.RunInfo, output *schema.StreamReader[callbacks.CallbackOutput]) context.Context {
	return ctx
}

type ctxKey string

const startTimeKey ctxKey = "eino_start_time"

// ChatLogger 用于上层对话日志（可选）。
type ChatLogger struct{}

func (l *ChatLogger) LogMessage(role, content string) {
	log.Printf("[Chat] %s: %s", role, content)
}

func (l *ChatLogger) LogToolCall(toolName, input, output string) {
	log.Printf("[Tool] 调用工具: %s", toolName)
	log.Printf("[Tool] 输入: %s", input)
	log.Printf("[Tool] 输出: %s", output)
}
