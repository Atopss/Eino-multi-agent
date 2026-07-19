package agent

import (
	"sync"

	"eino/callbacks"
	"eino/config"
	"eino/memory"
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
	// reactCache 按模型身份缓存编译好的 ReAct 智能体，支持运行时切换模型而不每次重编译。
	// 采用带容量上限与 TTL 的 LRU 实现（见 react_cache.go），避免无限增长。
	reactCache *reactAgentCache
	reactMu     sync.Mutex
	monitor      *callbacks.MonitoringHandler
	rag          *rag.RAGManager
	skillManager *skills.SkillManager
	// memory 是可选记忆组件。nil（默认）时 Agent 保持无状态：
	// 历史完全由调用方按请求传入。非 nil 且调用方未传历史时，
	// 才从 Memory 读取作为本轮上下文种子（见 seedHistory）。
	memory memory.Memory
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
	// ModelOverride 非 nil 时，覆盖本轮对话使用的模型（凭据字段），
	// 由全局模型选择器传入；nil 时回退到智能体内置的默认模型。
	ModelOverride *config.AgentConfig
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

// RAGReference / ToolCallTrace / ExecutionTraceItem / AgentStep / StreamEvent /
// SubTaskInfo 等领域类型已下沉到低层 eino/trace 包，agent 通过 trace_alias.go
// 中的类型别名保持原名可用（包内与 server 引用零改动）。

type RAGTrace struct {
	Query      string         `json:"query"`
	References []RAGReference `json:"references"`
}

type ChatResult struct {
	Reply         string               `json:"reply"`
	RAGQuery      string               `json:"ragQuery"`
	RAGReferences []RAGReference       `json:"ragReferences"`
	ToolCalls     []ToolCallTrace      `json:"toolCalls"`
	TraceItems    []ExecutionTraceItem `json:"traceItems"`
	AnswerMode    string               `json:"answerMode"`
}
