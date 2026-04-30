package monitor

import "testing"

func TestExtractEmulatorGameTitle(t *testing.T) {
	cases := []struct {
		name    string
		title   string
		want    string
	}{
		// DuckStation
		{"duckstation", "DuckStation [Super Mario 64] (Software Renderer)", "Super Mario 64"},
		{"duckstation", "DuckStation [Final Fantasy VII]", "Final Fantasy VII"},
		{"duckstation", "DuckStation", ""},

		// PCSX2
		{"pcsx2", "PCSX2 Qt [Final Fantasy X]", "Final Fantasy X"},
		{"pcsx2", "PCSX2 [Shadow of the Colossus]", "Shadow of the Colossus"},
		{"pcsx2", "PCSX2", ""},

		// Dolphin
		{"dolphin-emu", "Dolphin | Super Mario Sunshine", "Super Mario Sunshine"},
		{"dolphin-emu", "Dolphin", ""},

		// Cemu
		{"cemu", "Cemu - The Legend of Zelda: Breath of the Wild", "The Legend of Zelda: Breath of the Wild"},
		{"cemu", "Cemu", ""},

		// Yuzu / Ryujinx
		{"yuzu", "Super Mario Odyssey - yuzu 1234", "Super Mario Odyssey"},
		{"ryujinx", "Animal Crossing: New Horizons - Ryujinx", "Animal Crossing: New Horizons"},

		// RetroArch
		{"retroarch", "RetroArch [Snes9x] | Super Mario World", "Super Mario World"},
		{"retroarch", "RetroArch", ""},

		// melonDS / mGBA / Snes9x / DeSmuME / PPSSPP
		{"melonds", "Pokemon HeartGold - melonDS", "Pokemon HeartGold"},
		{"mgba", "Pokemon Emerald - mGBA", "Pokemon Emerald"},
		{"snes9x", "Super Mario World - Snes9x", "Super Mario World"},
		{"desmume", "Pokemon Black - DeSmuME", "Pokemon Black"},
		{"ppsspp", "God of War: Chains of Olympus - PPSSPP", "God of War: Chains of Olympus"},

		// RPCS3
		{"rpcs3", "RPCS3 [Demon's Souls]", "Demon's Souls"},

		// Negative: unknown emulator, empty title, generic menu
		{"unknown", "Something", ""},
		{"duckstation", "", ""},
		{"duckstation", "DuckStation [Select Game]", ""},
		{"duckstation", "DuckStation [Main Menu]", ""},
		{"pcsx2", "PCSX2 [No Disk]", ""},
	}

	for _, tc := range cases {
		got := ExtractEmulatorGameTitle(tc.name, tc.title)
		if got != tc.want {
			t.Errorf("ExtractEmulatorGameTitle(%q, %q) = %q, want %q", tc.name, tc.title, got, tc.want)
		}
	}
}
