package rag

import (
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// dimMismatchSearchOnce 用于检索时“维度不一致”告警仅打印一次。
var dimMismatchSearchOnce sync.Once

type SearchOptions struct {
	SourceFiles    []string
	SourceQuery    string
	MaxPerSource   int
	MinScore       float64
	NeighborChunks int
	// Owner 非空时仅检索属于该 owner 的文档；文档 Metadata["owner"] 为空视为“公共文档”，所有用户可见。
	// 用于多用户隔离：普通用户只能检索到自己上传的文档 + 公共文档，杜绝跨用户泄露机密。
	Owner string
}

func (v *VectorStore) SearchHybridWithOptions(queryVector []float64, query string, topK int, opts SearchOptions) []ScoredDocument {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if len(v.documents) == 0 || topK <= 0 {
		return nil
	}

	terms := queryTerms(query)
	candidates := make([]ScoredDocument, 0, minInt(topK*4, len(v.documents)))
	for i := range v.documents {
		doc := v.documents[i]
		if !docMatchesSearchOptions(doc, opts) {
			continue
		}
		var vectorScore float64
		if len(queryVector) != len(v.vectors[i]) {
			// 维度不一致（例如本地兜底与远程混用、或换模型后旧索引）：
			// 绝不静默返回 0 被当作“无命中”，而是显式告警并退化为仅关键词检索。
			dimMismatchSearchOnce.Do(func() {
				log.Printf("WARN: 查询向量维度(%d)与索引向量维度(%d)不一致，跳过向量相似度，仅用关键词检索", len(queryVector), len(v.vectors[i]))
			})
			vectorScore = 0
		} else {
			vectorScore = CosineSimilarity(queryVector, v.vectors[i])
		}
		searchableText := strings.Join([]string{
			doc.ID,
			doc.Metadata["source"],
			doc.Metadata["title"],
			doc.Chunk,
		}, "\n")
		score := vectorScore + weightedLexicalScore(query, terms, doc, searchableText)
		if opts.MinScore > 0 && score < opts.MinScore {
			continue
		}
		candidates = append(candidates, ScoredDocument{
			Document:  doc,
			Score:     score,
			MatchType: "hit",
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	return expandNeighborChunks(v.documents, limitBySource(candidates, topK, opts.MaxPerSource), opts.NeighborChunks, query)
}

func docMatchesSearchOptions(doc Document, opts SearchOptions) bool {
	source := docSourceName(doc)
	sourceBase := pathBase(source)
	if len(opts.SourceFiles) > 0 {
		matched := false
		for _, wanted := range opts.SourceFiles {
			wanted = strings.TrimSpace(wanted)
			if wanted == "" {
				continue
			}
			if strings.EqualFold(source, wanted) || strings.EqualFold(sourceBase, wanted) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if opts.SourceQuery != "" {
		query := strings.ToLower(strings.TrimSpace(opts.SourceQuery))
		if query != "" &&
			!strings.Contains(strings.ToLower(source), query) &&
			!strings.Contains(strings.ToLower(sourceBase), query) &&
			!strings.Contains(strings.ToLower(doc.ID), query) {
			return false
		}
	}
	if opts.Owner != "" {
		docOwner := doc.Metadata["owner"]
		// 仅命中“自己的文档”或“公共文档（owner 为空）”，其余用户的数据一律不可见。
		if docOwner != opts.Owner && docOwner != "" {
			return false
		}
	}
	return true
}

func limitBySource(candidates []ScoredDocument, topK, maxPerSource int) []ScoredDocument {
	results := make([]ScoredDocument, 0, minInt(topK, len(candidates)))
	seen := make(map[string]int)
	for _, candidate := range candidates {
		source := docSourceName(candidate.Document)
		if maxPerSource > 0 && seen[source] >= maxPerSource {
			continue
		}
		results = append(results, candidate)
		seen[source]++
		if len(results) >= topK {
			break
		}
	}
	return results
}

func docSourceName(doc Document) string {
	source := doc.Metadata["source"]
	if source == "" || source == "upload" {
		source = baseDocumentID(doc.ID)
	}
	return source
}

func pathBase(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	idx := strings.LastIndex(path, "/")
	if idx >= 0 && idx < len(path)-1 {
		return path[idx+1:]
	}
	return path
}

func weightedLexicalScore(query string, terms []string, doc Document, text string) float64 {
	if text == "" {
		return 0
	}
	lowerText := strings.ToLower(text)
	sourceText := strings.ToLower(docSourceName(doc) + "\n" + pathBase(docSourceName(doc)) + "\n" + doc.ID)
	score := 0.0

	query = strings.TrimSpace(strings.ToLower(query))
	if query != "" && strings.Contains(lowerText, query) {
		score += 2.5
	}
	for _, phrase := range importantPhrases(query) {
		if strings.Contains(lowerText, phrase) {
			score += 1.2
		}
		if strings.Contains(sourceText, phrase) {
			score += 1.5
		}
	}
	for _, term := range terms {
		runeLen := len([]rune(term))
		if runeLen < 2 || isWeakTerm(term) {
			continue
		}
		weight := 0.12
		if runeLen >= 3 {
			weight = 0.28
		}
		if runeLen >= 4 {
			weight = 0.45
		}
		if strings.Contains(lowerText, term) {
			score += weight
		}
		if strings.Contains(sourceText, term) {
			score += weight * 1.3
		}
	}
	if score > 6 {
		score = 6
	}
	return score
}

func importantPhrases(query string) []string {
	fields := strings.FieldsFunc(query, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\t' || r == ' ' || r == ',' || r == '，' || r == '?' || r == '？'
	})
	phrases := make([]string, 0, len(fields))
	seen := make(map[string]bool)
	for _, field := range fields {
		field = strings.TrimSpace(strings.ToLower(field))
		if len([]rune(field)) < 3 || seen[field] || isWeakTerm(field) {
			continue
		}
		seen[field] = true
		phrases = append(phrases, field)
	}
	return phrases
}

func isWeakTerm(term string) bool {
	switch strings.TrimSpace(strings.ToLower(term)) {
	case "状态", "流转", "流程", "内容", "相关", "什么", "哪些", "怎么", "如何", "说明", "信息", "项目", "文档":
		return true
	default:
		return false
	}
}

func expandNeighborChunks(all []Document, results []ScoredDocument, neighborChunks int, query string) []ScoredDocument {
	if neighborChunks <= 0 || len(results) == 0 {
		return results
	}
	byKey := make(map[string]Document, len(all))
	for _, doc := range all {
		source, idx, ok := docSourceAndIndex(doc)
		if ok {
			byKey[source+"#"+strconv.Itoa(idx)] = doc
		}
	}
	expanded := make([]ScoredDocument, 0, len(results)*(neighborChunks*2+1))
	seen := make(map[string]bool)
	for _, result := range results {
		source, idx, ok := docSourceAndIndex(result.Document)
		if !ok {
			if !seen[result.Document.ID] {
				expanded = append(expanded, result)
				seen[result.Document.ID] = true
			}
			continue
		}
		for _, offset := range neighborOffsets(neighborChunks) {
			key := source + "#" + strconv.Itoa(idx+offset)
			doc, exists := byKey[key]
			if !exists || seen[doc.ID] {
				continue
			}
			if offset < 0 && !TextHasQueryFocus(doc.Chunk, query) {
				continue
			}
			score := result.Score
			matchType := "hit"
			parentID := ""
			if offset != 0 {
				score = result.Score * (0.35 / float64(absInt(offset)))
				matchType = "neighbor"
				parentID = result.Document.ID
			}
			expanded = append(expanded, ScoredDocument{
				Document:       doc,
				Score:          score,
				MatchType:      matchType,
				NeighborOffset: offset,
				ParentID:       parentID,
			})
			seen[doc.ID] = true
		}
	}
	return expanded
}

func neighborOffsets(neighborChunks int) []int {
	offsets := []int{0}
	for i := 1; i <= neighborChunks; i++ {
		offsets = append(offsets, i)
	}
	for i := 1; i <= neighborChunks; i++ {
		offsets = append(offsets, -i)
	}
	return offsets
}

func docSourceAndIndex(doc Document) (string, int, bool) {
	id := doc.ID
	idx := strings.LastIndex(id, "_")
	if idx <= 0 || idx == len(id)-1 {
		return docSourceName(doc), 0, false
	}
	n, err := strconv.Atoi(id[idx+1:])
	if err != nil {
		return docSourceName(doc), 0, false
	}
	source := docSourceName(doc)
	if source == "" {
		source = id[:idx]
	}
	return source, n, true
}

func ChunkIndex(doc Document) int {
	_, idx, ok := docSourceAndIndex(doc)
	if !ok {
		return -1
	}
	return idx
}

func SourceFileName(doc Document) string {
	return pathBase(docSourceName(doc))
}

func SourcePath(doc Document) string {
	return docSourceName(doc)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
