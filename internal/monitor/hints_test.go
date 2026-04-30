package monitor

import (
	"testing"
)

func TestExtractSteamAppID_Args(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"AppId=", []string{"/usr/bin/game", "AppId=620"}, "620"},
		{"steam://rungameid/", []string{"/usr/bin/game", "steam://rungameid/730"}, "730"},
		{"steam://run/", []string{"/usr/bin/game", "steam://run/440"}, "440"},
		{"--appid next", []string{"/usr/bin/game", "--appid", "570"}, "570"},
		{"--appid non-numeric", []string{"/usr/bin/game", "--appid", "abc"}, ""},
		{"--appid signed plus ignored", []string{"/usr/bin/game", "--appid", "+570"}, ""},
		{"--appid signed minus ignored", []string{"/usr/bin/game", "--appid", "-570"}, ""},
		{"no match", []string{"/usr/bin/game", "--level", "1"}, ""},
		{"empty", []string{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSteamAppID(tt.args, nil)
			if got != tt.want {
				t.Errorf("ExtractSteamAppID(%v, nil)=%q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestExtractSteamAppID_Env(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"SteamAppId", map[string]string{"SteamAppId": "620"}, "620"},
		{"SteamGameId", map[string]string{"SteamGameId": "730"}, "730"},
		{"SteamAppID", map[string]string{"SteamAppID": "440"}, "440"},
		{"SteamOverlayGameId", map[string]string{"SteamOverlayGameId": "570"}, "570"},
		{"SteamCompatAppId", map[string]string{"SteamCompatAppId": "123"}, "123"},
		{"non-numeric ignored", map[string]string{"SteamAppId": "abc"}, ""},
		{"signed plus ignored", map[string]string{"SteamAppId": "+620"}, ""},
		{"signed minus ignored", map[string]string{"SteamAppId": "-620"}, ""},
		{"empty", map[string]string{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSteamAppID(nil, tt.env)
			if got != tt.want {
				t.Errorf("ExtractSteamAppID(nil, %v)=%q, want %q", tt.env, got, tt.want)
			}
		})
	}
}

func TestExtractSteamAppID_ArgsBeforeEnv(t *testing.T) {
	// Command-line Steam ID should take priority over env
	args := []string{"/usr/bin/game", "AppId=620"}
	env := map[string]string{"SteamAppId": "730"}
	got := ExtractSteamAppID(args, env)
	if got != "620" {
		t.Errorf("ExtractSteamAppID args priority: got %q, want 620", got)
	}
}

func TestParseEnvironAllowlist(t *testing.T) {
	data := "PATH=/usr/bin\x00HOME=/home/user\x00SteamAppId=620\x00SECRET=hidden\x00"
	allowed := []string{"SteamAppId", "PATH"}
	got := parseEnvironAllowlist([]byte(data), allowed)

	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got["SteamAppId"] != "620" {
		t.Errorf("SteamAppId=%q, want 620", got["SteamAppId"])
	}
	if got["PATH"] != "/usr/bin" {
		t.Errorf("PATH=%q, want /usr/bin", got["PATH"])
	}
	if _, ok := got["SECRET"]; ok {
		t.Error("SECRET should not be in allowlist result")
	}
	if _, ok := got["HOME"]; ok {
		t.Error("HOME should not be in allowlist result")
	}
}
