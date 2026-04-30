package monitor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func setupMockProcHints(t *testing.T, pid int, comm string, cmdline []string, env map[string]string) string {
	root := t.TempDir()
	oldRoot := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = oldRoot })

	procDir := filepath.Join(root, fmt.Sprintf("%d", pid))
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "comm"), []byte(comm+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// exe symlink
	if err := os.Symlink("/usr/bin/game", filepath.Join(procDir, "exe")); err != nil {
		t.Fatal(err)
	}
	// cwd symlink
	if err := os.Symlink("/home/user", filepath.Join(procDir, "cwd")); err != nil {
		t.Fatal(err)
	}

	// cmdline
	cmdlineData := ""
	for i, arg := range cmdline {
		if i > 0 {
			cmdlineData += "\x00"
		}
		cmdlineData += arg
	}
	if err := os.WriteFile(filepath.Join(procDir, "cmdline"), []byte(cmdlineData+"\x00"), 0644); err != nil {
		t.Fatal(err)
	}

	// environ
	envData := ""
	first := true
	for k, v := range env {
		if !first {
			envData += "\x00"
		}
		envData += k + "=" + v
		first = false
	}
	if err := os.WriteFile(filepath.Join(procDir, "environ"), []byte(envData+"\x00"), 0644); err != nil {
		t.Fatal(err)
	}

	return root
}

func TestReadProcHints_Basic(t *testing.T) {
	setupMockProcHints(t, 1234, "game", []string{"/usr/bin/game", "--level", "1"}, map[string]string{
		"SteamAppId": "620",
		"PATH":       "/usr/bin",
	})

	exePath, cwd, args, steamAppID, desktopID := readProcHints(1234, "game")
	if exePath != "/usr/bin/game" {
		t.Errorf("exePath=%q, want /usr/bin/game", exePath)
	}
	if cwd != "/home/user" {
		t.Errorf("cwd=%q, want /home/user", cwd)
	}
	if len(args) != 3 || args[0] != "/usr/bin/game" || args[1] != "--level" || args[2] != "1" {
		t.Errorf("args=%v, want [/usr/bin/game --level 1]", args)
	}
	if steamAppID != "620" {
		t.Errorf("steamAppID=%q, want 620", steamAppID)
	}
	if desktopID != "" {
		t.Errorf("desktopID=%q, want empty", desktopID)
	}
}

func TestReadProcHints_SteamGameId(t *testing.T) {
	setupMockProcHints(t, 1235, "game", []string{"/usr/bin/game"}, map[string]string{
		"SteamGameId": "730",
	})

	_, _, _, steamAppID, _ := readProcHints(1235, "game")
	if steamAppID != "730" {
		t.Errorf("steamAppID=%q, want 730", steamAppID)
	}
}

func TestReadProcHints_CmdlineAppId(t *testing.T) {
	setupMockProcHints(t, 1236, "game", []string{"/usr/bin/game", "AppId=440"}, map[string]string{})

	_, _, _, steamAppID, _ := readProcHints(1236, "game")
	if steamAppID != "440" {
		t.Errorf("steamAppID=%q, want 440", steamAppID)
	}
}

func TestReadProcHints_CmdlineSteamRunGameId(t *testing.T) {
	setupMockProcHints(t, 1237, "game", []string{"/usr/bin/game", "steam://rungameid/570"}, map[string]string{})

	_, _, _, steamAppID, _ := readProcHints(1237, "game")
	if steamAppID != "570" {
		t.Errorf("steamAppID=%q, want 570", steamAppID)
	}
}

func TestReadProcHints_NoSteam(t *testing.T) {
	setupMockProcHints(t, 1238, "firefox", []string{"/usr/bin/firefox"}, map[string]string{
		"PATH": "/usr/bin",
	})

	_, _, _, steamAppID, _ := readProcHints(1238, "firefox")
	if steamAppID != "" {
		t.Errorf("steamAppID=%q, want empty", steamAppID)
	}
}

func TestReadProcHints_DesktopIDFromEnv(t *testing.T) {
	setupMockProcHints(t, 1240, "firefox", []string{"/usr/bin/firefox"}, map[string]string{
		"GIO_LAUNCHED_DESKTOP_FILE": "/usr/share/applications/firefox.desktop",
	})

	_, _, _, steamAppID, desktopID := readProcHints(1240, "firefox")
	if steamAppID != "" {
		t.Errorf("steamAppID=%q, want empty", steamAppID)
	}
	if desktopID != "firefox" {
		t.Errorf("desktopID=%q, want firefox", desktopID)
	}
}

func TestReadProcHints_EnvNotExposed(t *testing.T) {
	setupMockProcHints(t, 1239, "game", []string{"/usr/bin/game"}, map[string]string{
		"SECRET_KEY": "should-not-appear",
		"SteamAppId": "620",
		"HOME":       "/home/user",
	})

	_, _, _, steamAppID, _ := readProcHints(1239, "game")
	if steamAppID != "620" {
		t.Errorf("steamAppID=%q, want 620", steamAppID)
	}
	// We have no way to directly verify SECRET_KEY wasn't returned, but the
	// function signature only returns allowlisted values, so this is a design-time
	// guarantee. parseEnvironAllowlist is tested below.
}

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
