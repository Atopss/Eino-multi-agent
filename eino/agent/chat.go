package agent

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	einocb "github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/schema"
)

// defaultMaxHistory 单次对话保留的“最近消息条数”上限。
const defaultMaxHistory = 20

// Run 是 Agent 的核心无状态入口。
// answerMode: "ask"=纯对话, "plan"=仅出计划, 其他=完整ReAct。
func (a *Agent) Run(ctx context.Context, history []*schema.Message, userMsg string, opts RunOptions) (RunResult, error) {
	opts.AnswerMode = normalizeAnswerMode(opts.AnswerMode)
	if opts.AnswerMode == "strict" {
		opts.StrictContextOnly = true
	}

	// Ask / Plan：无需 RAG / ReAct，直接 Generate
	if opts.AnswerMode == "ask" || opts.AnswerMode == "plan" {
		prompt := userMsg
		if opts.AnswerMode == "plan" {
			prompt = fmt.Sprintf(
				"用户需求如下，请输出简短的分步执行计划（Markdown有序列表），不要执行：\n\n%s\n\n用 1. 2. 3. 列出步骤。",
				limitString(userMsg, 4000),
			)
		}
		conv := append(cloneMessages(history), schema.UserMessage(prompt))
		conv = trimMessages(conv, defaultMaxHistory)
		resp, err := a.Generate(ctx, conv)
		if err != nil {
			return RunResult{}, err
		}
		reply := limitString(resp, 12000)
		conv = append(conv, &schema.Message{Role: schema.Assistant, Content: reply})
		conv = trimMessages(conv, defaultMaxHistory)
		return RunResult{Reply: reply, Messages: conv, AnswerMode: opts.AnswerMode}, nil
	}

	// Craft：完整 ReAct
	start := time.Now()
	trace := &RAGTrace{}
	runtimeTrace := &runtimeTrace{items: make([]ExecutionTraceItem, 0, 16)}
	traceID := newRuntimeTraceID()
	ctx = withRuntimeTrace(ctx, traceID, runtimeTrace)
	defer releaseRuntimeTrace(traceID)
	ctx = einocb.InitCallbacks(ctx, &einocb.RunInfo{}, a.monitor)

	appendTraceItem(ctx, ExecutionTraceItem{
		Type: "agent_start", Agent: a.config.Name, Message: a.config.Name + " 开始处理请求",
	})

	opts.RAGOptions.Owner = opts.Owner
	userMsg = limitString(userMsg, 12000)

	var userSchema *schema.Message
	if opts.UserMessageOverride != nil {
		userSchema = opts.UserMessageOverride
	} else {
		userSchema = schema.UserMessage(userMsg)
	}
	conv := append(cloneMessages(history), userSchema)
	conv = trimMessages(conv, defaultMaxHistory)

	msgs, err := a.messageGraph.Invoke(ctx, chatBuildInput{
		UserMessage: userMsg, History: conv, RAGTopK: opts.RAGTopK,
		RAGOptions: opts.RAGOptions, StrictContextOnly: opts.StrictContextOnly,
		AnswerMode: opts.AnswerMode, Trace: trace,
	})
	if err != nil {
		return RunResult{}, err
	}

	var resp *schema.Message
	if err := withRetry(ctx, 3, func() error {
		r, e := a.reactAgent.Generate(ctx, msgs)
		if e != nil {
			return e
		}
		resp = r
		return nil
	}); err != nil {
		return RunResult{}, err
	}
	reply := limitString(resp.Content, 12000)
	conv = append(conv, &schema.Message{Role: schema.Assistant, Content: reply})
	conv = trimMessages(conv, defaultMaxHistory)

	log.Printf("Agent %s: Eino ReAct graph completed in %v", a.config.Name, time.Since(start))
	appendTraceItem(ctx, ExecutionTraceItem{
		Type: "agent_done", Agent: a.config.Name, Message: a.config.Name + " 完成处理",
	})
	traceItems := traceItems(runtimeTrace)
	traceItems = ensureRAGTraceItems(traceItems, a.config.Name, trace)
	return RunResult{
		Reply: reply, Messages: conv, RAGQuery: trace.Query,
		RAGReferences: trace.References, ToolCalls: traceItemsToToolCalls(traceItems),
		TraceItems: traceItems, AnswerMode: opts.AnswerMode,
	}, nil
}

// RunStream 与 Run 行为一致，但以流式方式把增量内容通过 emit 回传。
// answerMode 驱动行为：
//
//	"ask"  → 纯对话（Generate，不走 ReAct / 不调工具 / 不检索 RAG）
//	"plan" → 仅生成执行计划，不实际执行
//	"craft" / "balanced" / "strict" → 完整 ReAct 流程
func (a *Agent) RunStream(ctx context.Context, history []*schema.Message, userMsg string, opts RunOptions, emit func(StreamEvent) error) (RunResult, error) {
	start := time.Now()
	opts.AnswerMode = normalizeAnswerMode(opts.AnswerMode)
	if opts.AnswerMode == "strict" {
		opts.StrictContextOnly = true
	}

	// ---- Ask 模式：纯对话，不走 ReAct / 不调工具 / 不检索 RAG ----
	if opts.AnswerMode == "ask" {
		return a.runAskStream(ctx, history, userMsg, emit, start)
	}

	// ---- Plan 模式：仅生成执行计划，不执行 ----
	if opts.AnswerMode == "plan" {
		return a.runPlanOnlyStream(ctx, userMsg, emit)
	}

	// ---- Craft 模式（默认）：完整 ReAct ----
	return a.runReactStream(ctx, history, userMsg, opts, emit, start)
}

// runAskStream Ask 模式：纯对话，仅 Generate，不走 ReAct、不调工具、不检索 RAG。
func (a *Agent) runAskStream(ctx context.Context, history []*schema.Message, userMsg string, emit func(StreamEvent) error, start time.Time) (RunResult, error) {
	if err := emit(StreamEvent{Type: "status", Stage: "start", Message: "对话模式"}); err != nil {
		return RunResult{}, err
	}
	// 用最近历史 + 本轮用户输入构建 prompt
	var askHistory []*schema.Message
	trimmed := trimMessages(cloneMessages(history), defaultMaxHistory)
	askHistory = append(trimmed, schema.UserMessage(userMsg))
	askHistory = trimMessages(askHistory, defaultMaxHistory)

	if err := emit(StreamEvent{Type: "status", Stage: "model", Message: "生成回答"}); err != nil {
		return RunResult{}, err
	}
	resp, err := a.model.Generate(ctx, askHistory)
	if err != nil {
		return RunResult{}, fmt.Errorf("Ask 模式生成失败: %w", err)
	}
	content := limitString(resp.Content, 12000)
	// 流式逐字输出
	for _, r := range content {
		if err := emit(StreamEvent{Type: "delta", Delta: string(r)}); err != nil {
			return RunResult{}, err
		}
	}

	conv := append(cloneMessages(history), schema.UserMessage(userMsg))
	conv = append(conv, &schema.Message{Role: schema.Assistant, Content: content})
	conv = trimMessages(conv, defaultMaxHistory)
	log.Printf("Agent %s: Ask completed in %v", a.config.Name, time.Since(start))
	if err := emit(StreamEvent{Type: "status", Stage: "done", Message: "完成"}); err != nil {
		return RunResult{}, err
	}
	return RunResult{
		Reply:      content,
		Messages:   conv,
		AnswerMode: "ask",
	}, nil
}

// runPlanOnlyStream Plan 模式：仅生成执行计划，不实际执行。计划通过 "plan" SSE 事件流出。
func (a *Agent) runPlanOnlyStream(ctx context.Context, userMsg string, emit func(StreamEvent) error) (RunResult, error) {
	if err := emit(StreamEvent{
		Type:       "plan",
		PlanStatus: "generating",
		Message:    "正在生成执行计划…",
	}); err != nil {
		return RunResult{}, err
	}
	planPrompt := fmt.Sprintf(
		"用户提出了以下需求，请你先输出一份简短、可操作的分步执行计划（用 Markdown 有序列表），不要执行、不要调用工具，只输出计划：\n\n%s\n\n请用 1. 2. 3. 的格式列出步骤。",
		limitString(userMsg, 4000),
	)
	planText, planErr := a.Generate(ctx, []*schema.Message{schema.UserMessage(planPrompt)})
	if planErr != nil {
		log.Printf("Agent %s: 生成计划失败: %v", a.config.Name, planErr)
	}
	if planText == "" {
		planText = "（未能生成执行计划，将直接执行）"
	}
	plannedSteps := parsePlannedSteps(planText)
	if err := emit(StreamEvent{
		Type:         "plan",
		Plan:         planText,
		PlannedSteps: plannedSteps,
		PlanStatus:   "done",
		Message:      "执行计划已生成",
	}); err != nil {
		return RunResult{}, err
	}
	if err := emit(StreamEvent{Type: "status", Stage: "done", Message: "等待确认执行"}); err != nil {
		return RunResult{}, err
	}
	return RunResult{
		Reply:      "",
		Messages:   nil, // Plan 不更新会话（等确认后用 Craft 执行）
		AnswerMode: "plan",
	}, nil
}

// runReactStream Craft 模式：完整 ReAct 流程（RAG + 工具调用）。
func (a *Agent) runReactStream(ctx context.Context, history []*schema.Message, userMsg string, opts RunOptions, emit func(StreamEvent) error, start time.Time) (RunResult, error) {
	trace := &RAGTrace{}
	runtimeTrace := &runtimeTrace{items: make([]ExecutionTraceItem, 0, 16)}
	traceID := newRuntimeTraceID()
	ctx = withRuntimeTrace(ctx, traceID, runtimeTrace)
	defer releaseRuntimeTrace(traceID)
	ctx = einocb.InitCallbacks(ctx, &einocb.RunInfo{}, a.monitor)
	ctx = withStepEmitter(ctx, emit)

	appendTraceItem(ctx, ExecutionTraceItem{
		Type:    "agent_start",
		Agent:   a.config.Name,
		Message: a.config.Name + " 开始处理请求",
	})
	if err := emit(StreamEvent{Type: "status", Stage: "start", Message: "开始处理问题"}); err != nil {
		return RunResult{}, err
	}

	userMsg = limitString(userMsg, 12000)
	var userSchema *schema.Message
	if opts.UserMessageOverride != nil {
		userSchema = opts.UserMessageOverride
	} else {
		userSchema = schema.UserMessage(userMsg)
	}
	conv := append(cloneMessages(history), userSchema)
	conv = trimMessages(conv, defaultMaxHistory)

	if err := emit(StreamEvent{Type: "status", Stage: "prepare", Message: "准备对话上下文和本地资料检索"}); err != nil {
		return RunResult{}, err
	}
	msgs, err := a.messageGraph.Invoke(ctx, chatBuildInput{
		UserMessage:       userMsg,
		History:           conv,
		RAGTopK:           opts.RAGTopK,
		RAGOptions:        opts.RAGOptions,
		StrictContextOnly: opts.StrictContextOnly,
		AnswerMode:        opts.AnswerMode,
		Trace:             trace,
	})
	if err != nil {
		return RunResult{}, err
	}
	if trace.Query != "" {
		if err := emit(StreamEvent{
			Type:          "status",
			Stage:         "rag",
			Message:       "本地知识库检索完成，命中 " + itoa(len(trace.References)) + " 条参考资料",
			RAGQuery:      trace.Query,
			RAGReferences: trace.References,
			AnswerMode:    opts.AnswerMode,
		}); err != nil {
			return RunResult{}, err
		}
	} else {
		if err := emit(StreamEvent{Type: "status", Stage: "rag", Message: "本轮未使用到本地知识库参考资料", AnswerMode: opts.AnswerMode}); err != nil {
			return RunResult{}, err
		}
	}

	if trace.Query != "" {
		_ = emit(StreamEvent{Type: "step", AgentStep: &AgentStep{
			Kind: "rag", Name: "rag", Title: "检索本地知识库", Status: "done",
			Input: trace.Query, Output: "命中 " + itoa(len(trace.References)) + " 条参考资料",
		}})
	}

	if err := emit(StreamEvent{
		Type: "meta", RAGQuery: trace.Query, RAGReferences: trace.References, AnswerMode: opts.AnswerMode,
	}); err != nil {
		return RunResult{}, err
	}

	if err := emit(StreamEvent{Type: "status", Stage: "model", Message: "开始调用模型生成回答"}); err != nil {
		return RunResult{}, err
	}
	var stream *schema.StreamReader[*schema.Message]
	if err := withRetry(ctx, 3, func() error {
		st, e := a.reactAgent.Stream(ctx, msgs)
		if e != nil {
			return e
		}
		stream = st
		return nil
	}); err != nil {
		return RunResult{}, err
	}
	if stream == nil {
		return RunResult{}, fmt.Errorf("模型流初始化失败（stream 为 nil）")
	}
	defer stream.Close()

	var reply strings.Builder
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return RunResult{}, err
		}
		if chunk == nil || chunk.Content == "" {
			continue
		}
		delta := limitString(chunk.Content, 4000)
		reply.WriteString(delta)
		if err := emit(StreamEvent{Type: "delta", Delta: delta}); err != nil {
			return RunResult{}, err
		}
	}

	content := limitString(reply.String(), 12000)
	conv = append(conv, &schema.Message{Role: schema.Assistant, Content: content})
	conv = trimMessages(conv, defaultMaxHistory)
	log.Printf("Agent %s: React stream completed in %v", a.config.Name, time.Since(start))
	if err := emit(StreamEvent{Type: "status", Stage: "done", Message: "回答生成完成"}); err != nil {
		return RunResult{}, err
	}
	appendTraceItem(ctx, ExecutionTraceItem{
		Type: "agent_done", Agent: a.config.Name, Message: a.config.Name + " 完成处理",
	})
	traceItems := traceItems(runtimeTrace)
	traceItems = ensureRAGTraceItems(traceItems, a.config.Name, trace)
	return RunResult{
		Reply:         content,
		Messages:      conv,
		RAGQuery:      trace.Query,
		RAGReferences: trace.References,
		ToolCalls:     traceItemsToToolCalls(traceItems),
		TraceItems:    traceItems,
		AnswerMode:    opts.AnswerMode,
	}, nil
}

func traceItemsToToolCalls(items []ExecutionTraceItem) []ToolCallTrace {
	out := make([]ToolCallTrace, 0)
	for _, item := range items {
		if item.Type != "tool_call" {
			continue
		}
		out = append(out, ToolCallTrace{
			Name:      item.Name,
			Arguments: item.Arguments,
			Result:    "",
		})
	}
	for i := range out {
		for _, item := range items {
			if item.Type == "tool_result" && item.Name == out[i].Name && out[i].Result == "" {
				out[i].Result = item.Result
				break
			}
		}
	}
	return out
}

func ensureRAGTraceItems(items []ExecutionTraceItem, agentName string, trace *RAGTrace) []ExecutionTraceItem {
	if trace == nil || trace.Query == "" {
		return items
	}
	hasSearch := false
	hasResult := false
	for _, item := range items {
		if item.Type == "rag_search" {
			hasSearch = true
		}
		if item.Type == "rag_result" {
			hasResult = true
		}
	}
	if !hasSearch {
		items = append(items, ExecutionTraceItem{
			Type:    "rag_search",
			Stage:   "rag",
			Agent:   agentName,
			Message: "检索本地知识库",
			Result:  trace.Query,
		})
	}
	if !hasResult {
		items = append(items, ExecutionTraceItem{
			Type:    "rag_result",
			Stage:   "rag",
			Agent:   agentName,
			Message: "本地知识库命中 " + itoa(len(trace.References)) + " 条",
			Result:  trace.Query,
		})
	}
	return items
}

// Generate 直接调用底层模型生成文本（不进入 ReAct / 工具调用流程），
// 供编排层的规划（planner）与结果合成（synthesizer）使用。
func (a *Agent) Generate(ctx context.Context, messages []*schema.Message) (string, error) {
	resp, err := a.model.Generate(ctx, messages)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("模型返回为空")
	}
	return resp.Content, nil
}

// parsePlannedSteps 从 Markdown 计划文本中解析有序列表步骤（1. / 2. / 3. 格式）
func parsePlannedSteps(plan string) []string {
	lines := strings.Split(plan, "\n")
	var steps []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 匹配 "1. xxx" / "1) xxx" / "1、xxx" 格式
		if len(trimmed) < 3 || trimmed[0] < '1' || trimmed[0] > '9' {
			continue
		}
		rest := strings.TrimLeft(strings.TrimLeft(trimmed[1:], ".、) "), ".、) ")
		if len(rest) > 0 {
			steps = append(steps, rest)
		}
	}
	return steps
}
