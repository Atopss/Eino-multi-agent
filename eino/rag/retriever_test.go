package rag

import "testing"

func TestSplitDocumentMakesProgressWithLargeOverlap(t *testing.T) {
	content := "a\nbcdefghij"
	chunks := SplitDocument(content, SplitterConfig{
		ChunkSize:    5,
		ChunkOverlap: 4,
	})
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	if len(chunks) > len(content) {
		t.Fatalf("splitter likely failed to make progress, got %d chunks for %d bytes", len(chunks), len(content))
	}
}
