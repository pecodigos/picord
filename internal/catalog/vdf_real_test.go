package catalog

import (
	"os"
	"testing"
)

func TestParseRealShortcutsVDF(t *testing.T) {
	paths := SteamShortcutsPaths()
	if len(paths) == 0 {
		t.Skip("no Steam userdata found")
	}

	found := false
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Logf("read %s: %v", p, err)
			continue
		}
		found = true

		vdf, err := parseBinaryVDF(data)
		if err != nil {
			t.Fatalf("parseBinaryVDF %s: %v", p, err)
		}

		shortcuts, err := parseSteamShortcuts(vdf)
		if err != nil {
			t.Fatalf("parseSteamShortcuts %s: %v", p, err)
		}

		t.Logf("Found %d shortcuts in %s", len(shortcuts), p)
		for _, sc := range shortcuts {
			t.Logf("  - %s (appid=%d, exe=%s)", sc.AppName, sc.AppID, sc.Exe)
		}
	}
	if !found {
		t.Skip("no shortcuts.vdf files readable")
	}
}
