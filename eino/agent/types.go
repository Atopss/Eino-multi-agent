package agent

import (
	"eino/callbacks"
	"eino/config"
	"eino/rag"
	"eino/skills"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	reactflow "github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
)

// Agent 现在是"无状态"的：对话历史由调用方（Server 的会话存储）按请求传入，
// 处理完成后通过 RunResult.Messages 把更新后的完整会话交还调用方持久化。
// 这样多个并发请求各自持有独立的消息副本，彻底避免了共享可变状态导致的穿插错乱。
type Agent struct {
	config       config.AgentConfig
	model        model.ChatModel
	messageGraph compose.Runnable[chatBuildInput, []*schema.Message]
	reactAgent   *reactflow.Agent
	monitor      *callbacks.MonitoringHandler
	rag          *rag.RAGManager
	skillManager *skills.SkillManager
}

// RunOptions 控制单次对话的行为
type RunOptions struct {
	RAGTopK           int
	RAGOptions        rag.SearchOptions
	StrictContextOnly bool
	AnswerMode        string
	Owner             string // 非空时仅检索该用户自己的文档（"" 视为公共，所有人可见）
	// UserMessageOverride 非 nil 时，作为"本轮用户消息"直接进入上下文，
	// 取代由 userMsg 文本构建的 schema.UserMessage（用于携带多模态图片附件）。
	UserMessageOverride *schema.Message
}

// RunResult 单次对话结果。
// Messages 为"更新后的完整会话（含本轮用户与助手消息）"，由调用方负责持久化。
type RunResult struct {
	Reply         string
	Messages      []*schema.Message
	RAGQuery      string
	RAGReferences []RAGReference
	ToolCalls     []ToolCallTrace
	TraceItems    []ExecutionTraceItem
	AnswerMode    string
}

type chatBuildInput struct {
	UserMessage       string
	History           []*schema.Message
	RAGTopK           int
	RAGOptions        rag.SearchOptions
	StrictContextOnly bool
	AnswerMode        string
	Trace             *RAGTrace
}

type chatBuildState struct {
	UserMessage       string
	History           []*schema.Message
	RAGTopK           int
	RAGOptions        rag.SearchOptions
	RAGContext        string
	StrictContextOnly bool
	AnswerMode        string
	Trace             *RAGTrace
}

type RAGReference struct {
	ID         string  `json:"id"`
	FileName   string  `json:"fileName"`
	Source     string  `json:"source"`
	ChunkIndex int     `json:"chunkIndex"`
	Score      float64 `json:"score"`
	Chunk      string  `json:"chunk"`
	MatchType  string  `json:"matchType"`
}

type RAGTrace struct {
	Query      string         `json:"query"`
	References []RAGReference `json:"references"`
}

type ToolCallTrace struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Result    string `json:"result"`
}

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
	Kind   string `json:"kind"`            // rag | tool | model | agent
	Name   string `json:"name"`            // 同 kind 下关联 running/done 的标识（如工具名）
	Title  string `json:"title"`           // 展示标题
	Status string `json:"status"`          // running | done | error
	Input  string `json:"input,omitempty"`  // 调用入参（可选，细节折叠）
	Output string `json:"output,omitempty"` // 返回出参（可选，细节折叠）
}

type ChatResult struct {
	Reply         string               `json:"reply"`
	RAGQuery      string               `json:"ragQuery"`
	RAGReferences []RAGReference       `json:"ragReferences"`
	ToolCalls     []ToolCallTrace      `json:"toolCalls"`
	TraceItems    []ExecutionTraceItem `json:"traceItems"`
	AnswerMode    string               `json:"answerMode"`
}

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
	AgentStep     *AgentStep          `json:"agentStep,omitempty"`

	// 以下字段用于多智能体编排（Router / Supervisor）的实时时间线。
	Topology string        `json:"topology,omitempty"` // router / supervisor
	Phase    string        `json:"phase,omitempty"`    // plan / dispatch / agent / synthesize / done
	SubTask  string        `json:"subTask,omitempty"`  // 子任务描述
	Agent    string        `json:"agent,omitempty"`    // 负责该子任务的智能体名
	Step     int           `json:"step,omitempty"`     // supervisor 循环步数
	SubTasks []SubTaskInfo `json:"subTasks,omitempty"` // 规划阶段产出的子任务清单

	// Plan 模式：先输出执行计划
	Plan          string   `json:"plan,omitempty"`          // 计划文本（Markdown）
	PlannedSteps  []string `json:"plannedSteps,omitempty"`  // 从计划中解析出的步骤列表
	PlanStatus    string   `json:"planStatus,omitempty"`    // "generating" | "done"
}

// SubTaskInfo 规划阶段产出的子任务（供前端时间线展示）。
type SubTaskInfo struct {
	Agent string `json:"agent"`
	Task  string `json:"task"`
}
