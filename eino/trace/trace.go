// Package trace 承载 agent 执行过程的「可观测轨迹」与「流式事件」领域类型，
// 以及基于 context 的运行时轨迹发射器。
//
// 它是一个不反向依赖 agent 的低层包，作为清晰的依赖底边：
// agent 包与未来的 agent/tools/* 工具子包都只依赖它，从而让工具实现可以
// 独立成包而不产生对 agent 的循环依赖。
package trace

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
)

// ---------------------------------------------------------------------------
// 领域类型（原定义于 agent 包，下沉至此统一维护；agent 以类型别名对外保持不变）
// ---------------------------------------------------------------------------

// RAGReference 单条检索命中的引用信息。
type RAGReference struct {
	ID         string  `json:"id"`
	FileName   string  `json:"fileName"`
	Source     string  `json:"source"`
	ChunkIndex int     `json:"chunkIndex"`
	Score      float64 `json:"score"`
	Chunk      string  `json:"chunk"`
	MatchType  string  `json:"matchType"`
}

// ToolCallTrace 一次工具调用的精简记录。
type ToolCallTrace struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Result    string `json:"result"`
}

// ExecutionTraceItem 执行轨迹中的一个节点（检索 / 工具调用 / 编排阶段等）。
type ExecutionTraceItem struct {
	Type      string `json:"type"`
	Stage     string `json:"stage,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Target    string `json:"target,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result,omitempty"`
	Message   string `json:"message,omitempty"`

	// 多智能体编排上下文
	Phase   string `json:"phase,omitempty"`   // plan / dispatch / agent / synthesize
	SubTask string `json:"subTask,omitempty"` // 子任务描述
}

// AgentStep 是「执行过程时间线」的一个步骤节点，通过 SSE 实时流式推送给前端，
// 让原本隐藏在最终 done 事件里的检索/工具调用过程即时可见。
// 同一动作会发两条：running（开始）→ done（完成），前端按 kind+name 合并。
type AgentStep struct {
	Kind   string `json:"kind"`             // rag | tool | model | agent
	Name   string `json:"name"`             // 同 kind 下关联 running/done 的标识（如工具名）
	Title  string `json:"title"`            // 展示标题
	Status string `json:"status"`           // running | done | error
	Input  string `json:"input,omitempty"`  // 调用入参（可选，细节折叠）
	Output string `json:"output,omitempty"` // 返回出参（可选，细节折叠）
}

// SubTaskInfo 规划阶段产出的子任务（供前端时间线展示）。
type SubTaskInfo struct {
	Agent string `json:"agent"`
	Task  string `json:"task"`
}

// StreamEvent 是流式对话（SSE）向前端推送的单个事件。
type StreamEvent struct {
	Type          string               `json:"type"`
	Stage         string               `json:"stage,omitempty"`
	Message       string               `json:"message,omitempty"`
	Delta         string               `json:"delta,omitempty"`
	Reply         string               `json:"reply,omitempty"`
	RAGQuery      string               `json:"ragQuery,omitempty"`
	RAGReferences []RAGReference       `json:"ragReferences,omitempty"`
	ToolCalls     []ToolCallTrace      `json:"toolCalls,omitempty"`
	TraceItems    []ExecutionTraceItem `json:"traceItems,omitempty"`
	AnswerMode    string               `json:"answerMode,omitempty"`
	Error         string               `json:"error,omitempty"`
	AgentStep     *AgentStep           `json:"agentStep,omitempty"`

	// 以下字段用于多智能体编排（Router / Supervisor）的实时时间线。
	Topology string        `json:"topology,omitempty"` // router / supervisor
	Phase    string        `json:"phase,omitempty"`    // plan / dispatch / agent / synthesize / done
	SubTask  string        `json:"subTask,omitempty"`  // 子任务描述
	Agent    string        `json:"agent,omitempty"`    // 负责该子任务的智能体名
	Step     int           `json:"step,omitempty"`     // supervisor 循环步数
	SubTasks []SubTaskInfo `json:"subTasks,omitempty"` // 规划阶段产出的子任务清单

	// Plan 模式：先输出执行计划
	Plan         string   `json:"plan,omitempty"`         // 计划文本（Markdown）
	PlannedSteps []string `json:"plannedSteps,omitempty"` // 从计划中解析出的步骤列表
	PlanStatus   string   `json:"planStatus,omitempty"`   // "generating" | "done"
}

// ---------------------------------------------------------------------------
// 运行时轨迹发射器（基于 context 传递）
// ---------------------------------------------------------------------------

type traceContextKey struct{}

// stepEmitterKey 用于在 ctx 中携带「实时步骤事件发射器」。
// 仅 RunStream 会注入；Run（非流式）不注入，AppendTraceItem 检测到缺失即跳过，无副作用。
type stepEmitterKey struct{}

// RuntimeTrace 收集单次运行期间累积的执行轨迹（并发安全）。
type RuntimeTrace struct {
	mu    sync.Mutex
	items []ExecutionTraceItem
}

// NewRuntimeTrace 创建一个空的运行时轨迹收集器。
func NewRuntimeTrace() *RuntimeTrace {
	return &RuntimeTrace{items: make([]ExecutionTraceItem, 0, 16)}
}

// WithStepEmitter 把流式步骤事件发射器挂到 ctx 上。
func WithStepEmitter(ctx context.Context, emit func(StreamEvent) error) context.Context {
	return context.WithValue(ctx, stepEmitterKey{}, emit)
}

// StepEmitterFromCtx 取出发射器（不存在时 ok=false）。
func StepEmitterFromCtx(ctx context.Context) (func(StreamEvent) error, bool) {
	e, ok := ctx.Value(stepEmitterKey{}).(func(StreamEvent) error)
	return e, ok
}

var (
	traceRegistryMu sync.Mutex
	traceRegistry   = make(map[string]*RuntimeTrace)
	traceSeq        uint64
)

// NewRuntimeTraceID 生成一个进程内唯一的轨迹 ID。
func NewRuntimeTraceID() string {
	return "trace-" + strconv.Itoa(int(atomic.AddUint64(&traceSeq, 1)))
}

// WithRuntimeTrace 把轨迹收集器登记到全局注册表并将 traceID 挂到 ctx。
func WithRuntimeTrace(ctx context.Context, traceID string, tr *RuntimeTrace) context.Context {
	traceRegistryMu.Lock()
	traceRegistry[traceID] = tr
	traceRegistryMu.Unlock()
	return context.WithValue(ctx, traceContextKey{}, traceID)
}

// ReleaseRuntimeTrace 从注册表移除轨迹（应在运行结束时 defer 调用）。
func ReleaseRuntimeTrace(traceID string) {
	traceRegistryMu.Lock()
	delete(traceRegistry, traceID)
	traceRegistryMu.Unlock()
}

// AppendTraceItem 追加一条执行轨迹项；若 ctx 上挂了实时发射器，则同步流出为 step 事件。
func AppendTraceItem(ctx context.Context, item ExecutionTraceItem) {
	traceID, ok := ctx.Value(traceContextKey{}).(string)
	if !ok || traceID == "" {
		return
	}
	traceRegistryMu.Lock()
	tr := traceRegistry[traceID]
	traceRegistryMu.Unlock()
	if tr == nil {
		return
	}
	tr.mu.Lock()
	tr.items = append(tr.items, item)
	tr.mu.Unlock()

	// 实时把该步转成 step SSE 事件流出（前端时间线即时展示）。
	// 仅对工具调用/检索类目发射；agent_start/done 等不发射，避免噪声。
	if em, ok := StepEmitterFromCtx(ctx); ok {
		if s := stepFromTraceItem(item); s != nil {
			_ = em(StreamEvent{Type: "step", AgentStep: s})
		}
	}
}

// stepFromTraceItem 把执行轨迹项映射为实时步骤节点。
// 仅映射工具调用类（tool_call/tool_result）：检索(RAG)的实时步骤由 RunStream 在检索完成后单独发射，
// 避免与最终 ensureRAGTraceItems 追加的 rag_search/rag_result 重复流出。
func stepFromTraceItem(item ExecutionTraceItem) *AgentStep {
	switch item.Type {
	case "tool_call":
		return &AgentStep{
			Kind:   "tool",
			Name:   item.Name,
			Title:  item.Message,
			Status: "running",
			Input:  item.Arguments,
		}
	case "tool_result":
		return &AgentStep{
			Kind:   "tool",
			Name:   item.Name,
			Title:  "", // 合并时回落到调用步的标题
			Status: "done",
			Output: item.Result,
		}
	}
	return nil
}

// TraceItems 返回轨迹项的快照副本（并发安全）。
func TraceItems(tr *RuntimeTrace) []ExecutionTraceItem {
	if tr == nil {
		return nil
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	out := make([]ExecutionTraceItem, len(tr.items))
	copy(out, tr.items)
	return out
}
