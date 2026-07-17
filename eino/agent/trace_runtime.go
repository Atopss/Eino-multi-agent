package agent

import (
	"context"
	"sync"
	"sync/atomic"
)

type traceContextKey struct{}

// stepEmitterKey 用于在 ctx 中携带「实时步骤事件发射器」。
// 仅 RunStream 会注入；Run（非流式）不注入，appendTraceItem 检测到缺失即跳过，无副作用。
type stepEmitterKey struct{}

type runtimeTrace struct {
	mu    sync.Mutex
	items []ExecutionTraceItem
}

// withStepEmitter 把流式步骤事件发射器挂到 ctx 上。
func withStepEmitter(ctx context.Context, emit func(StreamEvent) error) context.Context {
	return context.WithValue(ctx, stepEmitterKey{}, emit)
}

// stepEmitterFromCtx 取出发射器（不存在时 ok=false）。
func stepEmitterFromCtx(ctx context.Context) (func(StreamEvent) error, bool) {
	e, ok := ctx.Value(stepEmitterKey{}).(func(StreamEvent) error)
	return e, ok
}

var (
	traceRegistryMu sync.Mutex
	traceRegistry   = make(map[string]*runtimeTrace)
	traceSeq        uint64
)

func newRuntimeTraceID() string {
	return "trace-" + itoa(int(atomic.AddUint64(&traceSeq, 1)))
}

func withRuntimeTrace(ctx context.Context, traceID string, trace *runtimeTrace) context.Context {
	traceRegistryMu.Lock()
	traceRegistry[traceID] = trace
	traceRegistryMu.Unlock()
	return context.WithValue(ctx, traceContextKey{}, traceID)
}

func releaseRuntimeTrace(traceID string) {
	traceRegistryMu.Lock()
	delete(traceRegistry, traceID)
	traceRegistryMu.Unlock()
}

func appendTraceItem(ctx context.Context, item ExecutionTraceItem) {
	traceID, ok := ctx.Value(traceContextKey{}).(string)
	if !ok || traceID == "" {
		return
	}
	traceRegistryMu.Lock()
	trace := traceRegistry[traceID]
	traceRegistryMu.Unlock()
	if trace == nil {
		return
	}
	trace.mu.Lock()
	trace.items = append(trace.items, item)
	trace.mu.Unlock()

	// 实时把该步转成 step SSE 事件流出（前端时间线即时展示）。
	// 仅对工具调用/检索类目发射；agent_start/done 等不发射，避免噪声。
	if em, ok := stepEmitterFromCtx(ctx); ok {
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

func traceItems(trace *runtimeTrace) []ExecutionTraceItem {
	if trace == nil {
		return nil
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	out := make([]ExecutionTraceItem, len(trace.items))
	copy(out, trace.items)
	return out
}
