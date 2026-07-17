package rag

import (
	"sort"
	"strings"
	"sync"
	"unicode"
)

// ============================================================
// VectorStore - 内存向量存储
// ============================================================
// 存储文档片段和对应的向量，支持相似度检索
// 生产环境应该用 Milvus、Pinecone 等专业向量数据库
// ============================================================

type Document struct {
	ID       string            // 文档唯一 ID
	Content  string            // 文档原始内容
	Chunk    string            // 切片后的内容
	Metadata map[string]string // 元数据（来源、页码等）
}

type ScoredDocument struct {
	Document       Document // 文档
	Score          float64  // 相似度分数
	MatchType      string   // hit 或 neighbor
	NeighborOffset int      // 相邻切片偏移，0 表示直接命中
	ParentID       string   // 相邻切片来自哪个直接命中切片
}

type VectorStore struct {
	documents []Document  // 存储的文档
	vectors   [][]float64 // 对应的向量
	maxDocs   int         // 最大驻留切片数，0 表示不限制
	mu        sync.RWMutex
}

// NewVectorStore 创建空的向量存储
func NewVectorStore(maxDocs int) *VectorStore {
	return &VectorStore{
		documents: make([]Document, 0),
		vectors:   make([][]float64, 0),
		maxDocs:   maxDocs,
	}
}

// Add 添加文档和对应向量
func (v *VectorStore) Add(doc Document, vector []float64) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.evictIfNeeded(1)
	v.documents = append(v.documents, doc)
	v.vectors = append(v.vectors, vector)
}

// AddBatch 批量添加文档和向量
func (v *VectorStore) AddBatch(docs []Document, vectors [][]float64) {
	v.mu.Lock()
	defer v.mu.Unlock()

	for i := range docs {
		v.evictIfNeeded(1)
		v.documents = append(v.documents, docs[i])
		v.vectors = append(v.vectors, vectors[i])
	}
}

func (v *VectorStore) evictIfNeeded(incoming int) {
	if v.maxDocs <= 0 {
		return
	}
	overflow := len(v.documents) + incoming - v.maxDocs
	if overflow <= 0 {
		return
	}
	if overflow >= len(v.documents) {
		v.documents = v.documents[:0]
		v.vectors = v.vectors[:0]
		return
	}
	copy(v.documents, v.documents[overflow:])
	copy(v.vectors, v.vectors[overflow:])
	v.documents = v.documents[:len(v.documents)-overflow]
	v.vectors = v.vectors[:len(v.vectors)-overflow]
}

// Search 搜索最相似的文档
// queryVector: 查询向量
// topK: 返回前 K 个结果
func (v *VectorStore) Search(queryVector []float64, topK int) []ScoredDocument {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if len(v.documents) == 0 || topK <= 0 {
		return nil
	}

	results := make([]ScoredDocument, 0, minInt(topK, len(v.documents)))
	for i := range v.documents {
		candidate := ScoredDocument{
			Document: v.documents[i],
			Score:    CosineSimilarity(queryVector, v.vectors[i]),
		}
		results = appendTopK(results, candidate, topK)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}

func (v *VectorStore) SearchHybrid(queryVector []float64, query string, topK int) []ScoredDocument {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if len(v.documents) == 0 || topK <= 0 {
		return nil
	}

	terms := queryTerms(query)
	results := make([]ScoredDocument, 0, minInt(topK, len(v.documents)))
	for i := range v.documents {
		vectorScore := CosineSimilarity(queryVector, v.vectors[i])
		searchableText := strings.Join([]string{
			v.documents[i].ID,
			v.documents[i].Metadata["source"],
			v.documents[i].Metadata["title"],
			v.documents[i].Chunk,
		}, "\n")
		textScore := lexicalScore(terms, searchableText)
		candidate := ScoredDocument{
			Document: v.documents[i],
			Score:    vectorScore + textScore,
		}
		results = appendTopK(results, candidate, topK)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}

// Count 返回文档数量
func (v *VectorStore) Count() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.documents)
}

// Clear 清空存储
func (v *VectorStore) Clear() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.documents = make([]Document, 0)
	v.vectors = make([][]float64, 0)
}

// GetAllDocuments 获取所有文档
func (v *VectorStore) GetAllDocuments() []Document {
	v.mu.RLock()
	defer v.mu.RUnlock()
	result := make([]Document, len(v.documents))
	copy(result, v.documents)
	return result
}

// SearchWithFilter 带过滤条件的搜索
func (v *VectorStore) SearchWithFilter(queryVector []float64, topK int, filter map[string]string) []ScoredDocument {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if len(v.documents) == 0 || topK <= 0 {
		return nil
	}

	results := make([]ScoredDocument, 0, minInt(topK, len(v.documents)))
	for i := range v.documents {
		if !matchFilter(v.documents[i].Metadata, filter) {
			continue
		}

		candidate := ScoredDocument{
			Document: v.documents[i],
			Score:    CosineSimilarity(queryVector, v.vectors[i]),
		}
		results = appendTopK(results, candidate, topK)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}

// matchFilter 检查文档是否匹配过滤条件
func matchFilter(metadata, filter map[string]string) bool {
	if len(filter) == 0 {
		return true
	}
	for key, value := range filter {
		if metadata[key] != value {
			return false
		}
	}
	return true
}

func appendTopK(results []ScoredDocument, candidate ScoredDocument, topK int) []ScoredDocument {
	if len(results) < topK {
		return append(results, candidate)
	}
	minIdx := 0
	for i := 1; i < len(results); i++ {
		if results[i].Score < results[minIdx].Score {
			minIdx = i
		}
	}
	if candidate.Score > results[minIdx].Score {
		results[minIdx] = candidate
	}
	return results
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func queryTerms(query string) []string {
	fields := strings.FieldsFunc(query, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	seen := make(map[string]bool)
	terms := make([]string, 0, len(fields))
	addTerm := func(term string) {
		term = strings.TrimSpace(strings.ToLower(term))
		if len([]rune(term)) < 2 || seen[term] {
			return
		}
		seen[term] = true
		terms = append(terms, term)
	}
	for _, field := range fields {
		addTerm(field)
		runes := []rune(strings.TrimSpace(field))
		if len(runes) < 4 {
			continue
		}
		for size := 2; size <= 6 && size <= len(runes); size++ {
			for start := 0; start+size <= len(runes); start++ {
				addTerm(string(runes[start : start+size]))
			}
		}
	}
	return terms
}

func lexicalScore(terms []string, text string) float64 {
	if len(terms) == 0 || text == "" {
		return 0
	}
	lowerText := strings.ToLower(text)
	score := 0.0
	for _, term := range terms {
		if strings.Contains(lowerText, term) {
			score += 0.35
		}
	}
	if score > 3 {
		return 3
	}
	return score
}
