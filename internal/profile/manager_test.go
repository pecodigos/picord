package profile

import (
	"testing"
)

func TestNewManager_MergesDefaultsAndUser(t *testing.T) {
	defaults := []Profile{
		{Name: "default1", Enabled: true, Priority: 5, Match: MatchRule{Type: MatchProcessName, Value: "a"}},
	}
	user := []Profile{
		{Name: "user1", Enabled: true, Priority: 10, Match: MatchRule{Type: MatchProcessName, Value: "b"}},
	}

	m := NewManager(user, defaults)
	all := m.All()

	if len(all) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(all))
	}

	if m.Get("default1") == nil {
		t.Error("expected default1 to exist")
	}
	if m.Get("user1") == nil {
		t.Error("expected user1 to exist")
	}
}

func TestManager_UserOverridesDefault(t *testing.T) {
	defaults := []Profile{
		{Name: "game", Enabled: true, Priority: 5, Match: MatchRule{Type: MatchProcessName, Value: "a"}},
	}
	user := []Profile{
		{Name: "game", Enabled: true, Priority: 20, Match: MatchRule{Type: MatchProcessName, Value: "b"}},
	}

	m := NewManager(user, defaults)
	p := m.Get("game")
	if p == nil {
		t.Fatal("expected game profile")
	}
	if p.Priority != 20 {
		t.Errorf("expected user priority 20, got %d", p.Priority)
	}
	if p.Match.Value != "b" {
		t.Errorf("expected user match 'b', got %q", p.Match.Value)
	}
}

func TestManager_Add(t *testing.T) {
	m := NewManager(nil, nil)
	m.Add(Profile{Name: "new", Enabled: true, Priority: 5, Match: MatchRule{Type: MatchProcessName, Value: "x"}})

	if m.Get("new") == nil {
		t.Error("expected new profile to be added")
	}
}

func TestManager_AddUpdatesExisting(t *testing.T) {
	m := NewManager(nil, nil)
	m.Add(Profile{Name: "same", Enabled: true, Priority: 1, Match: MatchRule{Type: MatchProcessName, Value: "a"}})
	m.Add(Profile{Name: "same", Enabled: true, Priority: 9, Match: MatchRule{Type: MatchProcessName, Value: "b"}})

	p := m.Get("same")
	if p == nil || p.Priority != 9 {
		t.Errorf("expected updated priority 9, got %v", p)
	}
}

func TestManager_AddPreservesEnabled(t *testing.T) {
	m := NewManager(nil, nil)
	m.Add(Profile{Name: "game", Enabled: true, Priority: 5, Match: MatchRule{Type: MatchProcessName, Value: "a"}})

	// Simulate an edit from the UI that doesn't include the Enabled field.
	m.Add(Profile{Name: "game", Enabled: false, Priority: 5, Match: MatchRule{Type: MatchProcessName, Value: "a"}})

	p := m.Get("game")
	if p == nil {
		t.Fatal("expected game profile")
	}
	if !p.Enabled {
		t.Error("expected Enabled to be preserved as true on edit")
	}
}

func TestManager_Delete(t *testing.T) {
	m := NewManager(nil, nil)
	m.Add(Profile{Name: "todelete", Enabled: true, Priority: 5, Match: MatchRule{Type: MatchProcessName, Value: "x"}})

	if !m.Delete("todelete") {
		t.Error("expected delete to return true")
	}
	if m.Get("todelete") != nil {
		t.Error("expected profile to be deleted")
	}
	if m.Delete("todelete") {
		t.Error("expected delete of missing profile to return false")
	}
}

func TestManager_SortByPriority(t *testing.T) {
	m := NewManager(nil, nil)
	m.Add(Profile{Name: "low", Enabled: true, Priority: 1, Match: MatchRule{Type: MatchProcessName, Value: "a"}})
	m.Add(Profile{Name: "high", Enabled: true, Priority: 10, Match: MatchRule{Type: MatchProcessName, Value: "b"}})
	m.Add(Profile{Name: "mid", Enabled: true, Priority: 5, Match: MatchRule{Type: MatchProcessName, Value: "c"}})

	all := m.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 profiles, got %d", len(all))
	}

	if all[0].Name != "high" || all[1].Name != "mid" || all[2].Name != "low" {
		t.Errorf("expected sorted order high/mid/low, got %s/%s/%s", all[0].Name, all[1].Name, all[2].Name)
	}
}

func TestManager_Match(t *testing.T) {
	m := NewManager(nil, nil)
	m.Add(Profile{Name: "game", Enabled: true, Priority: 5, Match: MatchRule{Type: MatchProcessName, Value: "doom"}})

	procs := []DetectedProcess{{Name: "doom"}}
	match, proc := m.Match(procs)
	if match == nil || match.Name != "game" {
		t.Errorf("expected match game, got %v", match)
	}
	if proc == nil || proc.Name != "doom" {
		t.Errorf("expected proc doom, got %v", proc)
	}
}

func TestManager_ReplaceUser(t *testing.T) {
	defaults := []Profile{
		{Name: "default", Enabled: true, Priority: 5, Match: MatchRule{Type: MatchProcessName, Value: "a"}, isDefault: true},
	}
	m := NewManager([]Profile{{Name: "olduser", Enabled: true, Priority: 1, Match: MatchRule{Type: MatchProcessName, Value: "b"}}}, defaults)

	m.ReplaceUser([]Profile{{Name: "newuser", Enabled: true, Priority: 2, Match: MatchRule{Type: MatchProcessName, Value: "c"}}})

	if m.Get("olduser") != nil {
		t.Error("expected olduser to be removed")
	}
	if m.Get("newuser") == nil {
		t.Error("expected newuser to exist")
	}
	if m.Get("default") == nil {
		t.Error("expected default to still exist")
	}
}

func TestManager_StableAfterSortAndDelete(t *testing.T) {
	m := NewManager(nil, nil)
	m.Add(Profile{Name: "a", Enabled: true, Priority: 1, Match: MatchRule{Type: MatchProcessName, Value: "a"}})
	m.Add(Profile{Name: "b", Enabled: true, Priority: 2, Match: MatchRule{Type: MatchProcessName, Value: "b"}})
	m.Add(Profile{Name: "c", Enabled: true, Priority: 3, Match: MatchRule{Type: MatchProcessName, Value: "c"}})

	// After adds and sorts, Get should return correct profiles.
	if p := m.Get("a"); p == nil || p.Name != "a" {
		t.Errorf("Get(a) wrong: %v", p)
	}
	if p := m.Get("b"); p == nil || p.Name != "b" {
		t.Errorf("Get(b) wrong: %v", p)
	}
	if p := m.Get("c"); p == nil || p.Name != "c" {
		t.Errorf("Get(c) wrong: %v", p)
	}

	// Delete middle element.
	if !m.Delete("b") {
		t.Fatal("expected delete b to succeed")
	}

	// After delete, remaining should be correct.
	if p := m.Get("a"); p == nil || p.Name != "a" {
		t.Errorf("after delete, Get(a) wrong: %v", p)
	}
	if p := m.Get("c"); p == nil || p.Name != "c" {
		t.Errorf("after delete, Get(c) wrong: %v", p)
	}
	if m.Get("b") != nil {
		t.Error("expected b to be deleted")
	}

	// Add another to trigger potential reallocation.
	m.Add(Profile{Name: "d", Enabled: true, Priority: 4, Match: MatchRule{Type: MatchProcessName, Value: "d"}})

	if p := m.Get("a"); p == nil || p.Name != "a" {
		t.Errorf("after realloc, Get(a) wrong: %v", p)
	}
	if p := m.Get("c"); p == nil || p.Name != "c" {
		t.Errorf("after realloc, Get(c) wrong: %v", p)
	}
	if p := m.Get("d"); p == nil || p.Name != "d" {
		t.Errorf("after realloc, Get(d) wrong: %v", p)
	}
}
