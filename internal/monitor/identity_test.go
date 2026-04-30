package monitor

import (
	"os"
	"path/filepath"
	"strings"
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
	// 12345 should NOT appear as a generic alias (it is a SteamAppID)
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
	if foundAppID {
		t.Error("SteamAppId should not be a generic alias")
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

func TestBasenameAnyOS(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"C:\\Games\\Lethal Company\\Lethal Company.exe", "Lethal Company.exe"},
		{"Z:\\home\\user\\Games\\Game.exe", "Game.exe"},
		{"/mnt/sata/game.exe", "game.exe"},
		{"game.exe", "game.exe"},
		{"C:\\Game.exe\\", "Game.exe"},
		{"", ""},
	}
	for _, tt := range tests {
		got := basenameAnyOS(tt.input)
		if got != tt.want {
			t.Errorf("basenameAnyOS(%q)=%q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStripQuotes(t *testing.T) {
	if got := stripQuotes(`"C:\Game.exe"`); got != `C:\Game.exe` {
		t.Errorf("stripQuotes double: got %q", got)
	}
	if got := stripQuotes(`'C:\Game.exe'`); got != `C:\Game.exe` {
		t.Errorf("stripQuotes single: got %q", got)
	}
	if got := stripQuotes(`C:\Game.exe`); got != `C:\Game.exe` {
		t.Errorf("stripQuotes none: got %q", got)
	}
}

func TestExtractAliases_WindowsPath(t *testing.T) {
	info := &ProcessInfo{
		Name: "wine",
		Args: []string{"wine", `C:\Games\Lethal Company\Lethal Company.exe`},
	}
	aliases := ExtractAliases(info)

	// Should contain only the basename, not the full path
	for _, a := range aliases {
		if strings.Contains(a, `\`) || strings.Contains(a, `/`) {
			t.Errorf("alias should not contain path separator: %q", a)
		}
	}

	foundExe := false
	foundStripped := false
	for _, a := range aliases {
		if a == "Lethal Company.exe" {
			foundExe = true
		}
		if a == "Lethal Company" {
			foundStripped = true
		}
	}
	if !foundExe {
		t.Error("expected Lethal Company.exe alias")
	}
	if !foundStripped {
		t.Error("expected Lethal Company stripped alias")
	}
}

func TestResolveProcessIdentities_SanitizesWindowsCmdlineName(t *testing.T) {
	root := setupMockProc(t)

	gameDir := createMockProcDir(t, root, 6100, "wine-preloader", "PPid:\t1\nPgid:\t6100\nSid:\t6100\n")
	_ = os.WriteFile(filepath.Join(gameDir, "cmdline"), []byte(`C:\Users\alice\Games\Lethal Company\Lethal Company.exe`+"\x00"), 0644)

	procs := ResolveProcessIdentities()
	var gameProc *profile.DetectedProcess
	for i := range procs {
		if procs[i].PID == 6100 {
			gameProc = &procs[i]
			break
		}
	}
	if gameProc == nil {
		t.Fatal("expected mocked process")
	}
	if gameProc.Name != "Lethal Company.exe" {
		t.Fatalf("expected sanitized basename, got %q", gameProc.Name)
	}
	for _, a := range gameProc.Aliases {
		if strings.Contains(a, `\\`) || strings.Contains(a, `/`) {
			t.Fatalf("alias should not expose full path: %q", a)
		}
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
	// Numeric SteamAppIDs should not appear as generic aliases
	for _, a := range protonProc.Aliases {
		if a == "2267999134" {
			t.Error("numeric SteamAppID should not be a generic alias")
		}
	}
}

func TestResolveProcessIdentities_SteamAppIDFromArgs(t *testing.T) {
	root := setupMockProc(t)

	// Process with steam://rungameid/620 in cmdline
	gameDir := createMockProcDir(t, root, 4000, "game", "PPid:\t1\nPgid:\t4000\nSid:\t4000\n")
	_ = os.WriteFile(filepath.Join(gameDir, "cmdline"), []byte("game\x00steam://rungameid/620\x00"), 0644)

	procs := ResolveProcessIdentities()
	var gameProc *profile.DetectedProcess
	for i := range procs {
		if procs[i].Name == "game" {
			gameProc = &procs[i]
			break
		}
	}
	if gameProc == nil {
		t.Fatal("expected game process")
	}
	if gameProc.SteamAppID != "620" {
		t.Errorf("expected SteamAppID=620, got %q", gameProc.SteamAppID)
	}
}

func TestResolveProcessIdentities_SteamAppIDPropagation(t *testing.T) {
	root := setupMockProc(t)

	// Parent: wine (carrier)
	wineDir := createMockProcDir(t, root, 5000, "wine", "PPid:\t1\nPgid:\t5000\nSid:\t5000\n")
	_ = os.WriteFile(filepath.Join(wineDir, "cmdline"), []byte("wine\x00"), 0644)

	// Child: game with AppId=620 in args
	gameDir := createMockProcDir(t, root, 5001, "Game.exe", "PPid:\t5000\nPgid:\t5000\nSid:\t5000\n")
	_ = os.WriteFile(filepath.Join(gameDir, "cmdline"), []byte("Game.exe\x00AppId=620\x00"), 0644)

	procs := ResolveProcessIdentities()

	// Wine carrier should have SteamAppID propagated from child
	var wineProc *profile.DetectedProcess
	for i := range procs {
		if procs[i].Name == "wine" {
			wineProc = &procs[i]
			break
		}
	}
	if wineProc == nil {
		t.Fatal("expected wine process")
	}
	if wineProc.SteamAppID != "620" {
		t.Errorf("expected wine SteamAppID=620 propagated from child, got %q", wineProc.SteamAppID)
	}
}

func TestResolveProcessIdentities_ProtonCompatAppId(t *testing.T) {
	root := setupMockProc(t)

	// Proton process with SteamCompatAppId env
	protonDir := createMockProcDir(t, root, 6000, "proton", "PPid:\t1\nPgid:\t6000\nSid:\t6000\n")
	_ = os.WriteFile(filepath.Join(protonDir, "cmdline"), []byte("proton\x00"), 0644)
	env := "SteamCompatAppId=12345\x00PATH=/usr/bin\x00"
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
	if protonProc.SteamAppID != "12345" {
		t.Errorf("expected SteamAppID=12345 from SteamCompatAppId, got %q", protonProc.SteamAppID)
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

func TestResolveProcessIdentities_UnrelatedPgidPeerIgnored(t *testing.T) {
	root := setupMockProc(t)

	// wine in PGID 1000
	wineDir := createMockProcDir(t, root, 1000, "wine", "PPid:\t1\nPgid:\t1000\nSid:\t1000\n")
	_ = os.WriteFile(filepath.Join(wineDir, "cmdline"), []byte("wine\x00"), 0644)

	// firefox in same PGID but no shared Wine/Proton/Steam clue
	ffDir := createMockProcDir(t, root, 1001, "firefox", "PPid:\t1\nPgid:\t1000\nSid:\t1000\n")
	_ = os.WriteFile(filepath.Join(ffDir, "cmdline"), []byte("firefox\x00"), 0644)

	procs := ResolveProcessIdentities()

	var wineProc *profile.DetectedProcess
	for i := range procs {
		if procs[i].Name == "wine" {
			wineProc = &procs[i]
			break
		}
	}
	if wineProc == nil {
		t.Fatal("expected wine process")
	}
	// Wine should NOT have firefox alias because no clue is shared
	for _, a := range wineProc.Aliases {
		if strings.ToLower(a) == "firefox" {
			t.Errorf("wine should not inherit unrelated firefox alias, got %v", wineProc.Aliases)
		}
	}
}

func TestResolveProcessIdentities_PgidPeerWithSharedSteamAppID(t *testing.T) {
	root := setupMockProc(t)

	// pressure-vessel-wrap in PGID 2000
	pvDir := createMockProcDir(t, root, 2000, "pressure-vessel-wrap", "PPid:\t1\nPgid:\t2000\nSid:\t2000\n")
	_ = os.WriteFile(filepath.Join(pvDir, "cmdline"), []byte("pressure-vessel-wrap\x00"), 0644)
	env := "SteamAppId=620\x00PATH=/usr/bin\x00"
	_ = os.WriteFile(filepath.Join(pvDir, "environ"), []byte(env), 0644)

	// Game.exe in same PGID with same SteamAppId
	gameDir := createMockProcDir(t, root, 2001, "Game.exe", "PPid:\t1\nPgid:\t2000\nSid:\t2000\n")
	_ = os.WriteFile(filepath.Join(gameDir, "cmdline"), []byte("Game.exe\x00"), 0644)
	env2 := "SteamAppId=620\x00PATH=/usr/bin\x00"
	_ = os.WriteFile(filepath.Join(gameDir, "environ"), []byte(env2), 0644)

	procs := ResolveProcessIdentities()

	var pvProc *profile.DetectedProcess
	for i := range procs {
		if procs[i].Name == "pressure-vessel-wrap" {
			pvProc = &procs[i]
			break
		}
	}
	if pvProc == nil {
		t.Fatal("expected pressure-vessel-wrap process")
	}
	// Should have game alias because they share SteamAppID
	found := false
	for _, a := range pvProc.Aliases {
		if a == "Game.exe" || a == "Game" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected pressure-vessel-wrap to inherit Game alias via shared SteamAppID, got %v", pvProc.Aliases)
	}
}

func TestResolveProcessIdentities_PgidPeerWithSharedCompatPath(t *testing.T) {
	root := setupMockProc(t)

	// proton in PGID 3000
	protonDir := createMockProcDir(t, root, 3000, "proton", "PPid:\t1\nPgid:\t3000\nSid:\t3000\n")
	_ = os.WriteFile(filepath.Join(protonDir, "cmdline"), []byte("proton\x00"), 0644)
	env := "STEAM_COMPAT_DATA_PATH=/home/user/.steam/steam/steamapps/compatdata/730\x00PATH=/usr/bin\x00"
	_ = os.WriteFile(filepath.Join(protonDir, "environ"), []byte(env), 0644)

	// Game.exe in same PGID with same compat path
	gameDir := createMockProcDir(t, root, 3001, "cs2.exe", "PPid:\t1\nPgid:\t3000\nSid:\t3000\n")
	_ = os.WriteFile(filepath.Join(gameDir, "cmdline"), []byte("cs2.exe\x00"), 0644)
	env2 := "STEAM_COMPAT_DATA_PATH=/home/user/.steam/steam/steamapps/compatdata/730\x00PATH=/usr/bin\x00"
	_ = os.WriteFile(filepath.Join(gameDir, "environ"), []byte(env2), 0644)

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
	found := false
	for _, a := range protonProc.Aliases {
		if strings.ToLower(a) == "cs2.exe" || strings.ToLower(a) == "cs2" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected proton to inherit cs2 alias via shared compat path, got %v", protonProc.Aliases)
	}
}
