package rag

import (
	"fmt"
	"strings"
)

// ============================================================
// DocumentSplitter - 文档切片器
// ============================================================
// 把长文档切成小段，方便 embedding 和检索
// ============================================================

type SplitterConfig struct {
	ChunkSize    int // 每个切片的最大字符数
	ChunkOverlap int // 切片之间的重叠字符数（保持上下文连贯）
}

// DefaultSplitterConfig 默认配置
func DefaultSplitterConfig() SplitterConfig {
	return SplitterConfig{
		ChunkSize:    500, // 500 字符一个切片
		ChunkOverlap: 50,  // 重叠 50 字符
	}
}

// SplitDocument 把文档切成多个片段
// 输入：文档内容
// 输出：切片列表
func SplitDocument(content string, config SplitterConfig) []string {
	if len(content) == 0 {
		return nil
	}
	if config.ChunkSize <= 0 {
		config = DefaultSplitterConfig()
	}
	if config.ChunkOverlap < 0 {
		config.ChunkOverlap = 0
	}
	if config.ChunkOverlap >= config.ChunkSize {
		config.ChunkOverlap = config.ChunkSize / 10
	}

	// 如果内容比切片还短，直接返回
	if len(content) <= config.ChunkSize {
		return []string{content}
	}

	chunks := make([]string, 0)
	start := 0

	for start < len(content) {
		end := start + config.ChunkSize

		// 如果不是最后一片，尝试在句号、换行等位置切分
		if end < len(content) {
			// 在切片范围内找最后一个句号或换行
			breakPoint := findBreakPoint(content, start, end)
			if breakPoint > start {
				end = breakPoint
			}
		} else {
			end = len(content)
		}

		chunk := strings.TrimSpace(content[start:end])
		if len(chunk) > 0 {
			chunks = append(chunks, chunk)
		}

		// 下一片的起始位置（减去重叠部分）
		nextStart := end - config.ChunkOverlap
		if nextStart <= start {
			nextStart = end
		}
		start = nextStart
		if start < 0 {
			start = 0
		}
	}

	return chunks
}

// findBreakPoint 在范围内找一个合适的断点
func findBreakPoint(content string, start, end int) int {
	// 优先在换行处断开
	for i := end; i > start; i-- {
		if content[i-1] == '\n' {
			return i
		}
	}
	// 其次在句号处断开
	for i := end; i > start; i-- {
		r := rune(content[i-1])
		if r == '。' || r == '.' || r == '！' || r == '?' {
			return i
		}
	}
	// 再其次在逗号处断开
	for i := end; i > start; i-- {
		r := rune(content[i-1])
		if r == '，' || r == ',' || r == '；' {
			return i
		}
	}
	return end
}

// SplitDocuments 批量切片文档
func SplitDocuments(docs []Document, config SplitterConfig) ([]Document, []string) {
	allDocs := make([]Document, 0)
	allChunks := make([]string, 0)

	for _, doc := range docs {
		chunks := SplitDocument(doc.Chunk, config)
		for i, chunk := range chunks {
			newDoc := Document{
				ID:       fmt.Sprintf("%s_chunk_%d", doc.ID, i),
				Content:  doc.Content,
				Chunk:    chunk,
				Metadata: doc.Metadata,
			}
			allDocs = append(allDocs, newDoc)
			allChunks = append(allChunks, chunk)
		}
	}

	return allDocs, allChunks
}
