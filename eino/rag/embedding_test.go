package rag

import (
	"context"
	"testing"
)

// 无密钥 → local-only：向量维度恒为 256，且不要求联网。
func TestEmbedderLocalOnlyDim(t *testing.T) {
	e, err := NewEmbedder("", "") // 无密钥
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}
	if e.shouldUseRemote() {
		t.Fatal("无密钥时 shouldUseRemote 应为 false")
	}
	v, err := e.EmbedText(context.Background(), "你好世界")
	if err != nil {
		t.Fatalf("EmbedText: %v", err)
	}
	if len(v) != 256 {
		t.Fatalf("本地兜底维度应为 256，实际 %d", len(v))
	}
	if e.Dim() != 256 {
		t.Fatalf("Dim() 应为 256，实际 %d", e.Dim())
	}
	// 同进程内任意文本维度必须一致（杜绝“混合维度 → 检索静默失效”）。
	v2, _ := e.EmbedText(context.Background(), "another text sample 123")
	if len(v2) != 256 {
		t.Fatalf("第二次 embedding 维度不一致：%d", len(v2))
	}
}

// localEmbedding 必须产出指定维度（含 0 退化为默认 256）。
func TestLocalEmbeddingDim(t *testing.T) {
	for _, d := range []int{0, 128, 256, 1024} {
		want := d
		if want <= 0 {
			want = 256
		}
		v := localEmbedding("测试 embedding 维度", d)
		if len(v) != want {
			t.Fatalf("localEmbedding(dim=%d) 长度应为 %d，实际 %d", d, want, len(v))
		}
	}
}

// 远程向量维度变化时应被对齐到已锁定维度，而非静默失效。
func TestNormalizeDimAlignsRemote(t *testing.T) {
	e, _ := NewEmbedder("", "")
	// 首次远程成功 → 锁定维度 1024
	remote := make([]float64, 1024)
	out := e.normalizeDim(remote)
	if len(out) != 1024 || e.Dim() != 1024 {
		t.Fatalf("首次锁定失败: len=%d dim=%d", len(out), e.Dim())
	}
	// 之后远程返回维度变化（如 768），应被对齐到 1024
	changed := make([]float64, 768)
	out2 := e.normalizeDim(changed)
	if len(out2) != 1024 {
		t.Fatalf("维度对齐失败: 期望 1024，实际 %d", len(out2))
	}
}

// 长度不等的向量相似度必须为 0，检索层据此退化为关键词检索（而非把 0 当作命中）。
func TestCosineMismatchReturnsZero(t *testing.T) {
	a := make([]float64, 256)
	b := make([]float64, 1024)
	if CosineSimilarity(a, b) != 0 {
		t.Fatal("长度不等的向量 CosineSimilarity 应为 0")
	}
}

// 端到端验证：无密钥（local-only）下 RAG 仍能建索引并检索，不崩溃、有结果（非静默失效）。
func TestRAGLocalOnlyRoundtrip(t *testing.T) {
	dir := t.TempDir()
	m, err := NewRAGManager("", "", dir)
	if err != nil {
		t.Fatalf("NewRAGManager(local-only): %v", err)
	}
	ctx := context.Background()
	if err := m.AddText(ctx, "doc1", "Eino 是字节开源的 AI 应用开发框架，支持 ReAct 与 compose.Graph。", map[string]string{"source": "doc1"}, ""); err != nil {
		t.Fatalf("AddText: %v", err)
	}
	res, err := m.Search(ctx, "Eino 是什么框架", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("local-only 检索应返回至少 1 条结果，实际为空（静默失效）")
	}
}
