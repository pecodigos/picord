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
		if p.Name == "Firefox" || p.Name == "Chromium" {
			if p.Enabled {
				t.Errorf("expected desktop app profile %q to be disabled", p.Name)
			}
		}
	}
}

func TestDefaultProfiles_SoftwareForGamesEnabled(t *testing.T) {
	defaults := DefaultProfiles()
	for _, p := range defaults {
		if p.Name == "Dolphin Emulator" || p.Name == "PCSX2" || p.Name == "Wine" || p.Name == "VSCode" {
			if !p.Enabled {
				t.Errorf("expected software profile %q to be enabled", p.Name)
			}
		}
	}
}

func TestDefaultProfiles_LaunchersRemoved(t *testing.T) {
	defaults := DefaultProfiles()
	removed := []string{"Steam", "Lutris", "Heroic Games Launcher", "Prism Launcher", "MultiMC"}
	for _, name := range removed {
		for _, p := range defaults {
			if p.Name == name {
				t.Errorf("expected launcher %q to be removed from defaults", name)
			}
		}
	}
}

func TestDefaultProfiles_SpotifyDiscordRemoved(t *testing.T) {
	defaults := DefaultProfiles()
	for _, p := range defaults {
		if p.Name == "Spotify" || p.Name == "Discord" {
			t.Errorf("expected %q to be removed from defaults (has native integration)", p.Name)
		}
	}
}

func TestDefaultProfiles_ContainsVSCode(t *testing.T) {
	defaults := DefaultProfiles()
	var found bool
	for _, p := range defaults {
		if p.Name == "VSCode" {
			found = true
			if !p.Enabled {
				t.Error("expected VSCode profile to be enabled")
			}
			if p.Match.Value != "code" {
				t.Errorf("expected VSCode match value 'code', got %q", p.Match.Value)
			}
		}
	}
	if !found {
		t.Error("expected VSCode default profile to exist")
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
	// Ensure desktop apps are NOT in the active list.
	if m.Get("Firefox") != nil {
		t.Error("expected Firefox to be excluded from active defaults")
	}
	if m.Get("Discord") != nil {
		t.Error("expected Discord to be excluded from active defaults")
	}
	if m.Get("Spotify") != nil {
		t.Error("expected Spotify to be excluded from active defaults")
	}
	// Ensure IDE IS in the active list.
	if m.Get("VSCode") == nil {
		t.Error("expected VSCode to be included in active defaults")
	}
	// Ensure emulator IS in the active list.
	if m.Get("Dolphin Emulator") == nil {
		t.Error("expected Dolphin Emulator to be included in active defaults")
	}
}

func TestNewManager_UserDisableOverridesDefault(t *testing.T) {
	defaults := DefaultProfiles()
	m := NewManager([]Profile{
		{Name: "VSCode", Enabled: false, Priority: 3, Match: MatchRule{Type: MatchProcessName, Value: "code"}},
	}, defaults)

	p := m.Get("VSCode")
	if p == nil {
		t.Fatal("expected VSCode profile to exist")
	}
	if p.Enabled {
		t.Error("expected VSCode to be disabled by user override")
	}
}

func TestNewManager_DefaultMatchWorks(t *testing.T) {
	defaults := DefaultProfiles()
	m := NewManager(nil, defaults)

	match, proc := m.Match([]DetectedProcess{{Name: "code"}})
	if match == nil {
		t.Fatal("expected match for code process")
	}
	if match.Name != "VSCode" {
		t.Errorf("expected VSCode, got %q", match.Name)
	}
	if proc == nil || proc.Name != "code" {
		t.Errorf("expected proc code, got %+v", proc)
	}
}
