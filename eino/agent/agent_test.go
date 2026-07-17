package agent

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

// TestLimitStringUTF8 验证按 rune 截断不会切断中文等多字节字符（修复点）。
func TestLimitStringUTF8(t *testing.T) {
	if got := limitString("你好世界", 2); got != "你好" {
		t.Fatalf("limitString(中文,2) = %q, want %q", got, "你好")
	}
	if got := limitString("hello", 3); got != "hel" {
		t.Fatalf("limitString(ascii,3) = %q, want %q", got, "hel")
	}
	if got := limitString("hi", 10); got != "hi" {
		t.Fatalf("limitString(不截断) = %q", got)
	}
}

// TestCloneMessagesIndependent 验证 cloneMessages 返回深拷贝，调用方持有的历史不会被 Agent 修改。
// 这是“无状态 Agent”并发隔离的基础：每个请求拿到独立副本。
func TestCloneMessagesIndependent(t *testing.T) {
	src := []*schema.Message{schema.UserMessage("a")}
	cp := cloneMessages(src)
	cp[0].Content = "mutated"
	if src[0].Content != "a" {
		t.Fatal("cloneMessages 与原始消息共享底层数据，并发会互相污染")
	}
}

// TestTrimMessages 验证只保留最近 N 条。
func TestTrimMessages(t *testing.T) {
	msgs := make([]*schema.Message, 0, 5)
	for i := 0; i < 5; i++ {
		msgs = append(msgs, &schema.Message{Content: string(rune('A' + i))})
	}
	got := trimMessages(msgs, 3)
	if len(got) != 3 || got[0].Content != "C" {
		t.Fatalf("trimMessages 结果 = %v, 期望最后 3 条且首条为 C", got)
	}
	if len(trimMessages(msgs, 0)) != 5 {
		t.Fatal("trimMessages(max=0) 不应截断")
	}
}

// TestNormalizeAnswerMode 验证大小写归一与未知值回退。
func TestNormalizeAnswerMode(t *testing.T) {
	if normalizeAnswerMode("STRICT") != "strict" {
		t.Fatal("normalizeAnswerMode 未处理大写")
	}
	if normalizeAnswerMode("weird") != "balanced" {
		t.Fatal("normalizeAnswerMode 未回退到 balanced")
	}
}
