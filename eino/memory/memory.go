// Package memory 提供对话记忆的抽象与实现。
//
// 设计定位：Agent 本身刻意保持「无状态」——对话历史由上层
// server 的会话存储在每次请求时传入（见 agent.Agent 注释），
// 这样可以避免并发请求共享可变状态。因此本包提供的是「可插拔组件」：
//
//   - 既可作为 server 会话层的记忆后端（持久化 / 跨请求保持上下文）；
//   - 也可通过 agent.Agent.SetMemory 以「可选、只读种子」方式接入：
//     仅当调用方未传入历史时，才从 Memory 读取作为本轮上下文，
//     一旦调用方传入历史则完全保持原有无状态行为，零回归。
//
// 标准做法是将「状态从计算中分离」：Memory 是状态容器，Agent 是计算，
// 二者通过接口解耦，具体存储（内存 / DB / 向量）可自由替换。
package memory

import (
	"context"
	"sync"

	"github.com/cloudwego/eino/schema"
)

// Memory 是对话记忆抽象，用于在多次请求之间保持上下文。
// 实现可以是内存、数据库、向量库等。
type Memory interface {
	// Read 返回当前累积的会话消息（不含 system，system 由 prompt 每次独立注入）。
	Read(ctx context.Context) ([]*schema.Message, error)
	// Write 写入一条消息（通常为 user 或 assistant）。
	Write(ctx context.Context, msg *schema.Message) error
	// Clear 清空记忆。
	Clear(ctx context.Context) error
}

// ChatMemory 是内存版实现：
//   - 并发安全（sync.Mutex）；
//   - 环形缓冲：超过 maxTurns 时丢弃最早的消息，避免上下文无限增长；
//   - 不持有 system 消息。
type ChatMemory struct {
	mu       sync.Mutex
	msgs     []*schema.Message
	maxTurns int
}

// NewChatMemory 创建内存记忆，maxTurns<=0 时默认保留最近 20 条。
func NewChatMemory(maxTurns int) *ChatMemory {
	if maxTurns <= 0 {
		maxTurns = 20
	}
	return &ChatMemory{maxTurns: maxTurns}
}

// Read 返回当前消息的拷贝。
func (m *ChatMemory) Read(_ context.Context) ([]*schema.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*schema.Message, len(m.msgs))
	copy(out, m.msgs)
	return out, nil
}

// Write 追加一条消息，并按环形缓冲裁剪到最近 maxTurns 条。
func (m *ChatMemory) Write(_ context.Context, msg *schema.Message) error {
	if msg == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, msg)
	if len(m.msgs) > m.maxTurns {
		m.msgs = m.msgs[len(m.msgs)-m.maxTurns:]
	}
	return nil
}

// Clear 清空所有记忆。
func (m *ChatMemory) Clear(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = m.msgs[:0]
	return nil
}

// Append 批量写入（例如一次性存入一轮 user + assistant）。
func (m *ChatMemory) Append(ctx context.Context, msgs ...*schema.Message) error {
	for _, msg := range msgs {
		if err := m.Write(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

// 编译期断言：*ChatMemory 实现 Memory 接口。
var _ Memory = (*ChatMemory)(nil)
