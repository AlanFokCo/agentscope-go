package rag

import (
	"context"
	"sort"
	"strings"
	"testing"
)

// substringReranker is a test reranker that scores documents by how many times
// the query appears in the document content (case-insensitive).
type substringReranker struct{}

func (r *substringReranker) Rerank(_ context.Context, query string, docs []Document, topN int) ([]ScoredDocument, error) {
	q := strings.ToLower(query)
	scored := make([]ScoredDocument, len(docs))
	for i, d := range docs {
		count := strings.Count(strings.ToLower(d.Content), q)
		scored[i] = ScoredDocument{Document: d, Score: float64(count)}
	}
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	if topN > 0 && topN < len(scored) {
		scored = scored[:topN]
	}
	return scored, nil
}

func TestRerankedIndex(t *testing.T) {
	ctx := context.Background()

	base := NewInMemoryIndex()
	_ = base.AddDocuments(ctx, []Document{
		{ID: "1", Content: "Go is a statically typed language"},
		{ID: "2", Content: "Go Go Go! Go is great for Go concurrency in Go"},
		{ID: "3", Content: "Python is dynamically typed"},
		{ID: "4", Content: "Go routines and Go channels"},
	})

	reranker := &substringReranker{}
	idx := NewRerankedIndex(base, reranker, 0) // default multiplier

	results, err := idx.Query(ctx, "Go", 2)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Doc "2" has 6 occurrences of "go", should be first
	if results[0].ID != "2" {
		t.Errorf("expected doc 2 first (most 'Go' occurrences), got %s", results[0].ID)
	}
}

func TestRerankerInterface(t *testing.T) {
	// Compile-time interface check
	var _ Reranker = (*substringReranker)(nil)
	var _ Index = (*RerankedIndex)(nil)
}
