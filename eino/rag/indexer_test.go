package rag

import "testing"

func TestVectorStoreEvictsOldestWhenMaxDocsReached(t *testing.T) {
	store := NewVectorStore(2)
	store.Add(Document{ID: "a", Chunk: "a"}, []float64{1, 0})
	store.Add(Document{ID: "b", Chunk: "b"}, []float64{0, 1})
	store.Add(Document{ID: "c", Chunk: "c"}, []float64{1, 1})

	if got := store.Count(); got != 2 {
		t.Fatalf("expected 2 documents after eviction, got %d", got)
	}
	docs := store.GetAllDocuments()
	if docs[0].ID != "b" || docs[1].ID != "c" {
		t.Fatalf("expected oldest document to be evicted, got %#v", docs)
	}
}

func TestVectorStoreSearchReturnsOnlyTopK(t *testing.T) {
	store := NewVectorStore(0)
	store.Add(Document{ID: "low", Chunk: "low"}, []float64{0, 1})
	store.Add(Document{ID: "high", Chunk: "high"}, []float64{1, 0})

	results := store.Search([]float64{1, 0}, 1)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Document.ID != "high" {
		t.Fatalf("expected highest scoring document, got %s", results[0].Document.ID)
	}
}
