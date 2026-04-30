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

func TestDefaultProfiles_GamesEnabled(t *testing.T) {
	defaults := DefaultProfiles()
	for _, p := range defaults {
		if p.Name == "Factorio" || p.Name == "Stardew Valley" || p.Name == "Hollow Knight" {
			if !p.Enabled {
				t.Errorf("expected game profile %q to be enabled", p.Name)
			}
		}
	}
}

func TestDefaultProfiles_DesktopAppsDisabled(t *testing.T) {
	defaults := DefaultProfiles()
	for _, p := range defaults {
		if p.Name == "Firefox" || p.Name == "Discord" || p.Name == "VSCode" || p.Name == "Spotify" {
			if p.Enabled {
				t.Errorf("expected desktop app profile %q to be disabled", p.Name)
			}
		}
	}
}

func TestDefaultProfiles_SoftwareForGamesEnabled(t *testing.T) {
	defaults := DefaultProfiles()
	for _, p := range defaults {
		if p.Name == "Steam" || p.Name == "Dolphin Emulator" || p.Name == "PCSX2" || p.Name == "Wine" {
			if !p.Enabled {
				t.Errorf("expected software-for-games profile %q to be enabled", p.Name)
			}
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

func TestNewManager_DefaultsRespectEnabled(t *testing.T) {
	defaults := DefaultProfiles()
	m := NewManager(nil, defaults)
	all := m.All()
	if len(all) == 0 {
		t.Fatal("expected some active profiles")
	}
	for _, p := range all {
		if !p.IsDefault() {
			t.Errorf("expected profile %q to be marked as default", p.Name)
		}
	}
	// Ensure at least one desktop app is NOT in the active list.
	if m.Get("Firefox") != nil {
		t.Error("expected Firefox to be excluded from active defaults")
	}
	// Ensure Steam IS in the active list.
	if m.Get("Steam") == nil {
		t.Error("expected Steam to be included in active defaults")
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
