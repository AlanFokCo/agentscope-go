package rag

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
	"github.com/sirupsen/logrus"
)

// QdrantTextIndex is a higher-level Index implementation that:
//   - uses an Embedder to embed document contents and text queries
//   - persists vectors and payloads into a Qdrant collection
//   - performs vector similarity search for text queries.
//
// It builds on top of QdrantIndex but hides the vector plumbing from callers.
type QdrantTextIndex struct {
	qdrant   *QdrantIndex
	embedder Embedder
}

// QdrantTextConfig configures a QdrantTextIndex.
type QdrantTextConfig struct {
	Client        *qdrant.Client
	Collection    string
	VectorMetaKey string
	Embedder      Embedder
}

// NewQdrantTextIndex constructs a QdrantTextIndex with an Embedder.
func NewQdrantTextIndex(cfg QdrantTextConfig) (*QdrantTextIndex, error) {
	if cfg.Embedder == nil {
		return nil, fmt.Errorf("qdrant: embedder is required for QdrantTextIndex")
	}

	base, err := NewQdrantIndex(QdrantConfig{
		Client:        cfg.Client,
		Collection:    cfg.Collection,
		VectorMetaKey: cfg.VectorMetaKey,
	})
	if err != nil {
		return nil, err
	}
	return &QdrantTextIndex{
		qdrant:   base,
		embedder: cfg.Embedder,
	}, nil
}

// AddDocuments embeds each document's Content and stores both the vector and payload in Qdrant.
func (i *QdrantTextIndex) AddDocuments(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}

	texts := make([]string, len(docs))
	for idx, d := range docs {
		texts[idx] = d.Content
	}

	vectors, err := i.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("qdrant: embed documents: %w", err)
	}
	if len(vectors) != len(docs) {
		return fmt.Errorf("qdrant: embedder returned %d vectors for %d docs", len(vectors), len(docs))
	}

	for idx := range docs {
		if docs[idx].Meta == nil {
			docs[idx].Meta = make(map[string]any)
		}
		docs[idx].Meta[i.qdrant.vectorMetaKey] = vectors[idx]
		// Also store the original content into payload for retrieval.
		docs[idx].Meta["content"] = docs[idx].Content
	}

	return i.qdrant.AddDocuments(ctx, docs)
}

// Query embeds the input text and performs a vector similarity search in Qdrant.
func (i *QdrantTextIndex) Query(ctx context.Context, query string, topK int) ([]Document, error) {
	if topK <= 0 {
		topK = 10
	}

	vecs, err := i.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("qdrant: embed query: %w", err)
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("qdrant: embedder returned no vectors for query")
	}
	vector := vecs[0]

	// Use Qdrant helper constructors to build a simple nearest-neighbor query.
	sp, err := i.qdrant.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: i.qdrant.collection,
		Query:          qdrant.NewQuery(vector...),
		Limit:          &[]uint64{uint64(topK)}[0],
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant: query points: %w", err)
	}

	out := make([]Document, 0, len(sp))
	for _, p := range sp {
		var id string
		if pid := p.GetId(); pid != nil {
			if u := pid.GetUuid(); u != "" {
				id = u
			}
		}

		meta := make(map[string]any, len(p.Payload))
		for k, v := range p.Payload {
			meta[k] = v
		}

		doc := Document{
			ID:   id,
			Meta: meta,
		}

		// Try to recover original content if present in payload.
		if raw, ok := p.Payload["content"]; ok && raw != nil {
			if sv := raw.GetStringValue(); sv != "" {
				doc.Content = sv
			}
		}

		out = append(out, doc)
	}

	logrus.WithFields(logrus.Fields{
		"collection": i.qdrant.collection,
		"topK":       topK,
		"returned":   len(out),
	}).Info("qdrant: query completed")

	return out, nil
}
