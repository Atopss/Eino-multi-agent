package rag

import (
	"context"
	"testing"
)

// 编译期断言：RAGManager 实现 Retriever，DefaultReranker 实现 Reranker。
var (
	_ Retriever = (*RAGManager)(nil)
	_ Reranker  = DefaultReranker{}
)

func mkDoc(id string, score float64) ScoredDocument {
	return ScoredDocument{
		Document: Document{ID: id, Chunk: "chunk-" + id},
		Score:    score,
		MatchType: "hit",
	}
}

func TestDefaultReranker_StableDesc(t *testing.T) {
	r := DefaultReranker{}
	in := []ScoredDocument{
		mkDoc("a", 0.3),
		mkDoc("b", 0.9), // 最高分
		mkDoc("c", 0.3), // 与 a 同分，应保持原相对顺序（a 先于 c）
		mkDoc("d", 0.5),
	}
	out, err := r.Rerank(context.Background(), "q", in)
	if err != nil {
		t.Fatalf("Rerank error: %v", err)
	}
	if len(out) != 4 {
		t.Fatalf("期望 4 条，实际 %d", len(out))
	}
	wantOrder := []string{"b", "d", "a", "c"}
	for i, id := range wantOrder {
		if out[i].Document.ID != id {
			t.Errorf("位置 %d 期望 %s，实际 %s", i, id, out[i].Document.ID)
		}
	}
	// 同分稳定性：a(0.3) 应排在 c(0.3) 之前（in 中 a 在前）
	if out[2].Document.ID != "a" || out[3].Document.ID != "c" {
		t.Errorf("同分稳定性被破坏: %s, %s", out[2].Document.ID, out[3].Document.ID)
	}
}

func TestDefaultReranker_Dedup(t *testing.T) {
	r := DefaultReranker{}
	in := []ScoredDocument{
		mkDoc("dup", 0.9),
		mkDoc("x", 0.8),
		mkDoc("dup", 0.1), // 与首条同 ID，应被去重
	}
	out, err := r.Rerank(context.Background(), "q", in)
	if err != nil {
		t.Fatalf("Rerank error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("期望去重后 2 条，实际 %d", len(out))
	}
	if out[0].Document.ID != "dup" || out[1].Document.ID != "x" {
		t.Errorf("去重结果异常: %s, %s", out[0].Document.ID, out[1].Document.ID)
	}
}

func TestDefaultReranker_Empty(t *testing.T) {
	r := DefaultReranker{}
	out, err := r.Rerank(context.Background(), "q", nil)
	if err != nil || len(out) != 0 {
		t.Fatalf("空输入应返回空，got len=%d err=%v", len(out), err)
	}
}
