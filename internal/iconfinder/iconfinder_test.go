package iconfinder

import (
	"os"
	"testing"
)

func TestResolveGIMP(t *testing.T) {
	path, err := Resolve("gimp")
	if err != nil {
		// GIMP might not be installed on CI.
		t.Skipf("GIMP icon not found (expected on dev machines): %v", err)
	}
	if path == "" {
		t.Fatal("resolved path is empty")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("resolved path %q does not exist: %v", path, err)
	}
	t.Logf("GIMP icon: %s", path)

	key := RegisterPath(path)
	if key == "" {
		t.Fatal("RegisterPath returned empty key")
	}
	lookup, ok := LookupPath(key)
	if !ok {
		t.Fatal("LookupPath did not find registered key")
	}
	if lookup != path {
		t.Errorf("LookupPath = %q, want %q", lookup, path)
	}
}

func TestResolveAbsolutePath(t *testing.T) {
	path, err := Resolve("/usr/share/icons/hicolor/128x128/apps/gimp.png")
	if err != nil {
		t.Skipf("gimp.png not found: %v", err)
	}
	if path != "/usr/share/icons/hicolor/128x128/apps/gimp.png" {
		t.Errorf("expected exact path, got %q", path)
	}
}

func TestResolveEmpty(t *testing.T) {
	_, err := Resolve("")
	if err == nil {
		t.Fatal("expected error for empty icon value")
	}
}

func TestResolveNonexistent(t *testing.T) {
	_, err := Resolve("this-icon-does-not-exist-xyz123")
	if err == nil {
		t.Fatal("expected error for nonexistent icon")
	}
}

func TestRegisterLookupNotFound(t *testing.T) {
	_, ok := LookupPath("nonexistent-hash-key")
	if ok {
		t.Fatal("expected false for unknown key")
	}
}
