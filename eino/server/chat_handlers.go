package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"eino/agent"

	"github.com/cloudwego/eino/schema"
)

// reqAttachment 前端上传的附件（与前端 AttachedFile 对齐）。
type reqAttachment struct {
	Name string `json:"name"`
	Data string `json:"data"` // 图片=base64 data URL；文本=纯文本；二进制=空
	Kind string `json:"kind"` // image / text / binary
	Size int64  `json:"size"`
	Mime string `json:"mime"`
}

type chatRequest struct {
	Agent             string           `json:"agent"`
	SessionID         string           `json:"sessionId"`
	Message           string           `json:"message"`
	Image             string           `json:"image"`       // 兼容旧单图字段（多图分 ---IMAGE--- 拼接）
	Images            []reqAttachment `json:"images"`     // 新：结构化图片附件
	Files             []reqAttachment `json:"files"`      // 新：结构化文件附件
	RAGTopK           int             `json:"ragTopK"`
	RAGSourceFiles    []string        `json:"ragSourceFiles"`
	RAGSourceFilter   string          `json:"ragSourceFilter"`
	RAGMaxPerSource   int            `json:"ragMaxPerSource"`
	RAGMinScore       float64         `json:"ragMinScore"`
	StrictContextOnly bool            `json:"strictContextOnly"`
	AnswerMode        string          `json:"answerMode"`
	Topology          string          `json:"topology"` // router / supervisor
	Agents            []string        `json:"agents"`   // 参与编排的子智能体
	// Model / Provider 由前端全局模型选择器传入，覆盖智能体内置的默认模型。
	// 为空时回退到智能体的内置默认模型。
	Model    string `json:"model"`
	Provider string `json:"provider"`
}

// resolveSessionID 决定本次对话归属的会话：
// 前端显式传 sessionId 时使用它；否则退化为“每个 Agent 一个会话”，
// 与改造前“单 Agent 单轮对话”的体验保持一致。
func (s *Server) resolveSessionID(provided, agent string) string {
	if trimmed := strings.TrimSpace(provided); trimmed != "" {
		return trimmed
	}
	return "agent:" + agent
}

// reqAttachmentsToAgent 把前端结构化附件转换为持久化层 Attachment。
func reqAttachmentsToAgent(in []reqAttachment) []agent.Attachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]agent.Attachment, 0, len(in))
	for _, a := range in {
		if a.Name == "" && a.Data == "" {
			continue
		}
		out = append(out, agent.Attachment{
			Name: a.Name,
			Data: a.Data,
			Kind: a.Kind,
			Size: a.Size,
			Mime: a.Mime,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// buildUserMessage 构建“本轮用户输入”的 schema.Message。
// 若含图片附件，则转为多模态 UserInputMultiContent（text + image_url）；
// 否则退回纯文本（兼容旧单图 image 字段与文件内嵌文本）。
func buildUserMessage(text string, images, files []reqAttachment) *schema.Message {
	atts := reqAttachmentsToAgent(images)
	atts = append(atts, reqAttachmentsToAgent(files)...)
	hasImage := false
	for _, a := range atts {
		if a.Kind == "image" && a.Data != "" {
			hasImage = true
			break
		}
	}
	if !hasImage {
		return schema.UserMessage(text)
	}
	parts := make([]schema.MessageInputPart, 0, len(atts)+1)
	if text != "" {
		parts = append(parts, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeText,
			Text: text,
		})
	}
	for _, a := range atts {
		if a.Kind != "image" || a.Data == "" {
			continue
		}
		b64 := a.Data
		if i := strings.Index(b64, ","); strings.HasPrefix(b64, "data:") && i >= 0 {
			b64 = b64[i+1:]
		}
		mime := a.Mime
		if mime == "" {
			mime = "image/jpeg"
		}
		parts = append(parts, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{
				MessagePartCommon: schema.MessagePartCommon{
					Base64Data: &b64,
					MIMEType:   mime,
				},
			},
		})
	}
	return &schema.Message{Role: schema.User, UserInputMultiContent: parts}
}

// sessionLock 返回某个会话的专属锁，保证同一会话的“读历史→运行→写回”原子化，
// 不同会话之间仍可并发，从而彻底消除并发请求的上下文穿插。
func (s *Server) sessionLock(id string) *sync.Mutex {
	v, loaded := s.chatLocks.LoadOrStore(id, &sync.Mutex{})
	if !loaded {
		// 每次新增会话锁时，按节流周期清理已不存在的孤儿锁，
		// 防止 chatLocks 只增不减导致长期运行内存泄漏。
		if s.chatLocksTicks.Add(1)%chatLocksGCPeriod == 0 {
			s.gcOrphanChatLocks()
		}
	}
	return v.(*sync.Mutex)
}

// loadSessionHistory 确保会话存在并返回其当前消息列表。
func (s *Server) loadSessionHistory(sessionID string) ([]*schema.Message, error) {
	if _, ok := s.sessions.GetSession(sessionID); !ok {
		s.sessions.CreateSession(sessionID)
	}
	hist, err := s.sessions.GetMessages(sessionID)
	if err != nil {
		return nil, err
	}
	// 防御性上限：喂给模型的上下文只保留最近 MaxSessionHistory 条，
	// 与存储层 maxMessages(200) 互补，专门控制模型上下文窗口，
	// 避免长会话下历史无限膨胀导致 token 成本暴涨与响应变慢。
	if max := s.runtime.MaxSessionHistory; max > 0 && len(hist) > max {
		hist = hist[len(hist)-max:]
	}
	return hist, nil
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	owner := s.requestOwner(r)
	var req chatRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxChatBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Bad request", http.StatusBadRequest)
		return
	}
	// 多智能体编排分支（非流式）。
	if req.Topology != "" {
		s.mu.RLock()
		orch := s.orchestrator
		s.mu.RUnlock()
		if orch == nil {
			jsonError(w, "orchestrator not ready", http.StatusServiceUnavailable)
			return
		}
		ctx := r.Context()
		if len(req.Agents) == 0 {
			jsonError(w, "编排模式需要指定 agents", http.StatusBadRequest)
			return
		}
		// 解析全局所选模型的覆盖凭据；失败即给出明确中文提示。
		override, ovErr := s.resolveModelOverride(req, req.Agent)
		if ovErr != nil {
			jsonError(w, friendlyModelError(ovErr), http.StatusBadRequest)
			return
		}
		topK, ragOptions, strictOnly := s.resolveRAGRequestOptions(req.RAGTopK, req.RAGMaxPerSource, req.RAGMinScore, req.RAGSourceFilter, req.RAGSourceFiles, req.StrictContextOnly)
		ragOptions.Owner = owner
		result, err := orch.Run(ctx, agent.OrchestrationInput{
			Topology:         req.Topology,
			Task:             req.Message,
			Agents:           req.Agents,
			RAGTopK:          topK,
			RAGOptions:        ragOptions,
			StrictContextOnly: strictOnly,
			AnswerMode:        req.AnswerMode,
			Owner:             owner,
			MaxSteps:          6,
			ModelOverride:     override,
		})
		if err != nil {
			writeInternalError(w, err)
			return
		}
		s.recordUsage(r, chatInputChars(req), len(result.Reply))
		jsonOK(w, result)
		return
	}

	s.mu.RLock()
	a, ok := s.agents[req.Agent]
	cfg, cfgOk := s.configs[req.Agent]
	s.mu.RUnlock()
	if !ok || !cfgOk {
		jsonError(w, "Agent not found: "+req.Agent, http.StatusNotFound)
		return
	}
	// 解析全局所选模型的覆盖凭据；失败即给出明确中文提示。
	override, ovErr := s.resolveModelOverride(req, req.Agent)
	if ovErr != nil {
		jsonError(w, friendlyModelError(ovErr), http.StatusBadRequest)
		return
	}

	sessionID := s.sessionKey(r, s.resolveSessionID(req.SessionID, req.Agent))
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()

	hist, err := s.loadSessionHistory(sessionID)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.runtime.StreamTimeoutSec)*time.Second)
	defer cancel()
	if req.Image != "" || len(req.Images) > 0 || len(req.Files) > 0 {
		userMsg := buildUserMessage(req.Message, req.Images, req.Files)
		// 非流式图片/附件对话：直接调用模型，不走 ReAct 工具链。
		// 模型覆盖时复用所选模型的凭据（保留智能体人设 SystemPrompt）。
		cfgToUse := cfg
		if override != nil {
			cfgToUse = *override
			cfgToUse.SystemPrompt = cfg.SystemPrompt
		}
		var reply string
		var err error
		if len(req.Images) > 0 || len(req.Files) > 0 {
			reply, err = s.chatWithAttachments(ctx, cfgToUse, userMsg)
		} else {
			reply, err = s.chatWithImage(ctx, cfgToUse, req.Message, req.Image)
		}
		if err != nil {
			writeInternalError(w, fmt.Errorf("%s", friendlyModelError(err)))
			return
		}
		// 持久化本轮“用户消息 + 助手回复”，保证历史可见、可续聊重喂。
		_ = s.sessions.AddMessage(sessionID, userMsg)
		_ = s.sessions.AddMessage(sessionID, &schema.Message{Role: schema.Assistant, Content: reply})
		attachments := reqAttachmentsToAgent(req.Images)
		attachments = append(attachments, reqAttachmentsToAgent(req.Files)...)
		_ = s.sessions.AttachLastUserMessage(sessionID, attachments)
		s.sessions.RegisterAgent(sessionID, req.Agent)
		_ = s.sessions.Save(sessionID)
		s.recordUsage(r, chatInputChars(req), len(reply))
	jsonOK(w, map[string]string{"reply": reply})
		return
	}
	topK, ragOptions, strictOnly := s.resolveRAGRequestOptions(req.RAGTopK, req.RAGMaxPerSource, req.RAGMinScore, req.RAGSourceFilter, req.RAGSourceFiles, req.StrictContextOnly)
	ragOptions.Owner = owner
	userMsg := buildUserMessage(req.Message, req.Images, req.Files)
	attachments := reqAttachmentsToAgent(req.Images)
	attachments = append(attachments, reqAttachmentsToAgent(req.Files)...)
	result, err := a.Run(ctx, hist, req.Message, agent.RunOptions{
		RAGTopK:             topK,
		RAGOptions:          ragOptions,
		StrictContextOnly:   strictOnly,
		AnswerMode:          req.AnswerMode,
		Owner:               owner,
		UserMessageOverride: userMsg,
		ModelOverride:       override,
	})
	if err != nil {
		writeInternalError(w, fmt.Errorf("%s", friendlyModelError(err)))
		return
	}
	// 把更新后的完整会话写回并持久化（重启不丢上下文）。
	if err := s.sessions.SetMessages(sessionID, result.Messages); err != nil {
		log.Printf("save session %s failed: %v", sessionID, err)
	}
	// 把本轮附件挂载到写回后的最后一条 user 消息。
	if err := s.sessions.AttachLastUserMessage(sessionID, attachments); err != nil {
		log.Printf("attach attachments to session %s failed: %v", sessionID, err)
	}
	s.sessions.RegisterAgent(sessionID, req.Agent)
	_ = s.sessions.Save(sessionID)
	s.recordUsage(r, chatInputChars(req), len(result.Reply))
	jsonOK(w, result)
}

func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	owner := s.requestOwner(r)
	var req chatRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxChatBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Bad request", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	a, ok := s.agents[req.Agent]
	s.mu.RUnlock()
	if !ok {
		jsonError(w, "Agent not found: "+req.Agent, http.StatusNotFound)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.runtime.StreamTimeoutSec)*time.Second)
	defer cancel()
	writeSSE := func(event string, payload interface{}) error {
		// 客户端断开后立即返回，停止后续模型生成与计费。
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	// 解析全局所选模型的覆盖凭据；失败即给出明确中文提示。
	override, ovErr := s.resolveModelOverride(req, req.Agent)
	if ovErr != nil {
		_ = writeSSE("error", agent.StreamEvent{Type: "error", Error: friendlyModelError(ovErr)})
		return
	}

	// 多智能体编排分支：优先于单 Agent 流程。
	if req.Topology != "" {
		s.mu.RLock()
		orch := s.orchestrator
		s.mu.RUnlock()
		if orch == nil {
			_ = writeSSE("error", agent.StreamEvent{Type: "error", Error: "orchestrator not ready"})
			return
		}
		if len(req.Agents) == 0 {
			_ = writeSSE("error", agent.StreamEvent{Type: "error", Error: "编排模式需要指定 agents"})
			return
		}
		topK, ragOptions, strictOnly := s.resolveRAGRequestOptions(req.RAGTopK, req.RAGMaxPerSource, req.RAGMinScore, req.RAGSourceFilter, req.RAGSourceFiles, req.StrictContextOnly)
		result, err := orch.RunStream(ctx, agent.OrchestrationInput{
			Topology:         req.Topology,
			Task:             req.Message,
			Agents:           req.Agents,
			RAGTopK:          topK,
			RAGOptions:        ragOptions,
			StrictContextOnly: strictOnly,
			AnswerMode:        req.AnswerMode,
			Owner:             owner,
			MaxSteps:          6,
			ModelOverride:     override,
		}, func(ev agent.StreamEvent) error {
			return writeSSE(ev.Type, ev)
		})
		if err != nil {
			log.Printf("orchestrator stream error: %v", err)
			_ = writeSSE("error", agent.StreamEvent{Type: "error", Error: "编排执行失败：" + friendlyModelError(err)})
			return
		}
		s.recordUsage(r, chatInputChars(req), len(result.Reply))
		_ = writeSSE("done", agent.StreamEvent{
			Type:          "done",
			Reply:         result.Reply,
			RAGQuery:      result.RAGQuery,
			RAGReferences: result.RAGReferences,
			ToolCalls:     result.ToolCalls,
			TraceItems:    result.TraceItems,
			AnswerMode:    result.AnswerMode,
		})
		return
	}

	// 流式同样纳入会话锁，保证历史读写一致。
	sessionID := s.sessionKey(r, s.resolveSessionID(req.SessionID, req.Agent))
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	hist, err := s.loadSessionHistory(sessionID)
	if err != nil {
		log.Printf("load session %s failed: %v", sessionID, err)
		_ = writeSSE("error", agent.StreamEvent{Type: "error", Error: "会话加载失败: " + err.Error()})
		return
	}

	topK, ragOptions, strictOnly := s.resolveRAGRequestOptions(req.RAGTopK, req.RAGMaxPerSource, req.RAGMinScore, req.RAGSourceFilter, req.RAGSourceFiles, req.StrictContextOnly)
	ragOptions.Owner = owner
	// 构建“本轮用户输入”消息：含图片附件时转为多模态，否则纯文本。
	userMsg := buildUserMessage(req.Message, req.Images, req.Files)
	attachments := reqAttachmentsToAgent(req.Images)
	attachments = append(attachments, reqAttachmentsToAgent(req.Files)...)
	result, err := a.RunStream(ctx, hist, req.Message, agent.RunOptions{
		RAGTopK:             topK,
		RAGOptions:          ragOptions,
		StrictContextOnly:   strictOnly,
		AnswerMode:          req.AnswerMode,
		Owner:               owner,
		UserMessageOverride: userMsg,
		ModelOverride:       override,
	}, func(event agent.StreamEvent) error {
		return writeSSE(event.Type, event)
	})
		if err != nil {
			log.Printf("chat stream error (session %s): %v", sessionID, err)
			// 把友好化的错误透传到前端，方便用户排查（Key无效、模型不存在、超时、限流、欠费等）
			_ = writeSSE("error", agent.StreamEvent{Type: "error", Error: friendlyModelError(err)})
			return
		}
		s.recordUsage(r, chatInputChars(req), len(result.Reply))
	if result.AnswerMode != "plan" {
		if err := s.sessions.SetMessages(sessionID, result.Messages); err != nil {
			log.Printf("save session %s failed: %v", sessionID, err)
		}
	}
	// 把本轮附件挂载到写回后的最后一条 user 消息，保证历史里能看到、续聊能重喂。
	if err := s.sessions.AttachLastUserMessage(sessionID, attachments); err != nil {
		log.Printf("attach attachments to session %s failed: %v", sessionID, err)
	}
	if result.AnswerMode != "plan" {
		s.sessions.RegisterAgent(sessionID, req.Agent)
		_ = s.sessions.Save(sessionID)
	}
	_ = writeSSE("done", agent.StreamEvent{
		Type:          "done",
		Reply:         result.Reply,
		RAGQuery:      result.RAGQuery,
		RAGReferences: result.RAGReferences,
		ToolCalls:     result.ToolCalls,
		TraceItems:    result.TraceItems,
		AnswerMode:    result.AnswerMode,
	})
}
