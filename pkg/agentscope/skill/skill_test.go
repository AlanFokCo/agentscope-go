package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkillMD(t *testing.T, dir, name, desc, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// --- parseSKILLMD ---

func TestParseSKILLMD(t *testing.T) {
	data := []byte("---\nname: test\ndescription: A test skill\n---\n\nDo things step by step.")
	s, err := parseSKILLMD(data, "/some/dir", 1000.0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "test" {
		t.Errorf("expected name=test, got %s", s.Name)
	}
	if s.Description != "A test skill" {
		t.Errorf("expected description, got %s", s.Description)
	}
	if s.Markdown != "Do things step by step." {
		t.Errorf("unexpected body: %q", s.Markdown)
	}
	if s.Dir != "/some/dir" {
		t.Errorf("unexpected dir: %s", s.Dir)
	}
}

func TestParseSKILLMD_MissingFrontmatter(t *testing.T) {
	data := []byte("No frontmatter here.")
	_, err := parseSKILLMD(data, "/dir", 0)
	if err == nil {
		t.Error("expected error for missing frontmatter")
	}
}

func TestParseSKILLMD_MissingName(t *testing.T) {
	data := []byte("---\ndescription: only desc\n---\n\nbody")
	_, err := parseSKILLMD(data, "/dir", 0)
	if err == nil {
		t.Error("expected error for missing name")
	}
}

func TestParseSKILLMD_MissingDescription(t *testing.T) {
	data := []byte("---\nname: only_name\n---\n\nbody")
	_, err := parseSKILLMD(data, "/dir", 0)
	if err == nil {
		t.Error("expected error for missing description")
	}
}

// --- LocalSkillLoader ---

func TestLocalSkillLoader_SingleDir(t *testing.T) {
	dir := t.TempDir()
	writeSkillMD(t, dir, "coding", "Write code", "Use TDD always.")

	loader := NewLocalSkillLoader(dir, false)
	skills, err := loader.LoadSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "coding" {
		t.Errorf("expected name=coding, got %s", skills[0].Name)
	}
}

func TestLocalSkillLoader_ScanSubdirs(t *testing.T) {
	root := t.TempDir()
	writeSkillMD(t, filepath.Join(root, "skill_a"), "alpha", "First skill", "Alpha body.")
	writeSkillMD(t, filepath.Join(root, "skill_b"), "beta", "Second skill", "Beta body.")

	loader := NewLocalSkillLoader(root, true)
	skills, err := loader.LoadSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
}

func TestLocalSkillLoader_NoSkills(t *testing.T) {
	dir := t.TempDir()
	loader := NewLocalSkillLoader(dir, false)
	skills, err := loader.LoadSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}
}

func TestLocalSkillLoader_Caching(t *testing.T) {
	dir := t.TempDir()
	writeSkillMD(t, dir, "cached", "Cached skill", "Original body.")

	loader := NewLocalSkillLoader(dir, false)

	// First load
	skills1, _ := loader.LoadSkills()
	if len(skills1) != 1 {
		t.Fatal("first load failed")
	}

	// Second load should use cache
	skills2, _ := loader.LoadSkills()
	if len(skills2) != 1 || skills2[0].Name != "cached" {
		t.Error("cached load should return same skill")
	}
}

// --- FormatSkillInstructions ---

func TestFormatSkillInstructions(t *testing.T) {
	skills := []Skill{
		{Name: "coding", Description: "Write code"},
		{Name: "testing", Description: "Run tests"},
	}
	result := FormatSkillInstructions(skills)
	if !strings.Contains(result, "**coding**") {
		t.Error("should contain skill name")
	}
	if !strings.Contains(result, "Write code") {
		t.Error("should contain description")
	}
	if !strings.Contains(result, "**testing**") {
		t.Error("should contain second skill")
	}
}

func TestFormatSkillInstructions_Empty(t *testing.T) {
	result := FormatSkillInstructions(nil)
	if result != "" {
		t.Error("empty skills should produce empty string")
	}
}

// --- SkillViewerTool ---

func TestSkillViewerTool_Found(t *testing.T) {
	skills := []Skill{
		{Name: "coding", Markdown: "Step 1: Write tests\nStep 2: Write code"},
	}
	viewer := NewSkillViewerTool(skills)

	resp, err := viewer.Execute(context.Background(), map[string]any{"skill": "coding"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.State != "success" {
		t.Error("should succeed")
	}
	if len(resp.Content) == 0 {
		t.Fatal("response should have content")
	}
}

func TestSkillViewerTool_NotFound(t *testing.T) {
	viewer := NewSkillViewerTool(nil)

	resp, err := viewer.Execute(context.Background(), map[string]any{"skill": "nonexistent"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.State != "error" {
		t.Error("should return error for unknown skill")
	}
}

func TestSkillViewerTool_EmptyName(t *testing.T) {
	viewer := NewSkillViewerTool(nil)

	resp, _ := viewer.Execute(context.Background(), map[string]any{})
	if resp.State != "error" {
		t.Error("should return error for missing skill name")
	}
}

func TestSkillViewerTool_Name(t *testing.T) {
	viewer := NewSkillViewerTool(nil)
	if viewer.Name() != "Skill" {
		t.Errorf("expected tool name 'Skill', got %s", viewer.Name())
	}
}
