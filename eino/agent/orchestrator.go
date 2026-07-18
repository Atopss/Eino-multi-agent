package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"eino/config"
	"eino/rag"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// 多智能体编排拓扑：Supervisor（主管决策 → 子智能体执行 → 回到主管循环，步数上限防无限）。
// 基于 Eino 的 compose.Graph 表达；协调者由参与智能体列表的首个（跳过不存在的）自动担任。
type Topology string

const (
	TopologySupervisor Topology = "supervisor"
)

// SubTask 规划阶段产出的子任务；主管决策复用该结构，Finish 表示任务结束。
type SubTask struct {
	Agent  string `json:"agent"`
	Task   string `json:"task"`
	Finish bool   `json:"finish,omitempty"`
}

// SubResult 单个子智能体执行结果。
type SubResult struct {
	Agent         string
	Reply         string
	TraceItems    []ExecutionTraceItem
	RAGReferences []RAGReference
	ToolCalls     []ToolCallTrace
}

// OrchestrationInput 一次编排请求的输入。
type OrchestrationInput struct {
	Topology         string
	Task             string
	Agents           []string // 参与的子智能体名（配置期确定）
	History           []*schema.Message
	RAGTopK          int
	RAGOptions        rag.SearchOptions
	StrictContextOnly bool
	AnswerMode        string
	Owner             string // 非空时仅检索该用户自己的文档
	MaxSteps          int // supervisor 循环步数上限
	// ModelOverride 非空时，覆盖所有参与智能体（含协调者）本轮使用的模型，
	// 由全局模型选择器传入，实现"全局切换模型"。
	ModelOverride *config.AgentConfig
}

// OrchestrationResult 编排结果，字段对齐 RunResult 便于 server 层统一处理。
type OrchestrationResult struct {
	Reply         string
	RAGQuery      string
	RAGReferences []RAGReference
	ToolCalls     []ToolCallTrace
	TraceItems    []ExecutionTraceItem
	AnswerMode    string
}

// Orchestrator 多智能体编排器：基于 compose.Graph 组织两种拓扑，
// 子智能体本身仍是 Eino ReAct 图（Agent.RunStream），编排层只负责拆解/调度/汇聚。
type Orchestrator struct {
	manager *AgentManager
}

// NewOrchestrator 创建编排器，复用 AgentManager 中的子智能体。
func NewOrchestrator(m *AgentManager) *Orchestrator {
	return &Orchestrator{manager: m}
}

// emitFunc 与 server 层 SSE 写出对齐的事件回调。
type emitFunc func(StreamEvent) error

// safeEmitter 并发安全的事件发射器：多个子智能体并发执行时避免 SSE 帧交错。
type safeEmitter struct {
	mu sync.Mutex
	fn emitFunc
}

func (s *safeEmitter) emit(ev StreamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fn(ev)
}

// coordinatorAgent 在参与编排的智能体列表里挑选协调者：
// 优先选内置主控（Locked）智能体，使其成为真正的“控制者”；
// 否则取 names 中第一个真实存在的智能体（跳过不存在的）。
// “谁是协调者”由 Locked 标记自动决定，无需任何用户标记，也避免 worker 互相委派形成环路。
func (o *Orchestrator) coordinatorAgent(agents map[string]*Agent, names []string) (*Agent, string, error) {
	for _, name := range names {
		if a, ok := agents[name]; ok && a.config.Locked {
			return a, name, nil
		}
	}
	for _, name := range names {
		if a, ok := agents[name]; ok {
			return a, name, nil
		}
	}
	return nil, "", fmt.Errorf("协调者智能体缺失")
}

// RunStream 执行多智能体编排，过程事件通过 emit 实时回传。
func (o *Orchestrator) RunStream(ctx context.Context, in OrchestrationInput, emit emitFunc) (OrchestrationResult, error) {
	se := &safeEmitter{fn: emit}

	// 过滤出实际存在、可参与的子智能体。
	validAgents := make([]string, 0, len(in.Agents))
	agentMap := make(map[string]*Agent, len(in.Agents))
	for _, name := range in.Agents {
		if a, ok := o.manager.GetAgent(name); ok {
			validAgents = append(validAgents, name)
			agentMap[name] = a
		}
	}
	if len(validAgents) == 0 {
		return OrchestrationResult{}, fmt.Errorf("没有可用的子智能体参与编排")
	}
	in.Agents = validAgents

	// 协调者：内置主控（Locked）优先，否则取首个真实存在的智能体。
	coord, coordName, err := o.coordinatorAgent(agentMap, validAgents)
	if err != nil {
		return OrchestrationResult{}, err
	}
	// 子智能体池 = 除协调者以外的其它智能体；协调者只负责统筹、不把自己当作可被委派的 worker，
	// 从而真正“控制”其它智能体，而非把任务又派给自己。
	workerNames := make([]string, 0, len(validAgents))
	for _, n := range validAgents {
		if n != coordName {
			workerNames = append(workerNames, n)
		}
	}

	// 没有任何其它子智能体时，协调者直接作答。
	if len(workerNames) == 0 {
		reply, gErr := coord.GenerateForModel(ctx, []*schema.Message{
			schema.SystemMessage(coord.GetSystemPrompt()),
			schema.UserMessage(in.Task),
		}, in.ModelOverride)
		if gErr != nil {
			return OrchestrationResult{}, gErr
		}
		return OrchestrationResult{Reply: reply, AnswerMode: in.AnswerMode, RAGQuery: in.Task}, nil
	}

	// 编排分支恒走 Supervisor（前端仅发送 single / supervisor，single 时不进此分支）。
	return o.runSupervisor(ctx, in, agentMap, coordName, workerNames, se)
}

// Run 非流式入口：用空 emit 收集最终结果，供非流接口复用。
func (o *Orchestrator) Run(ctx context.Context, in OrchestrationInput) (OrchestrationResult, error) {
	return o.RunStream(ctx, in, func(StreamEvent) error { return nil })
}



// plan 调用协调者 LLM 把任务拆解为子任务清单，并实时回传规划事件。
func (o *Orchestrator) plan(ctx context.Context, in OrchestrationInput, agents map[string]*Agent, coordName string, workerNames []string, se *safeEmitter) ([]SubTask, error) {
	_ = se.emit(StreamEvent{
		Type: "orchestration", Phase: "plan", Topology: in.Topology,
		Message: "正在规划任务拆解与智能体分配",
	})
	coordinator, ok := agents[coordName]
	if !ok {
		return nil, fmt.Errorf("协调者智能体缺失")
	}
	const plannerSystem = "你是一个任务规划器。给定用户任务和一组可用智能体，请把任务拆解为可由单个智能体独立完成的子任务，并指定负责该子任务的智能体名称（必须来自可用列表）。只输出 JSON：{\"tasks\":[{\"agent\":\"智能体名\",\"task\":\"子任务描述\"}]}。不要输出其他内容。"
	raw, err := coordinator.GenerateForModel(ctx, []*schema.Message{
		schema.SystemMessage(plannerSystem),
		schema.UserMessage(buildPlannerPrompt(in.Task, workerNames)),
	}, in.ModelOverride)
	if err != nil {
		return nil, err
	}
	tasks := parseSubTasks(raw, workerNames)
	if len(tasks) == 0 {
		tasks = []SubTask{{Agent: in.Agents[0], Task: in.Task}}
	}
	_ = se.emit(StreamEvent{
		Type: "orchestration", Phase: "dispatch", Topology: in.Topology,
		Message: fmt.Sprintf("规划完成，分配给 %d 个子智能体并行处理", len(tasks)),
		SubTasks: toSubTaskInfos(tasks),
	})
	return tasks, nil
}

// toSubTaskInfos 把内部子任务结构转为前端时间线展示结构。
func toSubTaskInfos(tasks []SubTask) []SubTaskInfo {
	out := make([]SubTaskInfo, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, SubTaskInfo{Agent: t.Agent, Task: t.Task})
	}
	return out
}

// runSubAgentsConcurrently 并发驱动各子智能体执行，各自事件实时回传。
func (o *Orchestrator) runSubAgentsConcurrently(ctx context.Context, in OrchestrationInput, tasks []SubTask, agents map[string]*Agent, se *safeEmitter) ([]SubResult, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("规划未产出任何子任务")
	}
	var wg sync.WaitGroup
	results := make([]SubResult, len(tasks))
	errs := make([]error, len(tasks))

	for i, t := range tasks {
		wg.Add(1)
		go func(i int, t SubTask) {
			defer wg.Done()
			a, ok := agents[t.Agent]
			if !ok {
				errs[i] = fmt.Errorf("子智能体 %s 不存在", t.Agent)
				return
			}
			_ = se.emit(StreamEvent{
				Type: "orchestration", Phase: "agent", Topology: in.Topology,
				Agent: t.Agent, SubTask: t.Task, Message: t.Agent + " 开始处理子任务",
			})
			res, err := a.RunStream(ctx, nil, t.Task, RunOptions{
				RAGTopK:           in.RAGTopK,
				RAGOptions:        in.RAGOptions,
				StrictContextOnly: in.StrictContextOnly,
				AnswerMode:        in.AnswerMode,
				Owner:             in.Owner,
				ModelOverride:     in.ModelOverride,
			}, func(ev StreamEvent) error {
				ev.Agent = t.Agent
				if ev.Phase == "" {
					ev.Phase = "agent"
				}
				return se.emit(ev)
			})
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = SubResult{
				Agent:         t.Agent,
				Reply:         res.Reply,
				TraceItems:    res.TraceItems,
				RAGReferences: res.RAGReferences,
				ToolCalls:     res.ToolCalls,
			}
			_ = se.emit(StreamEvent{
				Type: "orchestration", Phase: "agent", Topology: in.Topology,
				Agent: t.Agent, Message: t.Agent + " 完成子任务",
			})
		}(i, t)
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	return results, nil
}

// synthesize 把各子智能体结果交给协调者 LLM 合成最终回答。
func (o *Orchestrator) synthesize(ctx context.Context, in OrchestrationInput, results []SubResult, coordName string, agents map[string]*Agent, se *safeEmitter) (OrchestrationResult, error) {
	_ = se.emit(StreamEvent{
		Type: "orchestration", Phase: "synthesize", Topology: in.Topology,
		Message: "正在汇聚各子智能体结果",
	})
	coordinator, ok := agents[coordName]
	if !ok {
		return OrchestrationResult{}, fmt.Errorf("协调者智能体缺失")
	}
	var b strings.Builder
	for _, r := range results {
		b.WriteString("【" + r.Agent + "】\n" + r.Reply + "\n\n")
	}
	prompt := "原始任务：\n" + in.Task + "\n\n各子智能体返回：\n" + b.String() +
		"\n\n请综合以上结果，去掉内部协调痕迹，给出面向用户的最终完整回答。"
	reply, err := coordinator.GenerateForModel(ctx, []*schema.Message{
		schema.SystemMessage("你是多智能体系统的结果合成器，负责把各子智能体的回答整合为面向用户的最终答复。"),
		schema.UserMessage(prompt),
	}, in.ModelOverride)
	if err != nil {
		return OrchestrationResult{}, err
	}
	final := OrchestrationResult{Reply: reply, AnswerMode: in.AnswerMode, RAGQuery: in.Task}
	for _, r := range results {
		final.TraceItems = append(final.TraceItems, r.TraceItems...)
		final.RAGReferences = append(final.RAGReferences, r.RAGReferences...)
		final.ToolCalls = append(final.ToolCalls, r.ToolCalls...)
	}
	return final, nil
}

// =====================================================================
// Supervisor 拓扑：主管决策 → 子智能体执行 → 回到主管（循环）
// =====================================================================

func (o *Orchestrator) runSupervisor(ctx context.Context, in OrchestrationInput, agents map[string]*Agent, coordName string, workerNames []string, se *safeEmitter) (OrchestrationResult, error) {
	maxSteps := in.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 6
	}
	g := compose.NewGraph[OrchestrationInput, OrchestrationResult]()
	if err := g.AddLambdaNode("planner", compose.InvokableLambda(func(ctx context.Context, in OrchestrationInput) (*schema.Message, error) {
		return schema.UserMessage(in.Task), nil
	})); err != nil {
		return OrchestrationResult{}, err
	}
	if err := g.AddLambdaNode("supervisor", compose.InvokableLambda(func(ctx context.Context, task *schema.Message) (OrchestrationResult, error) {
		return o.supervisorLoop(ctx, in, agents, coordName, workerNames, task, maxSteps, se)
	})); err != nil {
		return OrchestrationResult{}, err
	}
	if err := g.AddEdge(compose.START, "planner"); err != nil {
		return OrchestrationResult{}, err
	}
	if err := g.AddEdge("planner", "supervisor"); err != nil {
		return OrchestrationResult{}, err
	}
	if err := g.AddEdge("supervisor", compose.END); err != nil {
		return OrchestrationResult{}, err
	}
	compiled, err := g.Compile(ctx, compose.WithGraphName("EinoSupervisorOrchestration"))
	if err != nil {
		return OrchestrationResult{}, err
	}
	return compiled.Invoke(ctx, in)
}

// supervisorLoop 主管循环：每轮让协调者 LLM 决策下一步（委派某子智能体 / 结束），
// 直到判定完成或达到步数上限，最后合成最终回答。
func (o *Orchestrator) supervisorLoop(ctx context.Context, in OrchestrationInput, agents map[string]*Agent, coordName string, workerNames []string, taskMsg *schema.Message, maxSteps int, se *safeEmitter) (OrchestrationResult, error) {
	coordinator, ok := agents[coordName]
	if !ok {
		return OrchestrationResult{}, fmt.Errorf("协调者智能体缺失")
	}
	avail := strings.Join(workerNames, ", ")
	conv := []*schema.Message{
		schema.SystemMessage(buildSupervisorSystemPrompt(avail)),
		taskMsg,
	}
	finalTrace := []ExecutionTraceItem{}
	finalRefs := []RAGReference{}
	finalTools := []ToolCallTrace{}

	_ = se.emit(StreamEvent{
		Type: "orchestration", Phase: "plan", Topology: in.Topology,
		Message: "主管开始循环调度子智能体",
	})

	for step := 0; step < maxSteps; step++ {
		decisionRaw, err := coordinator.GenerateForModel(ctx, conv, in.ModelOverride)
		if err != nil {
			return OrchestrationResult{}, err
		}
		decision, parseErr := parseSupervisorDecision(decisionRaw, workerNames)
		if parseErr != nil || decision.Finish {
			_ = se.emit(StreamEvent{
				Type: "orchestration", Phase: "synthesize", Topology: in.Topology, Step: step,
				Message: "主管判定任务已完成，生成最终回答",
			})
			break
		}
		a, ok := agents[decision.Agent]
		if !ok {
			// 指定智能体不存在，反馈给主管重新决策。
			conv = append(conv,
				schema.AssistantMessage(decisionRaw, nil),
				schema.UserMessage(fmt.Sprintf("指定的智能体 %s 不存在，请重新决策或结束。", decision.Agent)),
			)
			continue
		}
		_ = se.emit(StreamEvent{
			Type: "orchestration", Phase: "agent", Topology: in.Topology, Step: step,
			Agent: decision.Agent, SubTask: decision.Task,
			Message: fmt.Sprintf("主管委派 %s 执行：%s", decision.Agent, decision.Task),
		})
		res, err := a.RunStream(ctx, nil, decision.Task, RunOptions{
			RAGTopK:           in.RAGTopK,
			RAGOptions:        in.RAGOptions,
			StrictContextOnly: in.StrictContextOnly,
			AnswerMode:        in.AnswerMode,
			Owner:             in.Owner,
			ModelOverride:     in.ModelOverride,
		}, func(ev StreamEvent) error {
			ev.Agent = decision.Agent
			if ev.Phase == "" {
				ev.Phase = "agent"
			}
			ev.Step = step
			return se.emit(ev)
		})
		if err != nil {
			return OrchestrationResult{}, err
		}
		finalTrace = append(finalTrace, res.TraceItems...)
		finalRefs = append(finalRefs, res.RAGReferences...)
		finalTools = append(finalTools, res.ToolCalls...)
		// 把本轮观察追加进主管上下文，作为下一轮决策依据。
		conv = append(conv,
			schema.AssistantMessage(decisionRaw, nil),
			schema.UserMessage(fmt.Sprintf("【%s 的执行结果】\n%s", decision.Agent, res.Reply)),
		)
	}

	finalReply, err := coordinator.GenerateForModel(ctx, append(conv,
		schema.UserMessage("现在请基于以上全部交互，给出面向用户的最终完整回答。")), in.ModelOverride)
	if err != nil {
		return OrchestrationResult{}, err
	}
	return OrchestrationResult{
		Reply:         finalReply,
		TraceItems:    finalTrace,
		RAGReferences: finalRefs,
		ToolCalls:     finalTools,
		AnswerMode:    in.AnswerMode,
	}, nil
}

// =====================================================================
// 规划 / 决策相关的提示词与解析
// =====================================================================

func buildPlannerPrompt(task string, agents []string) string {
	return "可用智能体：" + strings.Join(agents, ", ") +
		"\n\n用户任务：\n" + task +
		"\n\n请把任务拆解为可由单个智能体独立完成的子任务，并指定负责该子任务的智能体名称（必须来自上面的可用列表）。"
}

func buildSupervisorSystemPrompt(avail string) string {
	return "你是一个多智能体系统的主管（supervisor）。你负责管理一组子智能体完成任务。\n" +
		"每轮你只能输出一个 JSON 决策：\n" +
		"  {\"finish\":false,\"agent\":\"智能体名\",\"task\":\"给该智能体的指令\"} 表示委派给某智能体；\n" +
		"  {\"finish\":true} 表示任务已完成，可生成最终回答。\n" +
		"可用智能体：" + avail + "\n" +
		"不要输出 JSON 以外的解释文字。"
}

func parseSubTasks(raw string, validAgents []string) []SubTask {
	valid := make(map[string]bool, len(validAgents))
	for _, n := range validAgents {
		valid[n] = true
	}
	raw = extractJSON(raw)
	var parsed struct {
		Tasks []SubTask `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	out := make([]SubTask, 0, len(parsed.Tasks))
	for _, t := range parsed.Tasks {
		if t.Agent == "" || t.Task == "" {
			continue
		}
		if !valid[t.Agent] {
			continue
		}
		out = append(out, t)
	}
	return out
}

func parseSupervisorDecision(raw string, validAgents []string) (SubTask, error) {
	valid := make(map[string]bool, len(validAgents))
	for _, n := range validAgents {
		valid[n] = true
	}
	raw = extractJSON(raw)
	var d struct {
		Finish bool   `json:"finish"`
		Agent string `json:"agent"`
		Task  string `json:"task"`
	}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return SubTask{}, err
	}
	if d.Finish {
		return SubTask{Finish: true}, nil
	}
	if !valid[d.Agent] || d.Task == "" {
		return SubTask{}, fmt.Errorf("决策非法：agent=%q task=%q", d.Agent, d.Task)
	}
	return SubTask{Agent: d.Agent, Task: d.Task}, nil
}

// extractJSON 从可能夹带解释文字的模型输出中尽可能抽取 JSON 子串。
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return s
	}
	return s[start : end+1]
}
