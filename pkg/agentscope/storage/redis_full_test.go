package storage

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/agent"
)

// mockRedisClient is a pure in-memory implementation of RedisClient for testing.
type mockRedisClient struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMockRedisClient() *mockRedisClient {
	return &mockRedisClient{data: make(map[string][]byte)}
}

func (m *mockRedisClient) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = append([]byte(nil), value...)
	return nil
}

func (m *mockRedisClient) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return v, nil
}

func (m *mockRedisClient) Del(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *mockRedisClient) Scan(_ context.Context, pattern string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := strings.TrimSuffix(pattern, "*")
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *mockRedisClient) Close() error { return nil }

func newTestRedisFullStorage(t *testing.T) *RedisFullStorage {
	t.Helper()
	mock := newMockRedisClient()
	s, err := NewRedisFullStorage(RedisConfig{
		KeyPrefix: "test",
		Dial:      func(_ RedisConfig) (RedisClient, error) { return mock, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRedisFullStorage_StateSaver(t *testing.T) {
	ctx := context.Background()
	s := newTestRedisFullStorage(t)

	state := &agent.AgentState{SessionID: "s1", Summary: "hello"}
	if err := s.SaveState(ctx, "s1", state); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.LoadState(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Summary != "hello" {
		t.Errorf("expected 'hello', got %q", loaded.Summary)
	}

	sessions, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}

	if err := s.DeleteSession(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	_, err = s.LoadState(ctx, "s1")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestRedisFullStorage_Credentials(t *testing.T) {
	ctx := context.Background()
	s := newTestRedisFullStorage(t)

	r := &CredentialRecord{ID: "c1", UserID: "u1", Provider: "openai", Data: map[string]string{"key": "sk-..."}}
	if err := s.SaveCredential(ctx, r); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.LoadCredential(ctx, "u1", "c1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Provider != "openai" {
		t.Errorf("expected 'openai', got %q", loaded.Provider)
	}

	list, err := s.ListCredentials(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1, got %d", len(list))
	}

	if err := s.DeleteCredential(ctx, "u1", "c1"); err != nil {
		t.Fatal(err)
	}
	_, err = s.LoadCredential(ctx, "u1", "c1")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestRedisFullStorage_Agents(t *testing.T) {
	ctx := context.Background()
	s := newTestRedisFullStorage(t)

	r := &AgentRecord{ID: "a1", UserID: "u1", Name: "bot", SystemPrompt: "you are helpful"}
	if err := s.SaveAgent(ctx, r); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.LoadAgent(ctx, "u1", "a1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "bot" {
		t.Errorf("expected 'bot', got %q", loaded.Name)
	}

	list, err := s.ListAgents(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1, got %d", len(list))
	}

	if err := s.DeleteAgent(ctx, "u1", "a1"); err != nil {
		t.Fatal(err)
	}
}

func TestRedisFullStorage_Schedules(t *testing.T) {
	ctx := context.Background()
	s := newTestRedisFullStorage(t)

	r := &ScheduleRecord{ID: "sch1", UserID: "u1", Input: "daily report", Status: "active"}
	if err := s.SaveSchedule(ctx, r); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.LoadSchedule(ctx, "sch1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Input != "daily report" {
		t.Errorf("expected 'daily report', got %q", loaded.Input)
	}

	list, err := s.ListSchedules(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1, got %d", len(list))
	}

	all, err := s.ListAllSchedules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1, got %d", len(all))
	}

	if err := s.DeleteSchedule(ctx, "sch1"); err != nil {
		t.Fatal(err)
	}
}

func TestRedisFullStorage_Messages(t *testing.T) {
	ctx := context.Background()
	s := newTestRedisFullStorage(t)

	m1 := &MessageRecord{ID: "m1", SessionID: "s1", Role: "user", Content: "hello"}
	m2 := &MessageRecord{ID: "m2", SessionID: "s1", Role: "assistant", Content: "hi there"}

	if err := s.AppendMessage(ctx, m1); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, m2); err != nil {
		t.Fatal(err)
	}

	msgs, err := s.ListMessages(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}
	// Order preserved via index
	if msgs[0].ID != "m1" || msgs[1].ID != "m2" {
		t.Errorf("message order wrong: %s, %s", msgs[0].ID, msgs[1].ID)
	}

	loaded, err := s.LoadMessage(ctx, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Content != "hello" {
		t.Errorf("expected 'hello', got %q", loaded.Content)
	}
}

func TestRedisFullStorage_Teams(t *testing.T) {
	ctx := context.Background()
	s := newTestRedisFullStorage(t)

	r := &TeamRecord{ID: "t1", Name: "alpha", LeaderID: "a1", MemberIDs: []string{"a1", "a2"}}
	if err := s.SaveTeam(ctx, r); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.LoadTeam(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "alpha" {
		t.Errorf("expected 'alpha', got %q", loaded.Name)
	}

	list, err := s.ListTeams(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1, got %d", len(list))
	}

	if err := s.DeleteTeam(ctx, "t1"); err != nil {
		t.Fatal(err)
	}
}

func TestRedisFullStorage_Sessions(t *testing.T) {
	ctx := context.Background()
	s := newTestRedisFullStorage(t)

	r := &SessionRecord{ID: "sess1", AgentID: "a1", ScheduleID: "sch1"}
	if err := s.SaveSession(ctx, r); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.LoadSession(ctx, "sess1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AgentID != "a1" {
		t.Errorf("expected 'a1', got %q", loaded.AgentID)
	}

	if err := s.SetSessionTeamID(ctx, "sess1", "t1"); err != nil {
		t.Fatal(err)
	}
	updated, _ := s.LoadSession(ctx, "sess1")
	if updated.TeamID != "t1" {
		t.Errorf("expected team 't1', got %q", updated.TeamID)
	}

	bySchedule, err := s.ListSessionsBySchedule(ctx, "sch1")
	if err != nil {
		t.Fatal(err)
	}
	if len(bySchedule) != 1 {
		t.Errorf("expected 1, got %d", len(bySchedule))
	}
}
