package memory

import (
	"context"
	"sync"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestChatMemory_ReadWriteClear(t *testing.T) {
	ctx := context.Background()
	m := NewChatMemory(10)
	if err := m.Write(ctx, schema.UserMessage("你好")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := m.Write(ctx, schema.AssistantMessage("你好，有什么可以帮你？", nil)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := m.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("期望 2 条，实际 %d", len(got))
	}
	if got[0].Role != schema.User || got[1].Role != schema.Assistant {
		t.Errorf("角色顺序异常: %s, %s", got[0].Role, got[1].Role)
	}
	if err := m.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	got, _ = m.Read(ctx)
	if len(got) != 0 {
		t.Fatalf("Clear 后应为空，实际 %d", len(got))
	}
}

func TestChatMemory_RingBuffer(t *testing.T) {
	ctx := context.Background()
	m := NewChatMemory(3)
	for i := 0; i < 10; i++ {
		if err := m.Write(ctx, schema.UserMessage(string(rune('A'+i)))); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	got, err := m.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("环形缓冲应保留最近 3 条，实际 %d", len(got))
	}
	// 最近 3 条应为 H, I, J（A=0..J=9）
	want := []string{"H", "I", "J"}
	for i, w := range want {
		if got[i].Content != w {
			t.Errorf("位置 %d 期望 %q，实际 %q", i, w, got[i].Content)
		}
	}
}

func TestChatMemory_Concurrent(t *testing.T) {
	ctx := context.Background()
	m := NewChatMemory(1000)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = m.Write(ctx, schema.UserMessage(string(rune('A'+n%26))))
			_, _ = m.Read(ctx)
		}(i)
	}
	wg.Wait()
	got, err := m.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 50 {
		t.Fatalf("并发写入后应累计 50 条，实际 %d", len(got))
	}
}
