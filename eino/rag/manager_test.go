package rag

import "testing"

// TestTruncateRunes 验证按 rune（UTF-8 字符）截断，避免中文等多字节字符被切断成乱码。
func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("你好世界abc", 3); got != "你好世" {
		t.Fatalf("truncateRunes(中文,3) = %q, want %q", got, "你好世")
	}
	if got := truncateRunes("hello", 10); got != "hello" {
		t.Fatalf("truncateRunes(不截断) = %q", got)
	}
}

// TestContextFromResultsEmpty 验证空结果安全返回空串，不触发越界。
func TestContextFromResultsEmpty(t *testing.T) {
	if got := (&RAGManager{}).ContextFromResultsForQuery(nil, ""); got != "" {
		t.Fatalf("空结果应返回空串, got %q", got)
	}
}
