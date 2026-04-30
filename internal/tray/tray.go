package tray

import (
	_ "embed"
	"os"
	"os/exec"

	"github.com/energye/systray"

	"github.com/pecodigos/picord/internal/profile"
)

//go:embed icon.png
var iconData []byte

type Actions struct {
	OpenSettings  func()
	OpenGUI       func()
	ReloadConfig  func()
	SetAutoDetect func(bool)
	SetOverride   func(*profile.Profile)
	ClearOverride func()
	Quit          func()
}

var (
	autoDetectItem *systray.MenuItem
	statusItem     *systray.MenuItem
)

func Run(actions Actions, iconPath string) {
	systray.Run(func() { onReady(actions, iconPath) }, func() {
		if actions.Quit != nil {
			actions.Quit()
		}
	})
}

func loadIcon(path string) []byte {
	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return data
		}
	}
	return iconData
}

func onReady(actions Actions, iconPath string) {
	systray.SetIcon(loadIcon(iconPath))
	systray.SetTitle("Picord")
	systray.SetTooltip("Picord - Discord Rich Presence Manager")

	statusItem = systray.AddMenuItem("Status: Idle", "")
	statusItem.Disable()

	systray.AddSeparator()

	autoDetectItem = systray.AddMenuItemCheckbox("Auto-Detect", "Automatically set presence", true)
	autoDetectItem.Click(func() {
		if autoDetectItem.Checked() {
			autoDetectItem.Uncheck()
			actions.SetAutoDetect(false)
		} else {
			autoDetectItem.Check()
			actions.SetAutoDetect(true)
		}
	})

	overrideSub := systray.AddMenuItem("Manual Override", "")
	clearOverrideItem := overrideSub.AddSubMenuItem("Clear Override", "Remove manual override")
	clearOverrideItem.Click(func() {
		actions.ClearOverride()
	})

	systray.AddSeparator()

	settingsItem := systray.AddMenuItem("Settings", "Open settings dialog")
	settingsItem.Click(func() {
		actions.OpenSettings()
	})

	guiItem := systray.AddMenuItem("Open Web GUI", "Open web interface")
	guiItem.Click(func() {
		actions.OpenGUI()
	})

	reloadItem := systray.AddMenuItem("Reload Config", "Reload configuration from disk")
	reloadItem.Click(func() {
		actions.ReloadConfig()
	})

	systray.AddSeparator()

	quitItem := systray.AddMenuItem("Quit", "Exit Picord")
	quitItem.Click(func() {
		actions.Quit()
	})
}

func UpdateStatus(text string) {
	if statusItem != nil {
		statusItem.SetTitle("Status: " + text)
	}
}

func SetAutoDetectState(enabled bool) {
	if autoDetectItem == nil {
		return
	}
	if enabled {
		autoDetectItem.Check()
	} else {
		autoDetectItem.Uncheck()
	}
}

func OpenBrowser(url string) {
	exec.Command("xdg-open", url).Start()
}
