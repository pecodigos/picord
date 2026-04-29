package tray

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os/exec"

	"github.com/energye/systray"

	"github.com/pecodigos/picord/internal/profile"
)

type Actions struct {
	OpenGUI       func()
	ReloadConfig  func()
	SetAutoDetect func(bool)
	SetOverride   func(*profile.Profile)
	ClearOverride func()
	Quit          func()
}

var (
	iconData       []byte
	autoDetectItem *systray.MenuItem
	statusItem     *systray.MenuItem
)

func generateIcon() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))

	purple := color.RGBA{114, 137, 218, 255}
	dark := color.RGBA{26, 26, 46, 255}

	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			dx, dy := float64(x-16), float64(y-16)
			dist := dx*dx + dy*dy
			if dist < 144 {
				img.Set(x, y, purple)
			} else if dist < 196 {
				img.Set(x, y, dark)
			} else {
				img.Set(x, y, color.RGBA{0, 0, 0, 0})
			}
		}
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func Run(actions Actions) {
	iconData = generateIcon()
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
