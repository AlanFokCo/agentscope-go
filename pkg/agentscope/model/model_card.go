package model

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ModelCardType indicates the category of a model card.
type ModelCardType string

const (
	ModelCardTypeChat      ModelCardType = "chat_model"
	ModelCardTypeEmbedding ModelCardType = "embedding_model"
	ModelCardTypeTTS       ModelCardType = "tts_model"
)

// ModelStatus indicates the lifecycle status of a model.
type ModelStatus string

const (
	ModelStatusActive     ModelStatus = "active"
	ModelStatusDeprecated ModelStatus = "deprecated"
	ModelStatusSunset     ModelStatus = "sunset"
)

// ModelCard describes a model's capabilities and constraints.
type ModelCard struct {
	Type         ModelCardType `yaml:"type"          json:"type"`
	Name         string        `yaml:"name"          json:"name"`
	Label        string        `yaml:"label"         json:"label"`
	Provider     string        `yaml:"-"             json:"provider"`
	Status       ModelStatus   `yaml:"status"        json:"status"`
	DeprecatedAt *time.Time    `yaml:"deprecated_at" json:"deprecated_at,omitempty"`
	InputTypes   []string      `yaml:"input_types"   json:"input_types"`
	OutputTypes  []string      `yaml:"output_types"  json:"output_types"`
	ContextSize  int           `yaml:"context_size"  json:"context_size"`
	OutputSize   int           `yaml:"output_size"   json:"output_size"`
}

// SupportsThinking returns true if the model can produce thinking/reasoning output.
func (c *ModelCard) SupportsThinking() bool {
	for _, t := range c.OutputTypes {
		if t == "application/x-thinking" {
			return true
		}
	}
	return false
}

// SupportsImages returns true if the model accepts image inputs.
func (c *ModelCard) SupportsImages() bool {
	for _, t := range c.InputTypes {
		if strings.HasPrefix(t, "image/") {
			return true
		}
	}
	return false
}

// SupportsAudio returns true if the model accepts audio inputs.
func (c *ModelCard) SupportsAudio() bool {
	for _, t := range c.InputTypes {
		if strings.HasPrefix(t, "audio/") {
			return true
		}
	}
	return false
}

// SupportsVideo returns true if the model accepts video inputs.
func (c *ModelCard) SupportsVideo() bool {
	for _, t := range c.InputTypes {
		if strings.HasPrefix(t, "video/") {
			return true
		}
	}
	return false
}

//go:embed models
var modelsFS embed.FS

// embeddedModelCards is the parsed, sorted set of all embedded model cards. The
// embedded FS is immutable at runtime, so it is parsed exactly once (previously
// every ListModels/GetModelCard call re-read and re-parsed ~50 YAML files, and
// GetModelCard sits on the reply hot path via context compression).
var (
	embeddedModelCardsOnce sync.Once
	embeddedModelCards     []ModelCard
)

func loadEmbeddedModelCards() []ModelCard {
	embeddedModelCardsOnce.Do(func() {
		entries, err := fs.ReadDir(modelsFS, "models")
		if err != nil {
			return
		}
		var cards []ModelCard
		for _, providerDir := range entries {
			if !providerDir.IsDir() {
				continue
			}
			provider := providerDir.Name()
			yamlFiles, err := fs.ReadDir(modelsFS, path.Join("models", provider))
			if err != nil {
				continue
			}
			for _, f := range yamlFiles {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".yaml") {
					continue
				}
				card, err := loadModelCard(modelsFS, path.Join("models", provider, f.Name()), provider)
				if err != nil {
					continue
				}
				cards = append(cards, *card)
			}
		}
		sort.Slice(cards, func(i, j int) bool {
			if cards[i].Provider != cards[j].Provider {
				return cards[i].Provider < cards[j].Provider
			}
			return cards[i].Name < cards[j].Name
		})
		embeddedModelCards = cards
	})
	return embeddedModelCards
}

// ListModels returns all embedded model cards, optionally filtered by provider.
// If no provider is specified, all models are returned.
// Models are sorted by provider then name. The returned slice is a fresh copy;
// callers may mutate it without affecting the shared cache.
func ListModels(providers ...string) []ModelCard {
	all := loadEmbeddedModelCards()

	providerSet := make(map[string]bool, len(providers))
	for _, p := range providers {
		providerSet[strings.ToLower(p)] = true
	}

	cards := make([]ModelCard, 0, len(all))
	for _, c := range all {
		if len(providerSet) > 0 && !providerSet[c.Provider] {
			continue
		}
		cards = append(cards, c)
	}
	return cards
}

// GetModelCard returns a specific model card by name.
// Returns an error if the model is not found. The returned pointer references a
// copy, so mutating it does not corrupt the shared cache.
func GetModelCard(name string) (*ModelCard, error) {
	for _, c := range loadEmbeddedModelCards() {
		if c.Name == name {
			cp := c
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("model card %q not found", name)
}

// LoadModelCardsFromFS loads model cards from an arbitrary fs.FS.
// The FS should contain YAML files directly (not nested in provider dirs).
// The provider string is assigned to all loaded cards.
func LoadModelCardsFromFS(fsys fs.FS, provider string) ([]ModelCard, error) {
	var cards []ModelCard

	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		card, err := loadModelCard(fsys, path, provider)
		if err != nil {
			return nil // skip bad files
		}
		cards = append(cards, *card)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(cards, func(i, j int) bool {
		return cards[i].Name < cards[j].Name
	})

	return cards, nil
}

func loadModelCard(fsys fs.FS, path string, provider string) (*ModelCard, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var card ModelCard
	if err := yaml.Unmarshal(data, &card); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if card.Name == "" {
		return nil, fmt.Errorf("model card %s missing name", path)
	}

	card.Provider = provider

	// Defaults
	if card.Type == "" {
		card.Type = ModelCardTypeChat
	}
	if card.Status == "" {
		card.Status = ModelStatusActive
	}
	if len(card.InputTypes) == 0 {
		card.InputTypes = []string{"text/plain"}
	}
	if len(card.OutputTypes) == 0 {
		card.OutputTypes = []string{"text/plain"}
	}

	return &card, nil
}
