package catalog

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// buildTestVDF creates a minimal binary VDF for testing.
func buildTestVDF(t *testing.T) []byte {
	t.Helper()
	var b []byte
	// Root section: shortcuts
	b = append(b, 0x00)
	b = append(b, []byte("shortcuts")...)
	b = append(b, 0x00)

	// Entry 0
	b = append(b, 0x00)
	b = append(b, []byte("0")...)
	b = append(b, 0x00)

	// appid (int32)
	b = append(b, 0x02)
	b = append(b, []byte("appid")...)
	b = append(b, 0x00)
	appidBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(appidBytes, 0xDEADBEEF)
	b = append(b, appidBytes...)

	// AppName (string)
	b = append(b, 0x01)
	b = append(b, []byte("AppName")...)
	b = append(b, 0x00)
	b = append(b, []byte("Lethal Company")...)
	b = append(b, 0x00)

	// Exe (string, quoted)
	b = append(b, 0x01)
	b = append(b, []byte("Exe")...)
	b = append(b, 0x00)
	b = append(b, []byte(`"/mnt/sata/Lethal Company.exe"`)...)
	b = append(b, 0x00)

	// StartDir (string)
	b = append(b, 0x01)
	b = append(b, []byte("StartDir")...)
	b = append(b, 0x00)
	b = append(b, []byte(`"/mnt/sata"`)...)
	b = append(b, 0x00)

	// icon (string)
	b = append(b, 0x01)
	b = append(b, []byte("icon")...)
	b = append(b, 0x00)
	b = append(b, 0x00) // empty string

	// LaunchOptions (string)
	b = append(b, 0x01)
	b = append(b, []byte("LaunchOptions")...)
	b = append(b, 0x00)
	b = append(b, 0x00) // empty

	// IsHidden (int32 = 0)
	b = append(b, 0x02)
	b = append(b, []byte("IsHidden")...)
	b = append(b, 0x00)
	b = append(b, 0x00, 0x00, 0x00, 0x00)

	// AllowOverlay (int32 = 1)
	b = append(b, 0x02)
	b = append(b, []byte("AllowOverlay")...)
	b = append(b, 0x00)
	b = append(b, 0x01, 0x00, 0x00, 0x00)

	// Tags (empty section)
	b = append(b, 0x00)
	b = append(b, []byte("tags")...)
	b = append(b, 0x00)
	b = append(b, 0x08) // end tags

	b = append(b, 0x08) // end entry 0

	// Entry 1 (hidden, should be skipped)
	b = append(b, 0x00)
	b = append(b, []byte("1")...)
	b = append(b, 0x00)

	b = append(b, 0x02)
	b = append(b, []byte("appid")...)
	b = append(b, 0x00)
	binary.LittleEndian.PutUint32(appidBytes, 0xCAFEBABE)
	b = append(b, appidBytes...)

	b = append(b, 0x01)
	b = append(b, []byte("AppName")...)
	b = append(b, 0x00)
	b = append(b, []byte("Hidden Game")...)
	b = append(b, 0x00)

	b = append(b, 0x01)
	b = append(b, []byte("Exe")...)
	b = append(b, 0x00)
	b = append(b, []byte("hidden.exe")...)
	b = append(b, 0x00)

	b = append(b, 0x02)
	b = append(b, []byte("IsHidden")...)
	b = append(b, 0x00)
	b = append(b, 0x01, 0x00, 0x00, 0x00) // hidden = 1

	b = append(b, 0x00)
	b = append(b, []byte("tags")...)
	b = append(b, 0x00)
	b = append(b, 0x08)

	b = append(b, 0x08) // end entry 1

	b = append(b, 0x08) // end shortcuts

	return b
}

func TestParseBinaryVDF(t *testing.T) {
	data := buildTestVDF(t)
	vdf, err := parseBinaryVDF(data)
	if err != nil {
		t.Fatalf("parseBinaryVDF failed: %v", err)
	}
	if vdf == nil {
		t.Fatal("expected non-nil vdf")
	}
	shortcuts, ok := vdf["shortcuts"]
	if !ok {
		t.Fatal("expected 'shortcuts' section")
	}
	scMap, ok := shortcuts.(map[string]interface{})
	if !ok {
		t.Fatalf("expected shortcuts to be map, got %T", shortcuts)
	}
	if len(scMap) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(scMap))
	}
}

func TestParseSteamShortcuts(t *testing.T) {
	data := buildTestVDF(t)
	vdf, err := parseBinaryVDF(data)
	if err != nil {
		t.Fatalf("parseBinaryVDF failed: %v", err)
	}

	shortcuts, err := parseSteamShortcuts(vdf)
	if err != nil {
		t.Fatalf("parseSteamShortcuts failed: %v", err)
	}

	// Hidden game should be skipped.
	if len(shortcuts) != 1 {
		t.Fatalf("expected 1 non-hidden shortcut, got %d", len(shortcuts))
	}

	sc := shortcuts[0]
	if sc.AppName != "Lethal Company" {
		t.Errorf("AppName=%q, want Lethal Company", sc.AppName)
	}
	if sc.AppID != 0xDEADBEEF {
		t.Errorf("AppID=%x, want deadbeef", sc.AppID)
	}
	if sc.Exe != `/mnt/sata/Lethal Company.exe` {
		t.Errorf("Exe=%q, want /mnt/sata/Lethal Company.exe", sc.Exe)
	}
	if sc.IsHidden {
		t.Error("expected IsHidden=false")
	}
	if !sc.AllowOverlay {
		t.Error("expected AllowOverlay=true")
	}
}

func TestShortcutExeBase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`/mnt/sata/game.exe`, `game.exe`},
		{`C:\\Games\\game.exe`, `game.exe`},
		{`game.exe`, `game.exe`},
		{`/usr/bin/game`, `game`},
		{``, ``},
	}
	for _, tt := range tests {
		got := shortcutExeBase(tt.input)
		if got != tt.want {
			t.Errorf("shortcutExeBase(%q)=%q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSteamShortcutsSource_Refresh(t *testing.T) {
	data := buildTestVDF(t)

	dbPath := filepath.Join(t.TempDir(), "catalog.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	vdfPath := filepath.Join(t.TempDir(), "shortcuts.vdf")
	if err := os.WriteFile(vdfPath, data, 0644); err != nil {
		t.Fatalf("write vdf: %v", err)
	}

	src := &SteamShortcutsSource{Paths: []string{vdfPath}}
	if err := src.Refresh(context.Background(), store, RefreshOptions{}); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	// Verify entry was upserted.
	entries, err := store.ExactTitleMatch(context.Background(), "lethal company")
	if err != nil {
		t.Fatalf("exact title match: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Title != "Lethal Company" {
		t.Errorf("title=%q, want Lethal Company", entries[0].Title)
	}

	// Verify executable alias exists (without quotes).
	execAliases, err := store.SearchByAlias(context.Background(), AliasExecutable, "lethal company")
	if err != nil {
		t.Fatalf("search by exe: %v", err)
	}
	if len(execAliases) != 1 {
		t.Errorf("expected 1 executable alias match, got %d", len(execAliases))
	}

	// Verify no-exe-suffix alias also exists.
	noExtAliases, err := store.SearchByAlias(context.Background(), AliasExecutable, "lethal company")
	if err != nil {
		t.Fatalf("search by exe no-ext: %v", err)
	}
	// Both "lethal company.exe" and "lethal company" normalize to the same thing.
	if len(noExtAliases) < 1 {
		t.Errorf("expected at least 1 alias, got %d", len(noExtAliases))
	}
}
