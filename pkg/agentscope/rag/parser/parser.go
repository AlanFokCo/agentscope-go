package parser

import (
	"context"
	"io"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/rag"
)

// Parser converts a file into a slice of Documents suitable for indexing.
type Parser interface {
	Parse(ctx context.Context, r io.Reader, filename string) ([]rag.Document, error)
	SupportedExtensions() []string
}

// ChunkConfig controls text chunking behavior.
type ChunkConfig struct {
	MaxChunkSize int // max characters per chunk (default 1000)
	Overlap      int // overlap between consecutive chunks (default 200)
}

// DefaultChunkConfig returns sensible chunking defaults.
func DefaultChunkConfig() ChunkConfig {
	return ChunkConfig{
		MaxChunkSize: 1000,
		Overlap:      200,
	}
}

// ChunkText splits text into overlapping chunks according to cfg.
// If text is shorter than MaxChunkSize, a single chunk is returned.
func ChunkText(text string, cfg ChunkConfig) []string {
	if cfg.MaxChunkSize <= 0 {
		cfg.MaxChunkSize = 1000
	}
	if cfg.Overlap < 0 {
		cfg.Overlap = 0
	}
	if cfg.Overlap >= cfg.MaxChunkSize {
		cfg.Overlap = cfg.MaxChunkSize / 2
	}

	runes := []rune(text)
	total := len(runes)
	if total == 0 {
		return nil
	}
	if total <= cfg.MaxChunkSize {
		return []string{string(runes)}
	}

	var chunks []string
	step := cfg.MaxChunkSize - cfg.Overlap
	if step <= 0 {
		step = 1
	}

	for start := 0; start < total; start += step {
		end := start + cfg.MaxChunkSize
		if end > total {
			end = total
		}
		chunks = append(chunks, string(runes[start:end]))
		if end == total {
			break
		}
	}
	return chunks
}
