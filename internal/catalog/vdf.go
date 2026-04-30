package catalog

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// parseBinaryVDF parses a Steam binary VDF file.
// It returns a nested map structure. Values can be string, int32, or map[string]interface{}.
func parseBinaryVDF(data []byte) (map[string]interface{}, error) {
	result, offset, err := parseVDFSection(data, 0)
	if err != nil {
		return nil, err
	}
	if offset != len(data) {
		// Trailing bytes are common in Steam files; ignore them.
	}
	return result, nil
}

func parseVDFSection(data []byte, offset int) (map[string]interface{}, int, error) {
	result := make(map[string]interface{})
	for offset < len(data) {
		if data[offset] == 0x08 {
			// End of section
			return result, offset + 1, nil
		}
		if offset >= len(data) {
			return nil, offset, fmt.Errorf("unexpected EOF in VDF section")
		}
		typ := data[offset]
		offset++
		key, newOffset, err := readVDFCString(data, offset)
		if err != nil {
			return nil, offset, fmt.Errorf("read key: %w", err)
		}
		offset = newOffset
		switch typ {
		case 0x00: // nested section
			child, newOffset, err := parseVDFSection(data, offset)
			if err != nil {
				return nil, offset, fmt.Errorf("parse section %q: %w", key, err)
			}
			result[key] = child
			offset = newOffset
		case 0x01: // string
			val, newOffset, err := readVDFCString(data, offset)
			if err != nil {
				return nil, offset, fmt.Errorf("read string value for %q: %w", key, err)
			}
			result[key] = val
			offset = newOffset
		case 0x02: // int32
			if offset+4 > len(data) {
				return nil, offset, fmt.Errorf("truncated int32 for %q", key)
			}
			val := int32(binary.LittleEndian.Uint32(data[offset : offset+4]))
			result[key] = val
			offset += 4
		default:
			return nil, offset, fmt.Errorf("unknown VDF type byte %x for key %q", typ, key)
		}
	}
	// If we hit EOF without an explicit section end, that's still valid for the root.
	return result, offset, nil
}

func readVDFCString(data []byte, offset int) (string, int, error) {
	start := offset
	for offset < len(data) && data[offset] != 0 {
		offset++
	}
	if offset >= len(data) {
		return "", offset, fmt.Errorf("unterminated string starting at %d", start)
	}
	return string(data[start:offset]), offset + 1, nil
}

// SteamShortcut represents a non-Steam game shortcut from shortcuts.vdf.
type SteamShortcut struct {
	AppID         uint32
	AppName       string
	Exe           string
	StartDir      string
	Icon          string
	ShortcutPath  string
	LaunchOptions string
	IsHidden      bool
	AllowOverlay  bool
	Tags          []string
}

// parseSteamShortcuts extracts non-Steam game shortcuts from parsed VDF data.
func parseSteamShortcuts(vdf map[string]interface{}) ([]SteamShortcut, error) {
	shortcutsRoot, ok := vdf["shortcuts"]
	if !ok {
		return nil, fmt.Errorf("no 'shortcuts' section in VDF")
	}
	rootMap, ok := shortcutsRoot.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("'shortcuts' is not a map")
	}

	var result []SteamShortcut
	for _, v := range rootMap {
		entry, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		sc := SteamShortcut{}

		if appidVal, ok := entry["appid"]; ok {
			switch av := appidVal.(type) {
			case int32:
				sc.AppID = uint32(av)
			case uint32:
				sc.AppID = av
			}
		}
		if nameVal, ok := entry["AppName"]; ok {
			if s, ok := nameVal.(string); ok {
				sc.AppName = s
			}
		}
		if exeVal, ok := entry["Exe"]; ok {
			if s, ok := exeVal.(string); ok {
				sc.Exe = strings.Trim(s, `"`)
			}
		}
		if startDirVal, ok := entry["StartDir"]; ok {
			if s, ok := startDirVal.(string); ok {
				sc.StartDir = strings.Trim(s, `"`)
			}
		}
		if iconVal, ok := entry["icon"]; ok {
			if s, ok := iconVal.(string); ok {
				sc.Icon = strings.Trim(s, `"`)
			}
		}
		if pathVal, ok := entry["ShortcutPath"]; ok {
			if s, ok := pathVal.(string); ok {
				sc.ShortcutPath = strings.Trim(s, `"`)
			}
		}
		if optsVal, ok := entry["LaunchOptions"]; ok {
			if s, ok := optsVal.(string); ok {
				sc.LaunchOptions = s
			}
		}
		if hiddenVal, ok := entry["IsHidden"]; ok {
			if i, ok := hiddenVal.(int32); ok {
				sc.IsHidden = i != 0
			}
		}
		if overlayVal, ok := entry["AllowOverlay"]; ok {
			if i, ok := overlayVal.(int32); ok {
				sc.AllowOverlay = i != 0
			}
		}

		// Parse tags section
		if tagsVal, ok := entry["tags"]; ok {
			if tagsMap, ok := tagsVal.(map[string]interface{}); ok {
				for _, tv := range tagsMap {
					if s, ok := tv.(string); ok {
						sc.Tags = append(sc.Tags, s)
					}
				}
			}
		}

		// Skip hidden shortcuts
		if sc.IsHidden {
			continue
		}
		// Skip entries without a name
		if sc.AppName == "" {
			continue
		}

		result = append(result, sc)
	}
	return result, nil
}

// shortcutExeBase extracts the executable basename from a shortcut Exe path.
// Handles both Linux paths and Windows paths with forward/backward slashes.
func shortcutExeBase(exePath string) string {
	if exePath == "" {
		return ""
	}
	base := filepath.Base(exePath)
	// filepath.Base on Linux won't handle backslashes, so try that too.
	if idx := strings.LastIndex(base, `\`); idx >= 0 {
		base = base[idx+1:]
	}
	return base
}

// SteamShortcutsPaths returns candidate paths for shortcuts.vdf.
func SteamShortcutsPaths() []string {
	var paths []string
	home, _ := os.UserHomeDir()
	if home == "" {
		return paths
	}

	// The userdata directory contains per-Steam-account config.
	userdataBase := filepath.Join(home, ".local", "share", "Steam", "userdata")
	// Also check .steam paths
	altPaths := []string{
		filepath.Join(home, ".steam", "steam", "userdata"),
		filepath.Join(home, ".steam", "root", "userdata"),
	}

	for _, base := range append([]string{userdataBase}, altPaths...) {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			// SteamID folders are numeric
			if _, err := strconv.ParseUint(e.Name(), 10, 64); err != nil {
				continue
			}
			paths = append(paths, filepath.Join(base, e.Name(), "config", "shortcuts.vdf"))
		}
	}
	return paths
}
