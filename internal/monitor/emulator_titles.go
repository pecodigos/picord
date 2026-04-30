package monitor

import (
	"regexp"
	"strings"
)

// emulatorPatterns maps lower-case process names to regexes that extract the
// game title from the window title. Each regex should have a single capturing
// group containing the desired title.
var emulatorPatterns = map[string]*regexp.Regexp{
	// DuckStation: "DuckStation [Super Mario 64] (Software Renderer)"
	"duckstation": regexp.MustCompile(`\[([^\]]+)\]`),

	// PCSX2: "PCSX2 Qt [Final Fantasy X]" or "PCSX2 [Final Fantasy X]"
	"pcsx2": regexp.MustCompile(`\[([^\]]+)\]`),

	// Dolphin: "Dolphin | Super Mario Sunshine" or "Super Mario Sunshine | Dolphin"
	"dolphin-emu": regexp.MustCompile(`\|\s*([^|]+)$`),

	// Cemu: "Cemu - The Legend of Zelda: Breath of the Wild"
	"cemu": regexp.MustCompile(`^Cemu\s+-\s+(.+)$`),

	// Yuzu / Ryujinx: "Super Mario Odyssey - yuzu 1234" or "Game Title - Ryujinx"
	"yuzu":     regexp.MustCompile(`^(.+?)\s+-\s+[Yy]uzu`),
	"ryujinx":  regexp.MustCompile(`^(.+?)\s+-\s+[Rr]yujinx`),

	// RetroArch: "RetroArch [core] | Super Mario World" or "Super Mario World - RetroArch"
	"retroarch": regexp.MustCompile(`\|\s*(.+)$`),

	// melonDS: "Pokemon HeartGold - melonDS"
	"melonds": regexp.MustCompile(`^(.+?)\s+-\s+[Mm]elonDS`),

	// mGBA: "Pokemon Emerald - mGBA"
	"mgba": regexp.MustCompile(`^(.+?)\s+-\s+mGBA`),

	// Snes9x: "Super Mario World - Snes9x"
	"snes9x": regexp.MustCompile(`^(.+?)\s+-\s+Snes9x`),

	// DeSmuME: "Pokemon Black - DeSmuME"
	"desmume": regexp.MustCompile(`^(.+?)\s+-\s+[Dd]e[Ss]mu[Mm][Ee]`),

	// PPSSPP: "God of War: Chains of Olympus - PPSSPP"
	"ppsspp": regexp.MustCompile(`^(.+?)\s+-\s+PPSSPP`),

	// RPCS3: "RPCS3 [game name]" or "[game] · RPCS3"
	"rpcs3": regexp.MustCompile(`\[([^\]]+)\]`),
}

// ExtractEmulatorGameTitle attempts to extract the actual game title from an
// emulator window title. It returns the empty string when the title doesn't
// match a known pattern or when the result is just the emulator's own name.
func ExtractEmulatorGameTitle(processName, windowTitle string) string {
	if windowTitle == "" {
		return ""
	}
	re, ok := emulatorPatterns[strings.ToLower(processName)]
	if !ok {
		return ""
	}
	m := re.FindStringSubmatch(windowTitle)
	if len(m) < 2 {
		return ""
	}
	title := strings.TrimSpace(m[1])
	// Heuristic: if the extracted text is just the emulator name, ignore it.
	lower := strings.ToLower(title)
	switch lower {
	case "duckstation", "pcsx2", "dolphin", "cemu", "yuzu", "ryujinx",
		"retroarch", "melonds", "mgba", "snes9x", "desmume", "ppsspp", "rpcs3":
		return ""
	}
	// Also drop generic menu strings.
	if strings.Contains(lower, "no disk") || strings.Contains(lower, "no game") ||
		strings.Contains(lower, "select game") || strings.Contains(lower, "main menu") {
		return ""
	}
	return title
}
