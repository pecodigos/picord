package iconfinder

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var iconExtensions = []string{".png", ".svg", ".xpm", ".ico"}

var (
	iconPaths sync.Map // hash → file path
)

// RegisterPath stores a file path and returns its hash key for the
// /assets/picord-icons/{hash} endpoint.
func RegisterPath(path string) string {
	sum := sha256.Sum256([]byte(path))
	key := hex.EncodeToString(sum[:])
	iconPaths.Store(key, path)
	return key
}

// LookupPath returns the file path registered under the given hash key.
func LookupPath(key string) (string, bool) {
	v, ok := iconPaths.Load(key)
	if !ok {
		return "", false
	}
	return v.(string), true
}

// Resolve finds the actual file path for a desktop Icon field value.
// It handles absolute paths, icon names with extensions, and bare icon names
// by searching XDG icon theme directories.
func Resolve(iconValue string) (string, error) {
	if iconValue == "" {
		return "", fmt.Errorf("empty icon value")
	}

	// Absolute path — use directly.
	if strings.HasPrefix(iconValue, "/") {
		if _, err := os.Stat(iconValue); err == nil {
			return iconValue, nil
		}
		return "", fmt.Errorf("absolute icon path not found: %s", iconValue)
	}

	// Has an extension — search for exact filename.
	hasExt := filepath.Ext(iconValue) != ""
	searchDirs := searchDirectories()

	for _, dir := range searchDirs {
		if hasExt {
			candidate := filepath.Join(dir, iconValue)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		} else {
			for _, ext := range iconExtensions {
				candidate := filepath.Join(dir, iconValue+ext)
				if _, err := os.Stat(candidate); err == nil {
					return candidate, nil
				}
			}
		}
	}

	return "", fmt.Errorf("icon not found: %s", iconValue)
}

func searchDirectories() []string {
	var dirs []string

	home, err := os.UserHomeDir()
	if err == nil {
		dirs = append(dirs,
			filepath.Join(home, ".local", "share", "icons"),
			filepath.Join(home, ".icons"),
		)
	}

	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	for _, d := range strings.Split(dataDirs, ":") {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		dirs = append(dirs, filepath.Join(d, "icons"))
	}

	// Common pixel-art / pixmaps directory.
	dirs = append(dirs, "/usr/share/pixmaps")

	// Expand icon theme subdirectories: hicolor at common sizes.
	var expanded []string
	for _, iconDir := range dirs {
		for _, sub := range iconThemeSubdirs() {
			expanded = append(expanded, filepath.Join(iconDir, sub))
		}
	}
	return expanded
}

func iconThemeSubdirs() []string {
	return []string{
		"hicolor/256x256/apps",
		"hicolor/128x128/apps",
		"hicolor/96x96/apps",
		"hicolor/64x64/apps",
		"hicolor/48x48/apps",
		"hicolor/32x32/apps",
		"hicolor/scalable/apps",
		"hicolor/256x256/categories",
		"hicolor/128x128/categories",
		"hicolor/64x64/categories",
		"hicolor/48x48/categories",
	}
}
