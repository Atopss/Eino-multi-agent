package agent

import "eino/trace"

// 本文件把已下沉到低层 eino/trace 包的领域类型与发射器函数，以「类型别名 / 函数变量」
// 的形式在 agent 包内保持原名可用。这样：
//   1. agent 包内既有的非限定引用（chat.go / orchestrator.go / manager.go 等）零改动；
//   2. 包外（如 server）通过 agent.StreamEvent / agent.RunResult.TraceItems 的引用零改动；
//   3. 工具实现得以移出 agent 成为独立子包，只依赖 eino/trace，不再反向依赖 agent。

// 领域类型别名（与 trace 包同一类型，可无缝互换）。
type (
	RAGReference       = trace.RAGReference
	ToolCallTrace      = trace.ToolCallTrace
	ExecutionTraceItem = trace.ExecutionTraceItem
	AgentStep          = trace.AgentStep
	SubTaskInfo        = trace.SubTaskInfo
	StreamEvent        = trace.StreamEvent
	runtimeTrace       = trace.RuntimeTrace
)

// 发射器函数以包级变量别名对外保持原名。
var (
	newRuntimeTrace     = trace.NewRuntimeTrace
	newRuntimeTraceID   = trace.NewRuntimeTraceID
	withRuntimeTrace    = trace.WithRuntimeTrace
	releaseRuntimeTrace = trace.ReleaseRuntimeTrace
	appendTraceItem     = trace.AppendTraceItem
	traceItems          = trace.TraceItems
	withStepEmitter     = trace.WithStepEmitter
	stepEmitterFromCtx  = trace.StepEmitterFromCtx
)
