package rag

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
	"github.com/sirupsen/logrus"
)

// QdrantIndex is an Index implementation backed by a Qdrant collection.
// It assumes that embeddings are provided externally and passed in via
// the Document's Meta under a configurable key.
type QdrantIndex struct {
	client        *qdrant.Client
	collection    string
	vectorMetaKey string
}

// QdrantConfig configures a QdrantIndex.
type QdrantConfig struct {
	Client        *qdrant.Client
	Collection    string
	VectorMetaKey string // key in Document.Meta where []float32 vector is stored
}

// NewQdrantIndex constructs a Qdrant-backed Index.
func NewQdrantIndex(cfg QdrantConfig) (*QdrantIndex, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("qdrant: client is required")
	}
	if cfg.Collection == "" {
		return nil, fmt.Errorf("qdrant: collection is required")
	}
	key := cfg.VectorMetaKey
	if key == "" {
		key = "vector"
	}
	return &QdrantIndex{
		client:        cfg.Client,
		collection:    cfg.Collection,
		vectorMetaKey: key,
	}, nil
}

// AddDocuments upserts documents into the Qdrant collection.
// It expects a []float32 vector in doc.Meta[vectorMetaKey].
func (i *QdrantIndex) AddDocuments(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}

	points := make([]*qdrant.PointStruct, 0, len(docs))
	for _, d := range docs {
		rawVec, ok := d.Meta[i.vectorMetaKey]
		if !ok {
			return fmt.Errorf("qdrant: document %s is missing vector in meta[%s]", d.ID, i.vectorMetaKey)
		}
		vec, ok := rawVec.([]float32)
		if !ok {
			return fmt.Errorf("qdrant: document %s meta[%s] must be []float32", d.ID, i.vectorMetaKey)
		}

		payload := map[string]any{}
		// Copy Meta into payload so it can be retrieved later, including content if present.
		for k, v := range d.Meta {
			payload[k] = v
		}

		points = append(points, &qdrant.PointStruct{
			Id:      &qdrant.PointId{PointIdOptions: &qdrant.PointId_Uuid{Uuid: d.ID}},
			Vectors: &qdrant.Vectors{VectorsOptions: &qdrant.Vectors_Vector{Vector: &qdrant.Vector{Data: vec}}},
			Payload: qdrant.NewValueMap(payload),
		})
	}

	res, err := i.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: i.collection,
		Points:         points,
	})
	if err != nil {
		return fmt.Errorf("qdrant: upsert points: %w", err)
	}
	logrus.WithFields(logrus.Fields{
		"collection": i.collection,
		"count":      len(docs),
		"status":     res.GetStatus().String(),
	}).Info("qdrant: documents upserted")
	return nil
}

// Query performs a vector search using the query vector stored in meta[vectorMetaKey].
// It expects a []float32 vector in docs[0].Meta[vectorMetaKey]; other fields are ignored.
func (i *QdrantIndex) Query(ctx context.Context, query string, topK int) ([]Document, error) {
	_ = query // semantic search is driven by the vector, not raw text, in this implementation.
	if topK <= 0 {
		topK = 10
	}
	return nil, fmt.Errorf("qdrant: Query requires a vector-based implementation; use a custom wrapper that supplies a query vector")
}
