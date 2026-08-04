package hub

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// ---------- MCPHub Tests ----------

func TestMCPHub_List(t *testing.T) {
	expected := ListResult{
		Cards: []Card{
			{ID: "mcp-1", Kind: CardKindMCP, Name: "Weather", Version: "1.0.0"},
			{ID: "mcp-2", Kind: CardKindMCP, Name: "Calendar", Version: "2.1.0"},
		},
		NextCursor: "cursor-abc",
		HasMore:    true,
		Total:      42,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/mcp" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "10" {
			t.Errorf("expected limit=10, got %s", r.URL.Query().Get("limit"))
		}
		if r.URL.Query().Get("query") != "weather" {
			t.Errorf("expected query=weather, got %s", r.URL.Query().Get("query"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expected)
	}))
	defer srv.Close()

	hub := NewMCPHub(MCPHubConfig{
		BaseURL:     srv.URL,
		HubID:       "test-mcp",
		DisplayName: "Test MCP Hub",
	})
	defer hub.Close()

	result, err := hub.List(context.Background(), &ListOptions{
		Query: "weather",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(result.Cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(result.Cards))
	}
	if result.Cards[0].ID != "mcp-1" {
		t.Errorf("expected card ID mcp-1, got %s", result.Cards[0].ID)
	}
	if result.Cards[1].Name != "Calendar" {
		t.Errorf("expected card name Calendar, got %s", result.Cards[1].Name)
	}
	if result.NextCursor != "cursor-abc" {
		t.Errorf("expected next_cursor cursor-abc, got %s", result.NextCursor)
	}
	if !result.HasMore {
		t.Error("expected has_more=true")
	}
	if result.Total != 42 {
		t.Errorf("expected total=42, got %d", result.Total)
	}
}

func TestMCPHub_Get(t *testing.T) {
	expected := Card{
		ID:          "mcp-weather",
		Owner:       "alice",
		Kind:        CardKindMCP,
		Name:        "Weather MCP",
		Description: "Provides weather data",
		Version:     "1.2.3",
		Tags:        []string{"weather", "api"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/mcp/mcp-weather" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expected)
	}))
	defer srv.Close()

	hub := NewMCPHub(MCPHubConfig{
		BaseURL:     srv.URL,
		HubID:       "test-mcp",
		DisplayName: "Test MCP Hub",
	})
	defer hub.Close()

	card, err := hub.Get(context.Background(), "mcp-weather")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if card.ID != "mcp-weather" {
		t.Errorf("expected ID mcp-weather, got %s", card.ID)
	}
	if card.Owner != "alice" {
		t.Errorf("expected owner alice, got %s", card.Owner)
	}
	if card.Description != "Provides weather data" {
		t.Errorf("unexpected description: %s", card.Description)
	}
	if card.Version != "1.2.3" {
		t.Errorf("expected version 1.2.3, got %s", card.Version)
	}
	if len(card.Tags) != 2 || card.Tags[0] != "weather" {
		t.Errorf("unexpected tags: %v", card.Tags)
	}
}

func TestMCPHub_Install(t *testing.T) {
	configJSON := `{"name":"test-mcp","command":"npx","args":["mcp-server"]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/mcp/my-mcp/install" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(configJSON))
	}))
	defer srv.Close()

	hub := NewMCPHub(MCPHubConfig{
		BaseURL:     srv.URL,
		HubID:       "test-mcp",
		DisplayName: "Test MCP Hub",
	})
	defer hub.Close()

	targetDir := t.TempDir()

	err := hub.Install(context.Background(), "my-mcp", targetDir)
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	outPath := filepath.Join(targetDir, "my-mcp.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read installed file: %v", err)
	}
	if string(data) != configJSON {
		t.Errorf("expected %q, got %q", configJSON, string(data))
	}
}

// ---------- SkillHub Tests ----------

func TestSkillHub_List(t *testing.T) {
	expected := ListResult{
		Cards: []Card{
			{ID: "skill-code", Kind: CardKindSkill, Name: "Code Gen", Version: "0.5.0"},
		},
		NextCursor: "",
		HasMore:    false,
		Total:      1,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expected)
	}))
	defer srv.Close()

	hub := NewSkillHub(SkillHubConfig{
		BaseURL:     srv.URL,
		HubID:       "test-skill",
		DisplayName: "Test Skill Hub",
	})
	defer hub.Close()

	result, err := hub.List(context.Background(), &ListOptions{Limit: 20})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(result.Cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(result.Cards))
	}
	if result.Cards[0].Name != "Code Gen" {
		t.Errorf("expected name Code Gen, got %s", result.Cards[0].Name)
	}
	if result.HasMore {
		t.Error("expected has_more=false")
	}
}

func TestSkillHub_Install(t *testing.T) {
	// Create a tar.gz archive in memory with two files.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	type testFile struct {
		name    string
		content string
	}
	files := []testFile{
		{"SKILL.md", "# My Skill\nThis is a skill."},
		{"run.sh", "#!/bin/bash\necho hello"},
	}
	for _, f := range files {
		hdr := &tar.Header{
			Name: f.name,
			Mode: 0o644,
			Size: int64(len(f.content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write([]byte(f.content)); err != nil {
			t.Fatalf("write tar content: %v", err)
		}
	}
	tw.Close()
	gw.Close()

	archiveBytes := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills/my-skill/install" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(archiveBytes)
	}))
	defer srv.Close()

	hub := NewSkillHub(SkillHubConfig{
		BaseURL:     srv.URL,
		HubID:       "test-skill",
		DisplayName: "Test Skill Hub",
	})
	defer hub.Close()

	targetDir := t.TempDir()

	err := hub.Install(context.Background(), "my-skill", targetDir)
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify extracted files exist.
	for _, f := range files {
		path := filepath.Join(targetDir, "my-skill", f.name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read extracted file %s: %v", f.name, err)
		}
		if string(data) != f.content {
			t.Errorf("file %s: expected %q, got %q", f.name, f.content, string(data))
		}
	}
}

// ---------- Registry Tests ----------

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()

	hub := NewMCPHub(MCPHubConfig{BaseURL: "http://localhost", HubID: "hub-1", DisplayName: "Hub One"})

	if err := reg.Register(hub); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Duplicate registration should fail.
	if err := reg.Register(hub); err == nil {
		t.Error("expected error on duplicate registration")
	}

	got, ok := reg.Get("hub-1")
	if !ok {
		t.Fatal("expected to find hub-1")
	}
	if got.ID() != "hub-1" {
		t.Errorf("expected ID hub-1, got %s", got.ID())
	}
	if got.DisplayName() != "Hub One" {
		t.Errorf("expected display name Hub One, got %s", got.DisplayName())
	}
}

func TestRegistry_Unregister(t *testing.T) {
	reg := NewRegistry()

	hub := NewMCPHub(MCPHubConfig{HubID: "hub-x", DisplayName: "Hub X"})
	reg.Register(hub)

	if err := reg.Unregister("hub-x"); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	_, ok := reg.Get("hub-x")
	if ok {
		t.Error("expected hub-x to be removed")
	}

	// Unregistering non-existent hub should fail.
	if err := reg.Unregister("hub-x"); err == nil {
		t.Error("expected error on unregistering non-existent hub")
	}
}

func TestRegistry_List(t *testing.T) {
	reg := NewRegistry()

	hub1 := NewMCPHub(MCPHubConfig{HubID: "hub-a", DisplayName: "A"})
	hub2 := NewSkillHub(SkillHubConfig{HubID: "hub-b", DisplayName: "B"})

	reg.Register(hub1)
	reg.Register(hub2)

	hubs := reg.List()
	if len(hubs) != 2 {
		t.Fatalf("expected 2 hubs, got %d", len(hubs))
	}

	ids := map[string]bool{}
	for _, h := range hubs {
		ids[h.ID()] = true
	}
	if !ids["hub-a"] || !ids["hub-b"] {
		t.Errorf("missing expected hubs in list: %v", ids)
	}
}

func TestRegistry_SearchAll(t *testing.T) {
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ListResult{
			Cards: []Card{{ID: "result-from-hub1", Kind: CardKindMCP, Name: "Result 1"}},
			Total: 1,
		})
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ListResult{
			Cards: []Card{{ID: "result-from-hub2", Kind: CardKindSkill, Name: "Result 2"}},
			Total: 1,
		})
	}))
	defer srv2.Close()

	reg := NewRegistry()
	reg.Register(NewMCPHub(MCPHubConfig{BaseURL: srv1.URL, HubID: "hub-1", DisplayName: "Hub 1"}))
	reg.Register(NewSkillHub(SkillHubConfig{BaseURL: srv2.URL, HubID: "hub-2", DisplayName: "Hub 2"}))

	results, err := reg.SearchAll(context.Background(), &ListOptions{Query: "test"})
	if err != nil {
		t.Fatalf("SearchAll failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected results from 2 hubs, got %d", len(results))
	}

	r1, ok := results["hub-1"]
	if !ok {
		t.Fatal("missing results from hub-1")
	}
	if len(r1.Cards) != 1 || r1.Cards[0].ID != "result-from-hub1" {
		t.Errorf("unexpected hub-1 results: %+v", r1)
	}

	r2, ok := results["hub-2"]
	if !ok {
		t.Fatal("missing results from hub-2")
	}
	if len(r2.Cards) != 1 || r2.Cards[0].ID != "result-from-hub2" {
		t.Errorf("unexpected hub-2 results: %+v", r2)
	}
}
