package tray

import (
	_ "embed"
	"os/exec"

	"github.com/energye/systray"

	"github.com/pecodigos/picord/internal/profile"
)

//go:embed icon.png
var iconData []byte

type Actions struct {
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

func Run(actions Actions) {
	systray.Run(func() { onReady(actions) }, func() {
		if actions.Quit != nil {
			actions.Quit()
		}
	})
}

func onReady(actions Actions) {
	systray.SetIcon(iconData)
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

	openGUIItem := systray.AddMenuItem("Open Settings", "Open web configuration")
	openGUIItem.Click(func() {
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
