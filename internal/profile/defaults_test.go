package profile

import (
	"testing"
)

func TestDefaultProfiles_ReturnsNonEmpty(t *testing.T) {
	defaults := DefaultProfiles()
	if len(defaults) == 0 {
		t.Fatal("expected non-empty default profiles")
	}
}

func TestDefaultProfiles_AllEnabled(t *testing.T) {
	defaults := DefaultProfiles()
	for _, p := range defaults {
		if !p.Enabled {
			t.Errorf("expected default profile %q to be enabled", p.Name)
		}
	}
}

func TestDefaultProfiles_ContainsSteam(t *testing.T) {
	defaults := DefaultProfiles()
	var found bool
	for _, p := range defaults {
		if p.Name == "Steam" {
			found = true
			if !p.Enabled {
				t.Error("expected Steam profile to be enabled")
			}
			if p.Match.Value != "steam" {
				t.Errorf("expected Steam match value 'steam', got %q", p.Match.Value)
			}
		}
	}
	if !found {
		t.Error("expected Steam default profile to exist")
	}
}

func TestNewManager_DefaultsEnabled(t *testing.T) {
	defaults := DefaultProfiles()
	m := NewManager(nil, defaults)
	all := m.All()
	if len(all) != len(defaults) {
		t.Fatalf("expected %d profiles, got %d", len(defaults), len(all))
	}
	for _, p := range all {
		if !p.Enabled {
			t.Errorf("expected profile %q to be enabled", p.Name)
		}
		if !p.IsDefault() {
			t.Errorf("expected profile %q to be marked as default", p.Name)
		}
	}
}

func TestNewManager_UserDisableOverridesDefault(t *testing.T) {
	defaults := DefaultProfiles()
	m := NewManager([]Profile{
		{Name: "Steam", Enabled: false, Priority: 5, Match: MatchRule{Type: MatchProcessName, Value: "steam"}},
	}, defaults)

	p := m.Get("Steam")
	if p == nil {
		t.Fatal("expected Steam profile to exist")
	}
	if p.Enabled {
		t.Error("expected Steam to be disabled by user override")
	}
}

func TestNewManager_DefaultMatchWorks(t *testing.T) {
	defaults := DefaultProfiles()
	m := NewManager(nil, defaults)

	match, proc := m.Match([]DetectedProcess{{Name: "steam"}})
	if match == nil {
		t.Fatal("expected match for steam process")
	}
	if match.Name != "Steam" {
		t.Errorf("expected Steam, got %q", match.Name)
	}
	if proc == nil || proc.Name != "steam" {
		t.Errorf("expected proc steam, got %+v", proc)
	}
}
