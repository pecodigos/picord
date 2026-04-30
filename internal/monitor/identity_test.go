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

func TestIsExcludedApp(t *testing.T) {
	excluded := []string{
		// Discord
		"discord", "discordcanary",
		// Browsers (exact names)
		"firefox", "firefox-bin", "firefox-esr", "firefox-nightly", "firefox-beta",
		"chrome", "chromium", "chromium-browser",
		"brave", "brave-browser",
		"opera", "opera-beta", "opera-developer",
		"microsoft-edge", "microsoft-edge-beta", "microsoft-edge-dev",
		"vivaldi", "vivaldi-stable",
		"librewolf", "waterfox", "floorp", "palemoon", "basilisk",
		"icecat", "seamonkey",
		"thorium", "thorium-browser",
		"ungoogled-chromium",
		"epiphany", "falkon", "midori", "qutebrowser",
		"konqueror", "rekonq", "otter-browser",
		"luakit", "surf", "nyxt", "lagrange", "badwolf",
		"netsurf", "netsurf-gtk3", "dooble",
		"tor-browser", "torbrowser",
		"zen", "zen-browser",
		// Flatpak browser IDs
		"org.mozilla.firefox", "org.mozilla.firefox_beta", "org.mozilla.firefox_nightly",
		"com.google.Chrome", "com.google.Chrome.beta", "com.google.Chrome.dev",
		"org.chromium.Chromium",
		"com.brave.Browser",
		"com.microsoft.Edge", "com.microsoft.Edge.dev", "com.microsoft.Edge.beta",
		"com.opera.Opera",
		"com.vivaldi.Vivaldi",
		"com.github.Eloston.UngoogledChromium",
		"org.gnome.Epiphany",
		"io.gitlab.pale_moon",
		"io.github.zen_browser.zen",
		// File managers
		"dolphin", "nautilus", "thunar", "nemo", "pcmanfm",
		"caja", "spacefm", "krusader", "doublecmd",
		// Desktop noise
		"plasmashell", "gnome-shell", "nm-applet",
		// Helpers
		"xdg-desktop-portal", "xdg-document-portal",
		"gtk-settings", "kde-config",
		// Launchers
		"steam", "steamlinux", "steam-runtime", "steamwebhelper",
		"epicgameslauncher", "heroic", "lutris", "gog-galaxy",
		"itch", "bottles", "playnite",
		// Terminal emulators
		"kitty", "alacritty", "wezterm", "foot", "gnome-terminal",
		"konsole", "xfce4-terminal", "lxterminal", "terminator",
		"tilix", "guake", "yakuake", "tilda", "qterminal",
		"st", "xterm", "urxvt", "rxvt", "eterm",
		"hyper", "tabby", "warp", "rio",
		// Shells
		"bash", "zsh", "fish", "sh", "dash", "csh", "tcsh",
	}
	for _, name := range excluded {
		if !isExcludedApp(name) {
			t.Errorf("expected %q to be excluded", name)
		}
	}

	included := []string{
		"blender", "gimp", "krita", "inkscape", "godot", "unity",
		"factorio", "celeste", "retroarch", "pcsx2", "code", "obs",
		"runelite", "osu!",
		// Non-browser flatpak IDs must NOT be excluded
		"org.libretro.RetroArch",
		"com.obsproject.Studio", "org.blender.Blender",
	}
	for _, name := range included {
		if isExcludedApp(name) {
			t.Errorf("expected %q to NOT be excluded", name)
		}
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

func TestResolveProcessIdentities_ShellAncestorBlocked(t *testing.T) {
	root := setupMockProc(t)

	// bash parent -> wine child (no game, no shared clue)
	bashDir := createMockProcDir(t, root, 7000, "bash", "PPid:\t1\nPgid:\t7000\nSid:\t7000\n")
	_ = os.WriteFile(filepath.Join(bashDir, "cmdline"), []byte("bash\x00"), 0644)

	wineDir := createMockProcDir(t, root, 7001, "wine", "PPid:\t7000\nPgid:\t7000\nSid:\t7000\n")
	_ = os.WriteFile(filepath.Join(wineDir, "cmdline"), []byte("wine\x00"), 0644)

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
	// Wine should NOT have bash alias because bash is a common shell
	for _, a := range wineProc.Aliases {
		if strings.ToLower(a) == "bash" {
			t.Errorf("wine should not inherit bash alias from shell ancestor, got %v", wineProc.Aliases)
		}
	}
}

func TestResolveProcessIdentities_IdentitySourcesTracked(t *testing.T) {
	root := setupMockProc(t)

	// Parent: wine (carrier)
	wineDir := createMockProcDir(t, root, 8000, "wine", "PPid:\t1\nPgid:\t8000\nSid:\t8000\n")
	_ = os.WriteFile(filepath.Join(wineDir, "cmdline"), []byte("wine\x00"), 0644)

	// Child: Game.exe with AppId=620
	gameDir := createMockProcDir(t, root, 8001, "Game.exe", "PPid:\t8000\nPgid:\t8000\nSid:\t8000\n")
	_ = os.WriteFile(filepath.Join(gameDir, "cmdline"), []byte("Game.exe\x00AppId=620\x00"), 0644)

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
	if len(wineProc.IdentitySources) == 0 {
		t.Fatal("expected identity sources for enriched wine process")
	}
	foundAlias := false
	foundSteam := false
	for _, src := range wineProc.IdentitySources {
		if src.Alias == "Game.exe" && src.Type == "descendant" {
			foundAlias = true
		}
		if src.SteamAppID == "620" && src.Type == "descendant" {
			foundSteam = true
		}
	}
	if !foundAlias {
		t.Errorf("expected identity source for Game.exe alias from descendant, got %+v", wineProc.IdentitySources)
	}
	if !foundSteam {
		t.Errorf("expected identity source for SteamAppID 620 from descendant, got %+v", wineProc.IdentitySources)
	}
}

func TestResolveProcessIdentitiesLite_PgidPeerEnrichment(t *testing.T) {
	root := setupMockProc(t)

	// PID 1000: pressure-vessel-wrap (carrier candidate) with SteamAppId=620
	pvDir := createMockProcDir(t, root, 1000, "pressure-vessel-wrap", "PPid:\t1\nPgid:\t1000\nSid:\t1000\n")
	_ = os.WriteFile(filepath.Join(pvDir, "cmdline"), []byte("pressure-vessel-wrap\x00"), 0644)
	env := "SteamAppId=620\x00PATH=/usr/bin\x00"
	_ = os.WriteFile(filepath.Join(pvDir, "environ"), []byte(env), 0644)

	// PID 1001: Game.exe in same PGID with same SteamAppId
	gameDir := createMockProcDir(t, root, 1001, "Game.exe", "PPid:\t1\nPgid:\t1000\nSid:\t1000\n")
	_ = os.WriteFile(filepath.Join(gameDir, "cmdline"), []byte("Game.exe\x00"), 0644)
	env2 := "SteamAppId=620\x00PATH=/usr/bin\x00"
	_ = os.WriteFile(filepath.Join(gameDir, "environ"), []byte(env2), 0644)

	procs := ResolveProcessIdentitiesLite([]int{1000})

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
	// Carrier should inherit Game.exe alias from same-PGID peer
	found := false
	for _, a := range pvProc.Aliases {
		if a == "Game.exe" || a == "Game" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected pressure-vessel-wrap to inherit Game alias from PGID peer, got %v", pvProc.Aliases)
	}
}

func TestResolveProcessIdentitiesLite_UnrelatedPgidPeerBlocked(t *testing.T) {
	root := setupMockProc(t)

	// PID 2000: wine (carrier candidate) with SteamAppId=620
	wineDir := createMockProcDir(t, root, 2000, "wine", "PPid:\t1\nPgid:\t2000\nSid:\t2000\n")
	_ = os.WriteFile(filepath.Join(wineDir, "cmdline"), []byte("wine\x00"), 0644)
	env := "SteamAppId=620\x00PATH=/usr/bin\x00"
	_ = os.WriteFile(filepath.Join(wineDir, "environ"), []byte(env), 0644)

	// PID 2001: firefox in same PGID, no shared clue
	ffDir := createMockProcDir(t, root, 2001, "firefox", "PPid:\t1\nPgid:\t2000\nSid:\t2000\n")
	_ = os.WriteFile(filepath.Join(ffDir, "cmdline"), []byte("firefox\x00"), 0644)

	procs := ResolveProcessIdentitiesLite([]int{2000})

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
	// Wine should NOT have firefox alias
	for _, a := range wineProc.Aliases {
		if strings.ToLower(a) == "firefox" {
			t.Errorf("wine should not inherit firefox alias from unrelated PGID peer, got %v", wineProc.Aliases)
		}
	}
}

func TestResolveProcessIdentities_ConflictingSteamAppIDs(t *testing.T) {
	root := setupMockProc(t)

	// Parent: proton with SteamAppID=111
	protonDir := createMockProcDir(t, root, 9000, "proton", "PPid:\t1\nPgid:\t9000\nSid:\t9000\n")
	_ = os.WriteFile(filepath.Join(protonDir, "cmdline"), []byte("proton\x00"), 0644)
	env := "SteamAppId=111\x00PATH=/usr/bin\x00"
	_ = os.WriteFile(filepath.Join(protonDir, "environ"), []byte(env), 0644)

	// Child 1: GameA with SteamAppID=222
	gameADir := createMockProcDir(t, root, 9001, "GameA.exe", "PPid:\t9000\nPgid:\t9000\nSid:\t9000\n")
	_ = os.WriteFile(filepath.Join(gameADir, "cmdline"), []byte("GameA.exe\x00AppId=222\x00"), 0644)

	// Child 2: GameB with SteamAppID=333 (impossible in reality but tests the code)
	gameBDir := createMockProcDir(t, root, 9002, "GameB.exe", "PPid:\t9000\nPgid:\t9000\nSid:\t9000\n")
	_ = os.WriteFile(filepath.Join(gameBDir, "cmdline"), []byte("GameB.exe\x00AppId=333\x00"), 0644)

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
	// Proton already has its own SteamAppID=111, so it should keep that
	// and not be overwritten by descendants.
	if protonProc.SteamAppID != "111" {
		t.Errorf("expected proton to keep its own SteamAppID=111, got %q", protonProc.SteamAppID)
	}
}

// TestEndToEnd_ProtonGameIdentity is a synthetic end-to-end fixture that
// exercises the full identity resolver path for a Proton-launched Steam game.
func TestEndToEnd_ProtonGameIdentity(t *testing.T) {
	root := setupMockProc(t)

	// PID 100: launcher (ancestor with SteamAppId env, simulates Steam/reaper)
	launcherDir := createMockProcDir(t, root, 100, "launcher", "PPid:\t1\nPgid:\t100\nSid:\t100\n")
	_ = os.WriteFile(filepath.Join(launcherDir, "cmdline"), []byte("launcher\x00"), 0644)
	envLauncher := "SteamAppId=620\x00PATH=/usr/bin\x00"
	_ = os.WriteFile(filepath.Join(launcherDir, "environ"), []byte(envLauncher), 0644)

	// PID 101: pressure-vessel-wrap (carrier)
	pvDir := createMockProcDir(t, root, 101, "pressure-vessel-wrap", "PPid:\t100\nPgid:\t101\nSid:\t100\n")
	_ = os.WriteFile(filepath.Join(pvDir, "cmdline"), []byte("pressure-vessel-wrap\x00"), 0644)
	envPV := "SteamAppId=620\x00STEAM_COMPAT_DATA_PATH=/home/user/.steam/steam/steamapps/compatdata/620\x00PATH=/usr/bin\x00"
	_ = os.WriteFile(filepath.Join(pvDir, "environ"), []byte(envPV), 0644)

	// PID 102: proton (carrier)
	protonDir := createMockProcDir(t, root, 102, "proton", "PPid:\t101\nPgid:\t101\nSid:\t100\n")
	_ = os.WriteFile(filepath.Join(protonDir, "cmdline"), []byte("proton\x00"), 0644)
	envProton := "SteamAppId=620\x00STEAM_COMPAT_DATA_PATH=/home/user/.steam/steam/steamapps/compatdata/620\x00PATH=/usr/bin\x00"
	_ = os.WriteFile(filepath.Join(protonDir, "environ"), []byte(envProton), 0644)

	// PID 103: wine (carrier)
	wineDir := createMockProcDir(t, root, 103, "wine", "PPid:\t102\nPgid:\t101\nSid:\t100\n")
	_ = os.WriteFile(filepath.Join(wineDir, "cmdline"), []byte("wine\x00"), 0644)

	// PID 104: Portal2.exe (actual game, descendant)
	gameDir := createMockProcDir(t, root, 104, "Portal2.exe", "PPid:\t103\nPgid:\t101\nSid:\t100\n")
	_ = os.WriteFile(filepath.Join(gameDir, "cmdline"), []byte("Portal2.exe\x00"), 0644)

	procs := ResolveProcessIdentities()

	// Build a name→process map for assertions
	byName := make(map[string]*profile.DetectedProcess)
	for i := range procs {
		byName[procs[i].Name] = &procs[i]
	}

	// 1. The actual game process should exist with its own identity.
	if gameProc := byName["Portal2.exe"]; gameProc == nil {
		t.Fatal("expected Portal2.exe process")
	} else {
		if len(gameProc.Aliases) == 0 {
			t.Error("expected Portal2.exe to have aliases")
		}
	}

	// 2. Wine carrier should be enriched with game aliases.
	wineProc := byName["wine"]
	if wineProc == nil {
		t.Fatal("expected wine process in resolved identities")
	}
	foundGameAlias := false
	for _, a := range wineProc.Aliases {
		if a == "Portal2.exe" || a == "Portal2" {
			foundGameAlias = true
			break
		}
	}
	if !foundGameAlias {
		t.Errorf("expected wine to inherit Portal2 alias from descendant, got %v", wineProc.Aliases)
	}

	// 3. Proton carrier should have SteamAppID from env (not overwritten by descendants).
	protonProc := byName["proton"]
	if protonProc == nil {
		t.Fatal("expected proton process")
	}
	if protonProc.SteamAppID != "620" {
		t.Errorf("expected proton SteamAppID=620, got %q", protonProc.SteamAppID)
	}

	// 4. Pressure-vessel should also have SteamAppID and game aliases.
	pvProc := byName["pressure-vessel-wrap"]
	if pvProc == nil {
		t.Fatal("expected pressure-vessel-wrap process")
	}
	if pvProc.SteamAppID != "620" {
		t.Errorf("expected pv SteamAppID=620, got %q", pvProc.SteamAppID)
	}
	foundPVAlias := false
	for _, a := range pvProc.Aliases {
		if a == "Portal2.exe" || a == "Portal2" {
			foundPVAlias = true
			break
		}
	}
	if !foundPVAlias {
		t.Errorf("expected pv to inherit Portal2 alias, got %v", pvProc.Aliases)
	}

	// 5. Launcher ancestor should exist (not excluded since it's "launcher").
	launcherProc := byName["launcher"]
	if launcherProc == nil {
		t.Fatal("expected launcher process")
	}
	if launcherProc.SteamAppID != "620" {
		t.Errorf("expected launcher SteamAppID=620, got %q", launcherProc.SteamAppID)
	}

	// 6. Identity sources should explain alias propagation on wine.
	if len(wineProc.IdentitySources) == 0 {
		t.Error("expected wine to have identity_sources")
	} else {
		foundSource := false
		for _, src := range wineProc.IdentitySources {
			if src.Alias == "Portal2.exe" && src.Type == "descendant" {
				foundSource = true
				break
			}
		}
		if !foundSource {
			t.Errorf("expected identity source for Portal2.exe from descendant, got %+v", wineProc.IdentitySources)
		}
	}

	// 7. No shell aliases should leak into carriers.
	for _, a := range wineProc.Aliases {
		lower := strings.ToLower(a)
		if lower == "bash" || lower == "zsh" || lower == "fish" {
			t.Errorf("wine should not have shell alias %q", a)
		}
	}
}

func TestResolveProcessIdentities_ExcludesDesktopApps(t *testing.T) {
	root := setupMockProc(t)

	// discord process
	discordDir := createMockProcDir(t, root, 1000, "discord", "PPid:\t1\nPgid:\t1000\nSid:\t1000\n")
	_ = os.WriteFile(filepath.Join(discordDir, "cmdline"), []byte("discord\x00"), 0644)

	// firefox process
	ffDir := createMockProcDir(t, root, 1001, "firefox", "PPid:\t1\nPgid:\t1001\nSid:\t1001\n")
	_ = os.WriteFile(filepath.Join(ffDir, "cmdline"), []byte("firefox\x00"), 0644)

	// blender process (should be included)
	blenderDir := createMockProcDir(t, root, 1002, "blender", "PPid:\t1\nPgid:\t1002\nSid:\t1002\n")
	_ = os.WriteFile(filepath.Join(blenderDir, "cmdline"), []byte("blender\x00"), 0644)

	procs := ResolveProcessIdentities()

	names := make(map[string]bool)
	for _, p := range procs {
		names[p.Name] = true
	}

	if names["discord"] {
		t.Error("expected discord to be excluded")
	}
	if names["firefox"] {
		t.Error("expected firefox to be excluded")
	}
	if !names["blender"] {
		t.Error("expected blender to be included")
	}
}
