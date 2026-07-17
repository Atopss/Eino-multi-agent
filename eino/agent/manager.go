package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"eino/config"
	"eino/rag"

	"github.com/cloudwego/eino/schema"
)

// ============================================================
// AgentManager - 多智能体管理器
// ============================================================
// 管理多个 Agent，支持智能体协作和路由
// ============================================================

type AgentManager struct {
	agents map[string]*Agent
	rag    *rag.RAGManager
	mu     sync.RWMutex
}

// NewAgentManager 创建智能体管理器
func NewAgentManager() *AgentManager {
	return &AgentManager{
		agents: make(map[string]*Agent),
	}
}

// SetRAG 设置共享的 RAG 知识库
func (m *AgentManager) SetRAG(r *rag.RAGManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rag = r
	for _, a := range m.agents {
		a.SetRAG(r)
	}
}

// AddAgent 添加智能体
func (m *AgentManager) AddAgent(cfg config.AgentConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	a, err := New(cfg)
	if err != nil {
		return fmt.Errorf("创建 Agent %s 失败: %w", cfg.Name, err)
	}

	if m.rag != nil {
		a.SetRAG(m.rag)
	}

	m.agents[cfg.Name] = a
	log.Printf("Agent %s 已添加到管理器", cfg.Name)
	return nil
}

// RemoveAgent 移除智能体
func (m *AgentManager) RemoveAgent(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.agents, name)
	log.Printf("Agent %s 已从管理器移除", name)
}

// GetAgent 获取指定智能体
func (m *AgentManager) GetAgent(name string) (*Agent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.agents[name]
	return a, ok
}

// GetAllAgents 获取所有智能体
func (m *AgentManager) GetAllAgents() map[string]*Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]*Agent)
	for k, v := range m.agents {
		result[k] = v
	}
	return result
}

// ListAgentNames 获取所有智能体名称
func (m *AgentManager) ListAgentNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.agents))
	for name := range m.agents {
		names = append(names, name)
	}
	return names
}

// Count 返回智能体数量
func (m *AgentManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.agents)
}

// ============================================================
// 多智能体协作功能
// ============================================================

// RouteTask 智能体路由 - 根据任务内容自动选择最合适的智能体
// 基于关键词匹配和评分机制
func (m *AgentManager) RouteTask(task string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.agents) == 0 {
		return ""
	}

	// 为每个智能体计算匹配分数
	scores := make(map[string]float64)
	for name, agent := range m.agents {
		prompt := agent.GetSystemPrompt()
		scores[name] = calculateMatchScore(task, prompt, name)
	}

	// 找到分数最高的智能体
	bestAgent := ""
	bestScore := -1.0
	for name, score := range scores {
		if score > bestScore {
			bestScore = score
			bestAgent = name
		}
	}

	// 如果没有匹配的，返回第一个
	if bestAgent == "" {
		for name := range m.agents {
			return name
		}
	}

	return bestAgent
}

// calculateMatchScore 计算任务与智能体的匹配分数
func calculateMatchScore(task, prompt, name string) float64 {
	score := 0.0
	taskLower := strings.ToLower(task)
	promptLower := strings.ToLower(prompt)
	nameLower := strings.ToLower(name)

	// 关键词匹配（基于常见任务类型）
	keywords := map[string][]string{
		"code":     {"代码", "编程", "开发", "bug", "函数", "算法", "程序"},
		"math":     {"数学", "计算", "公式", "求解", "方程"},
		"write":    {"写作", "文章", "文档", "报告", "总结"},
		"research": {"研究", "分析", "调研", "报告", "数据"},
		"design":   {"设计", "界面", "ui", "ux", "图片"},
		"chat":     {"聊天", "对话", "闲聊", "问题"},
	}

	// 检查任务关键词
	for category, words := range keywords {
		for _, word := range words {
			if strings.Contains(taskLower, word) {
				// 检查智能体是否擅长这个类别
				if strings.Contains(promptLower, category) ||
					strings.Contains(promptLower, word) {
					score += 2.0
				} else {
					score += 0.5
				}
			}
		}
	}

	// 名称匹配
	if strings.Contains(taskLower, nameLower) {
		score += 3.0
	}

	// 系统提示词匹配（简单子串匹配）
	if strings.Contains(taskLower, promptLower[:min(50, len(promptLower))]) {
		score += 1.0
	}

	return score
}

// min 返回两个整数中较小的一个
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============================================================
// 共享会话管理
// ============================================================

// Attachment 一条用户消息携带的附件（图片/文件）。
// 与前端 AttachedFile 对齐：图片为 base64 data URL，文本文件为纯文本内容。
type Attachment struct {
	Name string `json:"name"`
	Data string `json:"data"` // 图片=base64 data URL；文本=纯文本；二进制=空
	Kind string `json:"kind"` // image / text / binary
	Size int64  `json:"size"`
	Mime string `json:"mime"`
}

// StoredMessage 持久化层对一条消息的表示。
// 在 eino schema.Message（仅 text）之外，额外保留用户上传的附件，
// 以便历史会话既能显示附件、又能在续聊时把图片重新喂给模型。
type StoredMessage struct {
	Role        string       `json:"role"`
	Content     string       `json:"content"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// ToSchema 把存储消息转换为模型层 schema.Message。
// 若含图片附件，则构建多模态 UserInputMultiContent（text + image_url）。
func (m *StoredMessage) ToSchema() *schema.Message {
	msg := &schema.Message{Role: schema.RoleType(m.Role), Content: m.Content}
	if m.Role != string(schema.User) || len(m.Attachments) == 0 {
		return msg
	}
	hasImage := false
	for _, a := range m.Attachments {
		if a.Kind == "image" && a.Data != "" {
			hasImage = true
			break
		}
	}
	if !hasImage {
		return msg
	}
	parts := make([]schema.MessageInputPart, 0, len(m.Attachments)+1)
	if m.Content != "" {
		parts = append(parts, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeText,
			Text: m.Content,
		})
	}
	for _, a := range m.Attachments {
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
	msg.UserInputMultiContent = parts
	return msg
}

// SharedSession 共享会话
type SharedSession struct {
	ID       string
	Messages []*StoredMessage
	Agents   []string // 参与的智能体
	mu       sync.RWMutex
}

// SessionManager 会话管理器
type SessionManager struct {
	sessions        map[string]*SharedSession
	dataDir         string
	maxMessages     int
	maxMessageChars int
	db              *sql.DB
	mu              sync.RWMutex
}

// SetDB 注入 SQLite 连接；非 nil 时会话持久化改为走数据库（并发安全、重启不丢）。
func (s *SessionManager) SetDB(db *sql.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
}

// NewSessionManager 创建会话管理器
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions:        make(map[string]*SharedSession),
		dataDir:         filepath.Join(".", "data", "sessions"),
		maxMessages:     200,
		maxMessageChars: 12000,
	}
}

// SetDataDir 设置数据存储目录
func (s *SessionManager) SetDataDir(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dataDir = dir
	os.MkdirAll(dir, 0755)
}

// CreateSession 创建新会话
func (s *SessionManager) CreateSession(id string) *SharedSession {
	s.mu.Lock()
	defer s.mu.Unlock()

	session := &SharedSession{
		ID:       id,
		Messages: make([]*StoredMessage, 0),
		Agents:   make([]string, 0),
	}
	s.sessions[id] = session
	return session
}

// GetSession 获取会话
func (s *SessionManager) GetSession(id string) (*SharedSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	return session, ok
}

// AddMessage 添加消息到会话
func (s *SessionManager) AddMessage(sessionID string, msg *schema.Message) error {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("会话 %s 不存在", sessionID)
	}

	stored := &StoredMessage{Role: string(msg.Role), Content: msg.Content}
	session.mu.Lock()
	defer session.mu.Unlock()
	stored.Content = trimSessionString(stored.Content, s.maxMessageChars)
	session.Messages = append(session.Messages, stored)
	if len(session.Messages) > s.maxMessages {
		session.Messages = session.Messages[len(session.Messages)-s.maxMessages:]
	}
	return nil
}

// GetMessages 获取会话消息（转换为模型层 schema.Message，含多模态附件）
func (s *SessionManager) GetMessages(sessionID string) ([]*schema.Message, error) {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("会话 %s 不存在", sessionID)
	}

	session.mu.RLock()
	defer session.mu.RUnlock()
	out := make([]*schema.Message, 0, len(session.Messages))
	for _, m := range session.Messages {
		out = append(out, m.ToSchema())
	}
	return out, nil
}

// GetStoredMessages 返回原始持久化消息（含附件），供历史接口向前端透出附件。
func (s *SessionManager) GetStoredMessages(sessionID string) ([]*StoredMessage, error) {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("会话 %s 不存在", sessionID)
	}

	session.mu.RLock()
	defer session.mu.RUnlock()
	out := make([]*StoredMessage, 0, len(session.Messages))
	for _, m := range session.Messages {
		cp := &StoredMessage{Role: m.Role, Content: m.Content}
		if len(m.Attachments) > 0 {
			cp.Attachments = make([]Attachment, len(m.Attachments))
			copy(cp.Attachments, m.Attachments)
		}
		out = append(out, cp)
	}
	return out, nil
}

// Save 保存会话（优先 SQLite，无 db 时回退本地文件）
func (s *SessionManager) Save(sessionID string) error {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("会话 %s 不存在", sessionID)
	}

	session.mu.RLock()
	type sessionJSON struct {
		ID       string          `json:"id"`
		Messages []*StoredMessage `json:"messages"`
		Agents   []string        `json:"agents"`
	}
	msgs := make([]*StoredMessage, 0, len(session.Messages))
	for _, m := range session.Messages {
		cp := &StoredMessage{Role: m.Role, Content: m.Content}
		if len(m.Attachments) > 0 {
			cp.Attachments = make([]Attachment, len(m.Attachments))
			copy(cp.Attachments, m.Attachments)
		}
		msgs = append(msgs, cp)
	}
	data := sessionJSON{ID: session.ID, Messages: msgs, Agents: session.Agents}
	jsonData, err := json.MarshalIndent(data, "", "  ")
	session.mu.RUnlock()
	if err != nil {
		return err
	}

	if s.db != nil {
		userID := userFromKey(sessionID)
		_, err := s.db.ExecContext(context.Background(),
			`INSERT INTO sessions(id, user_id, data, updated_at) VALUES(?,?,?,?)
			 ON CONFLICT(id) DO UPDATE SET user_id=excluded.user_id, data=excluded.data, updated_at=excluded.updated_at`,
			sessionID, userID, string(jsonData), time.Now().Format(time.RFC3339))
		return err
	}

	// 文件回退
	if err := os.MkdirAll(s.dataDir, 0755); err != nil {
		return err
	}
	filePath := filepath.Join(s.dataDir, sessionID+".json")
	return os.WriteFile(filePath, jsonData, 0644)
}

// Load 从 SQLite（或本地文件）加载会话
func (s *SessionManager) Load(sessionID string) error {
	var jsonData []byte
	var err error
	if s.db != nil {
		var data string
		e := s.db.QueryRowContext(context.Background(), `SELECT data FROM sessions WHERE id = ?`, sessionID).Scan(&data)
		if e == sql.ErrNoRows {
			return fmt.Errorf("会话 %s 不存在", sessionID)
		} else if e != nil {
			return e
		}
		jsonData = []byte(data)
	} else {
		jsonData, err = os.ReadFile(filepath.Join(s.dataDir, sessionID+".json"))
		if err != nil {
			return err
		}
	}

	type sessionJSON struct {
		ID       string          `json:"id"`
		Messages []*StoredMessage `json:"messages"`
		Agents   []string        `json:"agents"`
	}
	var sess sessionJSON
	if err := json.Unmarshal(jsonData, &sess); err != nil {
		return err
	}

	session := &SharedSession{
		ID:       sess.ID,
		Messages: make([]*StoredMessage, 0, len(sess.Messages)),
		Agents:   sess.Agents,
	}
	for _, m := range sess.Messages {
		if m == nil {
			continue
		}
		cp := &StoredMessage{
			Role:    m.Role,
			Content: trimSessionString(m.Content, s.maxMessageChars),
		}
		if len(m.Attachments) > 0 {
			cp.Attachments = make([]Attachment, len(m.Attachments))
			copy(cp.Attachments, m.Attachments)
		}
		session.Messages = append(session.Messages, cp)
	}
	if len(session.Messages) > s.maxMessages {
		session.Messages = session.Messages[len(session.Messages)-s.maxMessages:]
	}

	s.mu.Lock()
	s.sessions[sessionID] = session
	s.mu.Unlock()

	log.Printf("已加载会话: %s", sessionID)
	return nil
}

// LoadAll 加载所有会话（SQLite 或本地文件目录）
func (s *SessionManager) LoadAll() {
	if s.db != nil {
		rows, err := s.db.QueryContext(context.Background(), `SELECT id FROM sessions`)
		if err != nil {
			log.Printf("读取会话失败: %v", err)
			return
		}
		ids := make([]string, 0)
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				continue
			}
			ids = append(ids, id)
		}
		rows.Close()
		loaded := 0
		for _, id := range ids {
			if err := s.Load(id); err != nil {
				log.Printf("加载会话 %s 失败: %v", id, err)
				continue
			}
			loaded++
		}
		if loaded > 0 {
			log.Printf("已加载 %d 个会话", loaded)
		}
		return
	}

	s.mu.RLock()
	dataDir := s.dataDir
	s.mu.RUnlock()
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		log.Printf("读取会话目录失败: %v", err)
		return
	}
	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".json")
		if err := s.Load(sessionID); err != nil {
			log.Printf("加载会话 %s 失败: %v", sessionID, err)
			continue
		}
		loaded++
	}
	if loaded > 0 {
		log.Printf("已加载 %d 个会话", loaded)
	}
}

// SaveAll 保存所有会话
func (s *SessionManager) SaveAll() {
	s.mu.RLock()
	sessions := make(map[string]*SharedSession)
	for k, v := range s.sessions {
		sessions[k] = v
	}
	s.mu.RUnlock()

	for sessionID := range sessions {
		if err := s.Save(sessionID); err != nil {
			log.Printf("保存会话 %s 失败: %v", sessionID, err)
		}
	}
}

// ListSessions 获取所有会话ID（含命名空间前缀）
func (s *SessionManager) ListSessions() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	return ids
}

// ListUserSessions 获取某用户可见的会话ID（剥离 user 命名空间前缀）。
func (s *SessionManager) ListUserSessions(userID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix := userID + "/"
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		if userID == "" {
			if !strings.Contains(id, "/") {
				ids = append(ids, id)
			}
			continue
		}
		if strings.HasPrefix(id, prefix) {
			ids = append(ids, strings.TrimPrefix(id, prefix))
		}
	}
	return ids
}

// DeleteSession 删除会话（同时清理数据库与本地文件）
func (s *SessionManager) DeleteSession(sessionID string) error {
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()

	if s.db != nil {
		if _, err := s.db.ExecContext(context.Background(), `DELETE FROM sessions WHERE id = ?`, sessionID); err != nil {
			return err
		}
	}
	// 删除本地文件（回退兼容）
	filePath := filepath.Join(s.dataDir, sessionID+".json")
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return err
	}

	log.Printf("已删除会话: %s", sessionID)
	return nil
}

// userFromKey 从命名空间化的会话键（userID/clientID）中提取 user 部分。
func userFromKey(key string) string {
	if i := strings.Index(key, "/"); i >= 0 {
		return key[:i]
	}
	return "legacy"
}

func trimSessionString(value string, maxLen int) string {
	return truncateRunes(value, maxLen)
}

// SetMessages 用“完整会话”整体替换某会话的消息，并做上限裁剪与空值过滤。
// 该函数配合 Agent.Run 的返回值，实现“每次请求重新加载历史→运行→整体写回”的持久化模型。
// 模型层 schema.Message 会被转换为持久化层 StoredMessage（纯文本，附件需另行挂载）。
func (s *SessionManager) SetMessages(sessionID string, msgs []*schema.Message) error {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("会话 %s 不存在", sessionID)
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	return s.setMessagesLocked(session, msgs)
}

// setMessagesLocked 在持有 session.mu 写锁时替换消息。
func (s *SessionManager) setMessagesLocked(session *SharedSession, msgs []*schema.Message) error {
	cleaned := make([]*StoredMessage, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		cleaned = append(cleaned, &StoredMessage{Role: string(m.Role), Content: m.Content})
	}
	if len(cleaned) > s.maxMessages {
		cleaned = cleaned[len(cleaned)-s.maxMessages:]
	}
	session.Messages = cleaned
	return nil
}

// AttachLastUserMessage 给当前会话最后一条 user 消息挂载附件（图片/文件）。
// 用于“本轮用户输入含附件”时，在 Run 返回的完整会话写回前补回附件，
// 保证历史里每条带图消息既能显示、又能在续聊时重喂模型。
func (s *SessionManager) AttachLastUserMessage(sessionID string, attachments []Attachment) error {
	if len(attachments) == 0 {
		return nil
	}
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("会话 %s 不存在", sessionID)
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	for i := len(session.Messages) - 1; i >= 0; i-- {
		if session.Messages[i].Role == string(schema.User) {
			// 仅当该消息尚未挂载相同附件时追加，避免重复。
			existing := make(map[string]bool, len(session.Messages[i].Attachments))
			for _, a := range session.Messages[i].Attachments {
				existing[a.Name+"|"+a.Kind] = true
			}
			for _, a := range attachments {
				if existing[a.Name+"|"+a.Kind] {
					continue
				}
				session.Messages[i].Attachments = append(session.Messages[i].Attachments, a)
			}
			return nil
		}
	}
	return nil
}

// RegisterAgent 记录某会话参与过的智能体（用于会话面板展示）。
func (s *SessionManager) RegisterAgent(sessionID, agent string) {
	if agent == "" {
		return
	}
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	for _, a := range session.Agents {
		if a == agent {
			return
		}
	}
	if len(session.Agents) < 50 {
		session.Agents = append(session.Agents, agent)
	}
}
