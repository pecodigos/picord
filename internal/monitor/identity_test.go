package monitor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pecodigos/picord/internal/profile"
)

func TestIsCarrierProcess(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"wine", true},
		{"wine64", true},
		{"wineserver", true},
		{"wine-preloader", true},
		{"proton", true},
		{"pressure-vessel-wrap", true},
		{"explorer.exe", true},
		{"services.exe", true},
		{"steam.exe", true},
		{"steamwebhelper.exe", true},
		{"Lethal Company.exe", false},
		{"game", false},
		{"firefox", false},
	}
	for _, tt := range tests {
		if got := isCarrierProcess(tt.name); got != tt.want {
			t.Errorf("isCarrierProcess(%q)=%v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestIsNoisyProcess(t *testing.T) {
	if !isNoisyProcess("wine") {
		t.Error("expected wine to be noisy")
	}
	if isNoisyProcess("Lethal Company") {
		t.Error("expected Lethal Company to not be noisy")
	}
}

func TestExtractAliases(t *testing.T) {
	info := &ProcessInfo{
		Name:    "wine",
		ExePath: "/usr/bin/wine",
		Args:    []string{"wine", "/mnt/sata/game.exe", "-arg"},
		EnvHints: map[string]string{
			"SteamAppId": "12345",
		},
	}
	aliases := ExtractAliases(info)

	// wine and /usr/bin/wine should be excluded as noisy
	// game.exe should appear, plus "game" stripped
	// 12345 should appear
	foundGame := false
	foundGameStripped := false
	foundAppID := false
	for _, a := range aliases {
		if a == "game.exe" {
			foundGame = true
		}
		if a == "game" {
			foundGameStripped = true
		}
		if a == "12345" {
			foundAppID = true
		}
	}
	if !foundGame {
		t.Error("expected game.exe alias")
	}
	if !foundGameStripped {
		t.Error("expected stripped 'game' alias")
	}
	if !foundAppID {
		t.Error("expected SteamAppId alias")
	}
}

func TestExtractAliases_NoExe(t *testing.T) {
	info := &ProcessInfo{
		Name:    "firefox",
		ExePath: "/usr/bin/firefox",
		Args:    []string{"firefox"},
	}
	aliases := ExtractAliases(info)
	// Name and ExePath basename are both "firefox", so deduplication yields 1
	if len(aliases) != 1 || aliases[0] != "firefox" {
		t.Errorf("expected 1 alias 'firefox', got %d: %v", len(aliases), aliases)
	}
}

func TestResolveProcessIdentities_WineWithChild(t *testing.T) {
	root := setupMockProc(t)

	// Parent: wine
	wineDir := createMockProcDir(t, root, 1000, "wine", "PPid:\t1\nPgid:\t1000\nSid:\t1000\n")
	_ = os.WriteFile(filepath.Join(wineDir, "cmdline"), []byte("wine\x00"), 0644)

	// Child: Lethal Company.exe
	gameDir := createMockProcDir(t, root, 1001, "Lethal Company.exe", "PPid:\t1000\nPgid:\t1000\nSid:\t1000\n")
	_ = os.WriteFile(filepath.Join(gameDir, "cmdline"), []byte("Lethal Company.exe\x00"), 0644)

	// Need to also create fd symlinks for scanProcesses legacy check, but
	// ResolveProcessIdentities doesn't use that. We'll call it directly.
	procs := ResolveProcessIdentities()

	// Find the wine process
	var wineProc *profile.DetectedProcess
	for i := range procs {
		if procs[i].Name == "wine" {
			wineProc = &procs[i]
			break
		}
	}
	if wineProc == nil {
		t.Fatal("expected wine process in resolved identities")
	}

	// Wine should be enriched with the game alias
	found := false
	for _, a := range wineProc.Aliases {
		if a == "Lethal Company.exe" || a == "Lethal Company" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected wine process to have game alias, got %v", wineProc.Aliases)
	}

	// The actual game process should also exist
	var gameProc *profile.DetectedProcess
	for i := range procs {
		if procs[i].Name == "Lethal Company.exe" {
			gameProc = &procs[i]
			break
		}
	}
	if gameProc == nil {
		t.Fatal("expected game process in resolved identities")
	}
}

func TestResolveProcessIdentities_ProtonWithEnv(t *testing.T) {
	root := setupMockProc(t)

	// Proton process with SteamGameId env
	protonDir := createMockProcDir(t, root, 2000, "proton", "PPid:\t1\nPgid:\t2000\nSid:\t2000\n")
	_ = os.WriteFile(filepath.Join(protonDir, "cmdline"), []byte("proton\x00"), 0644)
	env := "SteamGameId=2267999134\x00PATH=/usr/bin\x00"
	_ = os.WriteFile(filepath.Join(protonDir, "environ"), []byte(env), 0644)

	procs := ResolveProcessIdentities()

	var protonProc *profile.DetectedProcess
	for i := range procs {
		if procs[i].Name == "proton" {
			protonProc = &procs[i]
			break
		}
	}
	if protonProc == nil {
		t.Fatal("expected proton process")
	}
	if protonProc.SteamAppID != "2267999134" {
		t.Errorf("expected SteamAppID=2267999134, got %q", protonProc.SteamAppID)
	}
	// Should also have the appid as an alias
	found := false
	for _, a := range protonProc.Aliases {
		if a == "2267999134" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected alias 2267999134, got %v", protonProc.Aliases)
	}
}

func TestResolveProcessIdentities_SkipsInternalHelpers(t *testing.T) {
	root := setupMockProc(t)

	// wineserver with no aliases or related processes
	wsDir := createMockProcDir(t, root, 3000, "wineserver", "PPid:\t1\nPgid:\t3000\nSid:\t3000\n")
	_ = os.WriteFile(filepath.Join(wsDir, "cmdline"), []byte("wineserver\x00"), 0644)

	procs := ResolveProcessIdentities()
	for _, p := range procs {
		if p.Name == "wineserver" {
			t.Error("expected wineserver to be skipped")
		}
	}
}
