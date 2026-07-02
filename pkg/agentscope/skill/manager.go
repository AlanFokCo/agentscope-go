package skill

import (
	"fmt"
	"sort"
	"sync"
)

// SkillManager is a concurrency-safe registry of loaded skills keyed by name.
type SkillManager struct {
	mu     sync.RWMutex
	skills map[string]*Skill
}

// NewSkillManager creates an empty SkillManager.
func NewSkillManager() *SkillManager {
	return &SkillManager{
		skills: make(map[string]*Skill),
	}
}

// Register adds a skill to the manager. Nil skills or skills with an empty
// name are ignored. An existing skill with the same name is replaced.
func (m *SkillManager) Register(s *Skill) {
	if s == nil || s.Name == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skills[s.Name] = s
}

// Get returns the skill with the given name, if present.
func (m *SkillManager) Get(name string) (*Skill, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.skills[name]
	return s, ok
}

// List returns all registered skills sorted by name.
func (m *SkillManager) List() []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Skill, 0, len(m.skills))
	for _, s := range m.skills {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// ListByCategory returns all registered skills in the given category, sorted by name.
func (m *SkillManager) ListByCategory(category string) []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Skill
	for _, s := range m.skills {
		if s.Category == category {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// LoadFromDir loads all skills found under dir (recursively) and registers them.
func (m *SkillManager) LoadFromDir(dir string) error {
	loader := NewLocalSkillLoader(dir, true)
	skills, err := loader.LoadSkills()
	if err != nil {
		return fmt.Errorf("skill: load from dir %s: %w", dir, err)
	}
	for i := range skills {
		s := skills[i]
		m.Register(&s)
	}
	return nil
}

// FormatInstructions renders the system prompt section listing registered skills.
func (m *SkillManager) FormatInstructions() string {
	list := m.List()
	skills := make([]Skill, 0, len(list))
	for _, s := range list {
		skills = append(skills, *s)
	}
	return FormatSkillInstructions(skills)
}

// Len returns the number of registered skills.
func (m *SkillManager) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.skills)
}
