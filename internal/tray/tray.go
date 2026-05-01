package tray

import (
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/energye/systray"

	"github.com/pecodigos/picord/internal/profile"
)

//go:embed icon.png
var iconData []byte

type Actions struct {
	OpenSettings  func()
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
	if err := waitForTrayHost(60 * time.Second); err != nil {
		log.Printf("[tray] %v, launching anyway (tray icon may be delayed)", err)
	}
	systray.Run(func() { onReady(actions, iconPath) }, func() {
		if actions.Quit != nil {
			actions.Quit()
		}
	})
}

// waitForTrayHost polls the D-Bus session bus until org.kde.StatusNotifierWatcher
// is available, or until timeout is reached. This ensures the tray host (waybar,
// KDE panel, etc.) is ready before systray.Run() attempts to register the icon.
func waitForTrayHost(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error

	for time.Now().Before(deadline) {
		conn, err := dbus.SessionBus()
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}

		var hasOwner bool
		err = conn.BusObject().Call(
			"org.freedesktop.DBus.NameHasOwner", 0,
			"org.kde.StatusNotifierWatcher",
		).Store(&hasOwner)
		conn.Close()

		if err == nil && hasOwner {
			return nil
		}
		if !hasOwner {
			lastErr = errors.New("StatusNotifierWatcher not registered on session bus")
		} else {
			lastErr = err
		}
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("tray host not available after %v: %v", timeout, lastErr)
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
