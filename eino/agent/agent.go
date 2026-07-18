package agent

import (
	"context"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"

	"eino/callbacks"
	"eino/config"
	"eino/rag"
	"eino/skills"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	reactflow "github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
)

// 模型身份键：把模型凭据组合为唯一字符串，用于 react agent 缓存去重。
func modelKey(ov config.AgentConfig) string {
	return ov.APIKey + "|" + ov.ProviderType + "|" + ov.BaseURL + "|" + ov.ModelID
}

func New(cfg config.AgentConfig) (*Agent, error) {
	m, err := createModel(cfg)
	if err != nil {
		return nil, err
	}
	a := &Agent{
		config: cfg,
		model:   m,
	}
	if err := a.initEinoComponents(context.Background()); err != nil {
		return nil, err
	}
	return a, nil
}

// getModel 返回本轮对话使用的底层模型：有覆盖则用覆盖，否则用智能体内置默认模型。
func (a *Agent) getModel(ov *config.AgentConfig) (model.ChatModel, error) {
	if ov == nil {
		return a.model, nil
	}
	return createModel(*ov)
}

// getReactAgent 返回编译好的 ReAct 智能体：有模型覆盖时按模型身份缓存复用，
// 避免每轮对话都重新编译 eino 图（开销大）；无覆盖时用内置默认 reactAgent。
func (a *Agent) getReactAgent(ctx context.Context, ov *config.AgentConfig) (*reactflow.Agent, error) {
	if ov == nil {
		return a.reactAgent, nil
	}
	key := modelKey(*ov)
	a.reactMu.Lock()
	defer a.reactMu.Unlock()
	if cached, ok := a.reactCache[key]; ok {
		return cached, nil
	}
	m, err := createModel(*ov)
	if err != nil {
		return nil, err
	}
	ra, err := a.buildReactAgent(ctx, m)
	if err != nil {
		return nil, err
	}
	a.reactCache[key] = ra
	return ra, nil
}

// GenerateForModel 直接用指定模型（覆盖或默认）生成文本，不进入 ReAct / 工具调用流程。
func (a *Agent) GenerateForModel(ctx context.Context, messages []*schema.Message, ov *config.AgentConfig) (string, error) {
	m, err := a.getModel(ov)
	if err != nil {
		return "", err
	}
	resp, err := m.Generate(ctx, messages)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("模型返回为空")
	}
	return resp.Content, nil
}

func (a *Agent) SetRAG(r *rag.RAGManager)               { a.rag = r }
func (a *Agent) GetRAG() *rag.RAGManager                { return a.rag }
func (a *Agent) SetSkillManager(m *skills.SkillManager) { a.skillManager = m }
func (a *Agent) GetSkillManager() *skills.SkillManager  { return a.skillManager }
func (a *Agent) GetName() string                        { return a.config.Name }
func (a *Agent) GetSystemPrompt() string                { return a.config.SystemPrompt }

func (a *Agent) initEinoComponents(ctx context.Context) error {
	messageGraph, err := a.buildMessageGraph(ctx)
	if err != nil {
		return err
	}
	reactAgent, err := a.buildReactAgent(ctx, a.model)
	if err != nil {
		return err
	}
	a.messageGraph = messageGraph
	a.reactAgent = reactAgent
	a.reactCache = make(map[string]*reactflow.Agent)
	a.monitor = callbacks.NewMonitoringHandler(a.config.Name)
	return nil
}

// buildReactAgent 基于给定底层模型编译一个 ReAct 智能体。
// 模型可为智能体内置默认模型，也可为运行时按所选模型动态创建的模型。
func (a *Agent) buildReactAgent(ctx context.Context, m model.ChatModel) (*reactflow.Agent, error) {
	toolList, err := GetAllTools(a.config.NeedTools)
	if err != nil {
		return nil, err
	}
	reactAgent, err := reactflow.NewAgent(ctx, &reactflow.AgentConfig{
		Model: m,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools:               toolList,
			ExecuteSequentially: true,
			UnknownToolsHandler: func(ctx context.Context, name, input string) (string, error) {
				return "未知工具: " + name, nil
			},
		},
		// DeepSeek 等模型在流式输出时，会先输出文本、随后才在后续 chunk
		// 中给出 tool_calls。默认的 firstChunkStreamToolCallChecker 只检查首块，
		// 会在看到首块文本后误判为“无工具调用”而直接结束，导致工具永不执行。
		// 这里用全量检测器：读完整个流，只要任一 chunk 含 tool_calls 就继续走工具节点。
		StreamToolCallChecker: fullStreamToolCallChecker,
		MaxStep:       40,
		GraphName:     "EinoLocalReActAgent",
		ModelNodeName: "模型推理",
		ToolsNodeName: "工具执行",
	})
	if err != nil {
		return nil, err
	}
	return reactAgent, nil
}

func (a *Agent) buildMessageGraph(ctx context.Context) (compose.Runnable[chatBuildInput, []*schema.Message], error) {
	g := compose.NewGraph[chatBuildInput, []*schema.Message]()
	if err := g.AddLambdaNode("prepare_input", compose.InvokableLambda(func(ctx context.Context, input chatBuildInput) (chatBuildState, error) {
		state := chatBuildState{
			UserMessage: input.UserMessage,
			History:     input.History,
			RAGTopK:     input.RAGTopK,
			RAGOptions:  input.RAGOptions,
			AnswerMode:  normalizeAnswerMode(input.AnswerMode),
			Trace:       input.Trace,
		}
		state.StrictContextOnly = input.StrictContextOnly || state.AnswerMode == "strict"
		return state, nil
	})); err != nil {
		return nil, err
	}

	if err := g.AddLambdaNode("rag_retrieve", compose.InvokableLambda(func(ctx context.Context, state chatBuildState) (chatBuildState, error) {
		if a.rag != nil && a.rag.Count() > 0 {
			topK := state.RAGTopK
			if topK <= 0 {
				topK = 3
			}
			ragQuery := buildRAGSearchQuery(state.History, state.UserMessage)
			appendTraceItem(ctx, ExecutionTraceItem{
				Type:    "rag_search",
				Stage:   "rag",
				Agent:   a.config.Name,
				Message: "检索本地知识库",
				Result:  ragQuery,
			})
			results, err := a.rag.SearchWithOptions(ctx, ragQuery, topK, state.RAGOptions)
			if err != nil {
				log.Printf("Agent %s: RAG 检索失败: %v", a.config.Name, err)
			} else {
				state.RAGContext = a.rag.ContextFromResultsForQuery(results, ragQuery)
				appendTraceItem(ctx, ExecutionTraceItem{
					Type:    "rag_result",
					Stage:   "rag",
					Agent:   a.config.Name,
					Message: "本地知识库命中 " + itoa(len(results)) + " 条",
					Result:  ragQuery,
				})
				if state.Trace != nil {
					state.Trace.Query = ragQuery
					state.Trace.References = toRAGReferences(results)
				}
			}
		}
		return state, nil
	})); err != nil {
		return nil, err
	}

	if err := g.AddLambdaNode("build_messages", compose.InvokableLambda(func(ctx context.Context, state chatBuildState) ([]*schema.Message, error) {
		systemPrompt := a.config.SystemPrompt
		if a.rag != nil {
			systemPrompt += "\n\n当前本地知识库状态：已初始化，源文件数 " + itoa(a.rag.SourceFileCount()) + "，切片数 " + itoa(a.rag.Count()) + "。"
		}
		if state.RAGContext != "" {
			systemPrompt += "\n\n参考资料：\n" + state.RAGContext + "\n\n请优先基于以上资料回答。每条关键结论后尽量标注来源，例如：[文件名 切片 N]。资料不足时先说明资料不足，再补充通用知识，并明确哪些内容来自资料库外。"
		} else if state.StrictContextOnly {
			systemPrompt += "\n\n本轮没有检索到可用参考资料。用户要求只基于知识库回答，所以请直接说明资料库中没有找到相关内容，不要使用资料库外知识补充。"
		}
		systemPrompt += answerModePrompt(state.AnswerMode)
		if state.StrictContextOnly {
			systemPrompt += "\n\n严格模式：只能依据本轮参考资料回答；如果参考资料不足，请明确说资料库没有足够信息，不要自由发挥。"
		}
		if a.skillManager != nil {
			if skillsPrompt := a.skillManager.GetAgentSkillsPrompt(a.config.Name); skillsPrompt != "" {
				systemPrompt += skillsPrompt
			}
		}
		msgs := make([]*schema.Message, 0, len(state.History)+1)
		msgs = append(msgs, schema.SystemMessage(systemPrompt))
		msgs = append(msgs, state.History...)
		return msgs, nil
	})); err != nil {
		return nil, err
	}
	if err := g.AddEdge(compose.START, "prepare_input"); err != nil {
		return nil, err
	}
	if err := g.AddEdge("prepare_input", "rag_retrieve"); err != nil {
		return nil, err
	}
	if err := g.AddEdge("rag_retrieve", "build_messages"); err != nil {
		return nil, err
	}
	if err := g.AddEdge("build_messages", compose.END); err != nil {
		return nil, err
	}
	return g.Compile(ctx, compose.WithGraphName("EinoRAGMessageGraph"))
}

func cloneMessages(messages []*schema.Message) []*schema.Message {
	cloned := make([]*schema.Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		copied := *msg
		cloned = append(cloned, &copied)
	}
	return cloned
}

// limitString 按 UTF-8 字符（rune）截断，避免切断多字节字符导致乱码。
func limitString(value string, maxLen int) string {
	return truncateRunes(value, maxLen)
}

// truncateRunes 按 rune 数量安全截断字符串。
func truncateRunes(value string, maxRunes int) string {
	if maxRunes <= 0 || len([]rune(value)) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes])
}

// trimMessages 保留最近 max 条消息，避免历史无限增长。
func trimMessages(messages []*schema.Message, max int) []*schema.Message {
	if max <= 0 || len(messages) <= max {
		return messages
	}
	return messages[len(messages)-max:]
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

func normalizeAnswerMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "locate", "strict", "summary":
		return strings.TrimSpace(strings.ToLower(mode))
	default:
		return "balanced"
	}
}

func answerModePrompt(mode string) string {
	switch normalizeAnswerMode(mode) {
	case "locate":
		return "\n\n当前回答模式：定位资料。请优先告诉用户信息在哪个文件、哪个切片、对应原文大意是什么；如果能直接回答，也要先给结论，再列来源位置。不要展开过多无关解释。"
	case "strict":
		return "\n\n当前回答模式：严格资料。只能根据本轮参考资料回答；不要使用资料库外知识补充。每个关键结论都要标注来源文件和切片。"
	case "summary":
		return "\n\n当前回答模式：总结复习。请基于本轮参考资料先做整体总结，再按知识点分层梳理，最后列出可复习的要点；保留关键字段、状态值、流程分支和代码位置。"
	default:
		return "\n\n当前回答模式：学习问答。优先使用本地资料，用清楚易懂的方式回答；资料不足时可以补充通用知识，但必须明确哪些是资料库外补充。"
	}
}

func buildRAGSearchQuery(history []*schema.Message, current string) string {
	parts := make([]string, 0, 4)
	for i := len(history) - 1; i >= 0 && len(parts) < 3; i-- {
		msg := history[i]
		if msg == nil || msg.Role != schema.User {
			continue
		}
		content := limitString(strings.TrimSpace(msg.Content), 1000)
		if content == "" {
			continue
		}
		parts = append(parts, content)
	}
	if len(parts) == 0 {
		return current
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "\n")
}

func toRAGReferences(results []rag.ScoredDocument) []RAGReference {
	refs := make([]RAGReference, 0, len(results))
	for _, result := range results {
		source := result.Document.Metadata["source"]
		if source == "" {
			source = result.Document.ID
		}
		matchType := result.MatchType
		if matchType == "" {
			matchType = "hit"
		}
		refs = append(refs, RAGReference{
			ID:         result.Document.ID,
			FileName:   rag.SourceFileName(result.Document),
			Source:     source,
			ChunkIndex: rag.ChunkIndex(result.Document),
			Score:      result.Score,
			Chunk:      limitString(rag.CleanText(result.Document.Chunk), 800),
			MatchType:  matchType,
		})
	}
	return refs
}

// fullStreamToolCallChecker 读取完整的模型流式输出（直到 EOF），
// 只要任意 chunk 包含 tool_calls 就返回 true。
// 适用于先输出文本、后输出工具调用的模型（如 DeepSeek / Claude 类）。
// 注意：按 eino 约定，必须在返回前关闭传入的流。
func fullStreamToolCallChecker(_ context.Context, sr *schema.StreamReader[*schema.Message]) (bool, error) {
	defer sr.Close()
	hasToolCall := false
	for {
		msg, err := sr.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false, err
		}
		if len(msg.ToolCalls) > 0 {
			hasToolCall = true
		}
	}
	return hasToolCall, nil
}
