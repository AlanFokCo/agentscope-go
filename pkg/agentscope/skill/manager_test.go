package skill

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSkillManagerRegisterAndGet(t *testing.T) {
	m := NewSkillManager()
	s := &Skill{Name: "alpha", Description: "first"}
	m.Register(s)

	got, ok := m.Get("alpha")
	if !ok {
		t.Fatal("expected to find skill alpha")
	}
	if got != s {
		t.Errorf("expected same skill pointer back")
	}
	if m.Len() != 1 {
		t.Errorf("expected len 1, got %d", m.Len())
	}
}

func TestSkillManagerGetNotFound(t *testing.T) {
	m := NewSkillManager()
	if _, ok := m.Get("missing"); ok {
		t.Error("expected missing skill to not be found")
	}
}

func TestSkillManagerRegisterNilAndEmpty(t *testing.T) {
	m := NewSkillManager()
	m.Register(nil)
	m.Register(&Skill{Name: ""})
	if m.Len() != 0 {
		t.Errorf("expected len 0 after nil/empty registrations, got %d", m.Len())
	}
}

func TestSkillManagerList(t *testing.T) {
	m := NewSkillManager()
	m.Register(&Skill{Name: "gamma"})
	m.Register(&Skill{Name: "alpha"})
	m.Register(&Skill{Name: "beta"})

	list := m.List()
	want := []string{"alpha", "beta", "gamma"}
	if len(list) != len(want) {
		t.Fatalf("expected %d skills, got %d", len(want), len(list))
	}
	for i, name := range want {
		if list[i].Name != name {
			t.Errorf("expected list[%d]=%s, got %s", i, name, list[i].Name)
		}
	}
}

func TestSkillManagerListByCategory(t *testing.T) {
	m := NewSkillManager()
	m.Register(&Skill{Name: "b", Category: "dev"})
	m.Register(&Skill{Name: "a", Category: "dev"})
	m.Register(&Skill{Name: "c", Category: "ops"})

	dev := m.ListByCategory("dev")
	if len(dev) != 2 {
		t.Fatalf("expected 2 dev skills, got %d", len(dev))
	}
	if dev[0].Name != "a" || dev[1].Name != "b" {
		t.Errorf("expected sorted [a b], got [%s %s]", dev[0].Name, dev[1].Name)
	}

	ops := m.ListByCategory("ops")
	if len(ops) != 1 || ops[0].Name != "c" {
		t.Errorf("expected [c], got %v", ops)
	}

	if got := m.ListByCategory("none"); len(got) != 0 {
		t.Errorf("expected no skills for unknown category, got %d", len(got))
	}
}

func TestSkillManagerLoadFromDir(t *testing.T) {
	root := t.TempDir()
	writeSkillMD(t, filepath.Join(root, "one"), "one", "first skill", "body one")
	writeSkillMD(t, filepath.Join(root, "two"), "two", "second skill", "body two")

	m := NewSkillManager()
	if err := m.LoadFromDir(root); err != nil {
		t.Fatal(err)
	}
	if m.Len() != 2 {
		t.Fatalf("expected 2 skills loaded, got %d", m.Len())
	}
	if _, ok := m.Get("one"); !ok {
		t.Error("expected skill one to be loaded")
	}
	if _, ok := m.Get("two"); !ok {
		t.Error("expected skill two to be loaded")
	}
}

func TestSkillManagerFormatInstructions(t *testing.T) {
	m := NewSkillManager()
	m.Register(&Skill{Name: "alpha", Description: "does alpha things"})

	out := m.FormatInstructions()
	if !strings.Contains(out, "alpha") {
		t.Errorf("expected instructions to mention alpha, got %q", out)
	}
	if !strings.Contains(out, "does alpha things") {
		t.Errorf("expected instructions to mention description, got %q", out)
	}
}

func TestSkillManagerReplaceExisting(t *testing.T) {
	m := NewSkillManager()
	m.Register(&Skill{Name: "alpha", Description: "old"})
	m.Register(&Skill{Name: "alpha", Description: "new"})

	if m.Len() != 1 {
		t.Fatalf("expected len 1 after replace, got %d", m.Len())
	}
	got, _ := m.Get("alpha")
	if got.Description != "new" {
		t.Errorf("expected description=new, got %s", got.Description)
	}
}

func TestSkillManagerConcurrency(t *testing.T) {
	m := NewSkillManager()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := "skill" + string(rune('A'+n%26))
			m.Register(&Skill{Name: name, Category: "cat"})
			m.Get(name)
			m.List()
			m.ListByCategory("cat")
			m.Len()
			m.FormatInstructions()
		}(i)
	}
	wg.Wait()
}
