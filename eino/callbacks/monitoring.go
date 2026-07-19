package callbacks

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
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
//
// 可观测性三件套：
//   - 结构化日志：所有生命周期事件经 log/slog 输出（key=value），
//     与全局 slog 处理器保持一致，可被外部配置为 JSON / 文件 / 采样。
//   - traceID：每个 Run 入口生成一次，写入 ctx 与顶层 RunInfo.UID，
//     同一次用户请求的所有节点日志共享同一 traceID，便于串联调用链。
//   - 指标：处理器内维护一份内存指标快照（调用数 / 成功 / 失败 / 时延），
//     通过 Metrics() 读取，无需引入外部依赖即可支撑基础监控。

// traceIDKey / startTimeKey 为 ctx 内部键，类型不导出以保证键的唯一性。
type ctxKey string

const (
	traceIDKey    ctxKey = "eino_trace_id"
	startTimeKey  ctxKey = "eino_start_time"
)

// NewTraceID 生成一个新的分布式追踪 ID（UUID v4）。
func NewTraceID() string { return uuid.NewString() }

// WithTraceID 将 traceID 注入 ctx，供本次请求的所有回调读取。
func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceIDKey, id)
}

// TraceIDFromContext 从 ctx 取出 traceID；未设置时返回空串。
func TraceIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(traceIDKey).(string); ok {
		return id
	}
	return ""
}

// Metrics 是处理器维护的一份指标快照（拷贝，读取线程安全）。
type Metrics struct {
	Runs         int64 `json:"runs"`          // 已开始的节点调用数
	Completed    int64 `json:"completed"`     // 已结束（成功或失败）的调用数
	Success      int64 `json:"success"`       // 成功结束的调用数
	Errors       int64 `json:"errors"`        // 错误结束的调用数
	LatencySumMs int64 `json:"latencySumMs"` // 累计时延（毫秒）
	LatencyMaxMs int64 `json:"latencyMaxMs"` // 最大单跳时延（毫秒）
}

// AvgLatencyMs 返回平均时延（毫秒）；无样本时为 0。
func (m Metrics) AvgLatencyMs() int64 {
	if m.Completed == 0 {
		return 0
	}
	return m.LatencySumMs / m.Completed
}

// MonitoringHandler 实现 eino callbacks.Handler 接口。
type MonitoringHandler struct {
	name string

	mu      sync.Mutex
	metrics Metrics
}

// NewMonitoringHandler 创建一个新的监控处理器。
func NewMonitoringHandler(name string) *MonitoringHandler {
	return &MonitoringHandler{name: name}
}

// Metrics 返回当前指标快照（值拷贝）。
func (h *MonitoringHandler) Metrics() Metrics {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.metrics
}

func (h *MonitoringHandler) recordStart() {
	h.mu.Lock()
	h.metrics.Runs++
	h.mu.Unlock()
}

func (h *MonitoringHandler) recordEnd(d time.Duration, errored bool) {
	h.mu.Lock()
	m := &h.metrics
	m.Completed++
	if errored {
		m.Errors++
	} else {
		m.Success++
	}
	ms := d.Milliseconds()
	m.LatencySumMs += ms
	if ms > m.LatencyMaxMs {
		m.LatencyMaxMs = ms
	}
	h.mu.Unlock()
}

// OnStart 在开始执行某个组件时调用。
func (h *MonitoringHandler) OnStart(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
	h.recordStart()
	slog.Info("eino run start",
		"agent", h.name,
		"traceID", TraceIDFromContext(ctx),
		"node", info.Name,
		"type", info.Type,
	)
	return context.WithValue(ctx, startTimeKey, time.Now())
}

// OnEnd 在执行成功结束时调用，计算并输出耗时。
func (h *MonitoringHandler) OnEnd(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
	start, _ := ctx.Value(startTimeKey).(time.Time)
	elapsed := time.Since(start)
	h.recordEnd(elapsed, false)
	slog.Info("eino run end",
		"agent", h.name,
		"traceID", TraceIDFromContext(ctx),
		"node", info.Name,
		"latency_ms", elapsed.Milliseconds(),
	)
	return ctx
}

// OnError 在发生错误时调用。
func (h *MonitoringHandler) OnError(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
	start, _ := ctx.Value(startTimeKey).(time.Time)
	elapsed := time.Since(start)
	h.recordEnd(elapsed, true)
	slog.Error("eino run error",
		"agent", h.name,
		"traceID", TraceIDFromContext(ctx),
		"node", info.Name,
		"latency_ms", elapsed.Milliseconds(),
		"error", err,
	)
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

// ChatLogger 用于上层对话日志（可选）。
type ChatLogger struct{}

func (l *ChatLogger) LogMessage(role, content string) {
	slog.Info("chat message", "role", role, "content", content)
}

func (l *ChatLogger) LogToolCall(toolName, input, output string) {
	slog.Info("tool call", "tool", toolName, "input", input, "output", output)
}
