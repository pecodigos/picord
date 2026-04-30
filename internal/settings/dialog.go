package settings

import (
	"os/exec"
	"strconv"

	"github.com/gotk3/gotk3/gtk"

	"github.com/pecodigos/picord/internal/config"
)

type Dialog struct {
	cfg    config.AppConfig
	save   func(config.AppConfig) error
	reload func()
}

func NewDialog(cfg config.AppConfig, saveFn func(config.AppConfig) error, reloadFn func()) *Dialog {
	return &Dialog{cfg: cfg, save: saveFn, reload: reloadFn}
}

func (d *Dialog) UpdateConfig(cfg config.AppConfig) {
	d.cfg = cfg
}

func (d *Dialog) Show() {
	go func() {
		gtk.Init(nil)

		win, err := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
		if err != nil {
			return
		}
		win.SetTitle("Picord Settings")
		win.SetDefaultSize(480, 400)
		win.SetPosition(gtk.WIN_POS_CENTER)
		win.Connect("destroy", func() {
			gtk.MainQuit()
		})

		notebook, _ := gtk.NotebookNew()
		notebook.SetBorderWidth(8)

		notebook.AppendPage(d.buildGeneralTab(), label("General"))
		notebook.AppendPage(d.buildDetectionTab(), label("Detection"))
		notebook.AppendPage(d.buildCatalogTab(), label("Catalog"))

		btnBox, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)

		saveBtn, _ := gtk.ButtonNewWithLabel("Save")
		cancelBtn, _ := gtk.ButtonNewWithLabel("Cancel")

		btnBox.PackEnd(saveBtn, false, false, 0)
		btnBox.PackEnd(cancelBtn, false, false, 0)

		mainBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 6)
		mainBox.SetBorderWidth(8)
		mainBox.PackStart(notebook, true, true, 0)
		mainBox.PackStart(btnBox, false, false, 0)

		win.Add(mainBox)

		cancelBtn.Connect("clicked", func() {
			win.Close()
		})

		saveBtn.Connect("clicked", func() {
			if err := d.save(d.cfg); err == nil {
				d.reload()
			}
			win.Close()
		})

		win.ShowAll()
		gtk.Main()
	}()
}

func (d *Dialog) buildGeneralTab() *gtk.Box {
	box, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 8)
	box.SetBorderWidth(12)

	frame, _ := gtk.FrameNew("Startup")
	box.PackStart(frame, false, false, 0)
	frameBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 4)
	frameBox.SetBorderWidth(8)
	frame.Add(frameBox)

	loginCheck, _ := gtk.CheckButtonNewWithLabel("Launch on login (systemd service)")
	loginCheck.SetActive(serviceEnabled())
	loginCheck.Connect("toggled", func() {
		if loginCheck.GetActive() {
			enableService()
		} else {
			disableService()
		}
	})
	frameBox.PackStart(loginCheck, false, false, 0)

	trayFrame, _ := gtk.FrameNew("Appearance")
	box.PackStart(trayFrame, false, false, 0)
	trayBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 4)
	trayBox.SetBorderWidth(8)
	trayFrame.Add(trayBox)

	trayCheck, _ := gtk.CheckButtonNewWithLabel("Show icon in system tray")
	trayCheck.SetActive(d.cfg.ShowTrayIcon)
	trayCheck.Connect("toggled", func() {
		d.cfg.ShowTrayIcon = trayCheck.GetActive()
	})
	trayBox.PackStart(trayCheck, false, false, 0)

	trayNote, _ := gtk.LabelNew("(requires restart to take effect)")
	trayNote.SetXAlign(0)
	trayNote.SetOpacity(0.6)
	trayBox.PackStart(trayNote, false, false, 0)

	return box
}

func (d *Dialog) buildDetectionTab() *gtk.Box {
	box, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 8)
	box.SetBorderWidth(12)

	frame, _ := gtk.FrameNew("Process Scanning")
	box.PackStart(frame, false, false, 0)
	frameBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 4)
	frameBox.SetBorderWidth(8)
	frame.Add(frameBox)

	scanCheck, _ := gtk.CheckButtonNewWithLabel("Scan all processes")
	scanCheck.SetActive(d.cfg.ScanAllProcesses)
	scanCheck.Connect("toggled", func() {
		d.cfg.ScanAllProcesses = scanCheck.GetActive()
	})
	frameBox.PackStart(scanCheck, false, false, 0)

	filterFrame, _ := gtk.FrameNew("Show in Discord Presence")
	box.PackStart(filterFrame, false, false, 0)
	filterBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 4)
	filterBox.SetBorderWidth(8)
	filterFrame.Add(filterBox)

	gamesCheck, _ := gtk.CheckButtonNewWithLabel("Show games")
	gamesCheck.SetActive(d.cfg.Detection.ShowGames)
	gamesCheck.Connect("toggled", func() {
		d.cfg.Detection.ShowGames = gamesCheck.GetActive()
	})
	filterBox.PackStart(gamesCheck, false, false, 0)

	toolsCheck, _ := gtk.CheckButtonNewWithLabel("Show creative & tool apps (OBS, Blender, etc.)")
	toolsCheck.SetActive(d.cfg.Detection.ShowTools)
	toolsCheck.Connect("toggled", func() {
		d.cfg.Detection.ShowTools = toolsCheck.GetActive()
	})
	filterBox.PackStart(toolsCheck, false, false, 0)

	return box
}

func (d *Dialog) buildCatalogTab() *gtk.Box {
	box, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 8)
	box.SetBorderWidth(12)

	frame, _ := gtk.FrameNew("Catalog")
	box.PackStart(frame, false, false, 0)
	frameBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 4)
	frameBox.SetBorderWidth(8)
	frame.Add(frameBox)

	enabledCheck, _ := gtk.CheckButtonNewWithLabel("Enable game catalog")
	enabledCheck.SetActive(d.cfg.Catalog.Enabled)
	enabledCheck.Connect("toggled", func() {
		d.cfg.Catalog.Enabled = enabledCheck.GetActive()
	})
	frameBox.PackStart(enabledCheck, false, false, 0)

	autoRefreshCheck, _ := gtk.CheckButtonNewWithLabel("Auto-refresh catalog")
	autoRefreshCheck.SetActive(d.cfg.Catalog.AutoRefresh)
	autoRefreshCheck.Connect("toggled", func() {
		d.cfg.Catalog.AutoRefresh = autoRefreshCheck.GetActive()
	})
	frameBox.PackStart(autoRefreshCheck, false, false, 0)

	apiFrame, _ := gtk.FrameNew("SteamGridDB API Key (optional, for cover art)")
	box.PackStart(apiFrame, false, false, 0)
	apiBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 4)
	apiBox.SetBorderWidth(8)
	apiFrame.Add(apiBox)

	apiEntry, _ := gtk.EntryNew()
	apiEntry.SetText(d.cfg.Catalog.SteamGridDBAPIKey)
	apiEntry.SetVisibility(false)
	apiEntry.SetInvisibleChar('*')
	apiEntry.Connect("changed", func() {
		text, _ := apiEntry.GetText()
		d.cfg.Catalog.SteamGridDBAPIKey = text
	})
	apiBox.PackStart(apiEntry, false, false, 0)

	intervalLabel, _ := gtk.LabelNew("Refresh interval (hours):")
	intervalLabel.SetXAlign(0)
	apiBox.PackStart(intervalLabel, false, false, 0)

	intervalEntry, _ := gtk.EntryNew()
	intervalEntry.SetText(strconv.Itoa(d.cfg.Catalog.RefreshHours))
	intervalEntry.Connect("changed", func() {
		text, _ := intervalEntry.GetText()
		if n, err := strconv.Atoi(text); err == nil && n > 0 {
			d.cfg.Catalog.RefreshHours = n
		}
	})
	apiBox.PackStart(intervalEntry, false, false, 0)

	return box
}

func label(text string) *gtk.Label {
	l, _ := gtk.LabelNew(text)
	return l
}

func serviceEnabled() bool {
	out, err := exec.Command("systemctl", "--user", "is-enabled", "picord.service").Output()
	return err == nil && string(out) == "enabled\n"
}

func enableService() {
	exec.Command("systemctl", "--user", "enable", "picord.service").Run()
}

func disableService() {
	exec.Command("systemctl", "--user", "disable", "picord.service").Run()
}


