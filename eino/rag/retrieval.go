package rag

import (
	"context"
	"sort"
)

// ============================================================
// 检索抽象层
// ============================================================
// 把「检索」与「重排」从 RAGManager 的具体实现中抽离为接口，
// 使上层 agent 只依赖抽象、不依赖具体检索后端，符合依赖倒置原则：
//
//	Agent ──▶ rag.Retriever ──▶ *RAGManager（默认实现）
//	                          └─▶ 任意可插拔后端（云端向量库 / 第三方检索）
//
// 重排（Reranker）则是「召回后精排」的可插拔阶段，默认启用
// DefaultReranker（确定性、零依赖），其行为与现有检索排序等价，
// 同时提供去重与稳定排序，可作为接入更强重排模型（如交叉编码器）的扩展点。

// Retriever 是 RAG 检索的抽象接口。
// RAGManager 实现了该接口，因此可被替换为任意检索后端，而上层 agent 无需改动。
type Retriever interface {
	// Retrieve 根据查询召回相关文档片段。
	//   - query: 用户查询（将被 NormalizeSearchQuery 归一化）
	//   - topK: 期望返回的最大候选数
	//   - opts: 过滤 / owner 隔离 / 邻居扩展等检索选项
	Retrieve(ctx context.Context, query string, topK int, opts SearchOptions) ([]ScoredDocument, error)
}

// Reranker 是「检索后重排」的抽象接口。
// 在向量 / 关键词召回候选之后，由重排器对候选做精细化打分与排序，
// 例如引入更精确的语义匹配、去重、上下文连续性等信号。
// 可通过 RAGManager.SetReranker 注入自定义实现。
type Reranker interface {
	// Rerank 对召回候选做重排，返回排序后的列表。
	Rerank(ctx context.Context, query string, docs []ScoredDocument) ([]ScoredDocument, error)
}

// DefaultReranker 提供零依赖、确定性的重排：
//  1. 按文档 ID 去重（保留首次出现），避免同一切片以 hit / neighbor 重复出现；
//  2. 按分数稳定降序排序，保证等价分数下的顺序可复现。
// 在默认检索已经过混合打分与邻居扩展的前提下，该重排与现有结果等价，
// 但更稳健（稳定排序 + 显式去重），并作为更强重排模型的扩展点。
type DefaultReranker struct{}

// Rerank 实现 Reranker 接口。
func (DefaultReranker) Rerank(_ context.Context, _ string, docs []ScoredDocument) ([]ScoredDocument, error) {
	if len(docs) == 0 {
		return docs, nil
	}
	seen := make(map[string]bool, len(docs))
	out := make([]ScoredDocument, 0, len(docs))
	for _, d := range docs {
		if seen[d.Document.ID] {
			continue
		}
		seen[d.Document.ID] = true
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	return out, nil
}

// Retrieve 实现 Retriever 接口，等价于 SearchWithOptions。
func (m *RAGManager) Retrieve(ctx context.Context, query string, topK int, opts SearchOptions) ([]ScoredDocument, error) {
	return m.SearchWithOptions(ctx, query, topK, opts)
}

// SetReranker 设置检索后重排器；传 nil 则关闭重排（回归为原始召回顺序）。
func (m *RAGManager) SetReranker(r Reranker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reranker = r
}
