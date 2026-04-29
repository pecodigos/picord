package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/pecodigos/picord/internal/config"
	"github.com/pecodigos/picord/internal/monitor"
	"github.com/pecodigos/picord/internal/profile"
	"github.com/pecodigos/picord/internal/rpc"
	"github.com/pecodigos/picord/internal/server"
	"github.com/pecodigos/picord/internal/tray"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	configDir := configDirPath()
	configPath := filepath.Join(configDir, "picord", "config.yaml")

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Printf("Error loading config: %v, using defaults", err)
		cfg = defaultConfig()
	}

	rpcClient, rpcErr := rpc.NewClient(cfg.AppID)
	if rpcErr != nil {
		log.Printf("Warning: Cannot connect to Discord: %v", rpcErr)
		log.Println("Picord will run but Rich Presence won't work until Discord is available.")
	}

	state := server.NewAppState()
	state.SetAutoDetect(true)

	defaultProfiles := profile.DefaultProfiles()
	profileMgr := profile.NewManager(cfg.Profiles, defaultProfiles)

	var currentProfile *profile.Profile

	configMgr, configErr := config.NewManager(configPath, func(newCfg config.AppConfig) {
		cfg = newCfg
		profileMgr.MergeUser(newCfg.Profiles)
		log.Println("Config auto-reloaded")
	})
	if configErr != nil {
		log.Printf("Config watcher error: %v", configErr)
	}

	webServer := server.New(state, profileMgr)
	webServer.OnOverrideSet = func(p *profile.Profile) {
		state.SetOverride(p)
		if p != nil {
			setRichPresence(rpcClient, p)
			tray.UpdateStatus("Manual: " + p.Name)
		}
	}
	webServer.OnOverrideClear = func() {
		state.SetOverride(nil)
		currentProfile = nil
		if rpcClient != nil {
			rpcClient.ClearActivity()
		}
		tray.UpdateStatus("Idle")
	}
	webServer.OnAutoDetectSet = func(enabled bool) {
		state.SetAutoDetect(enabled)
		tray.SetAutoDetectState(enabled)
		if !enabled && rpcClient != nil {
			rpcClient.ClearActivity()
			tray.UpdateStatus("Disabled")
		}
		if enabled {
			tray.UpdateStatus("Idle")
		}
	}
	webServer.OnReloadConfig = func() {
		newCfg, err := config.Load(configPath)
		if err == nil {
			cfg = newCfg
			profileMgr.MergeUser(newCfg.Profiles)
			log.Println("Config reloaded from GUI")
		}
	}
	webServer.OnProfilesSaved = func(profiles []profile.Profile) {
		if configMgr != nil {
			if err := configMgr.UpdateProfiles(profiles); err != nil {
				log.Printf("Error saving profiles: %v", err)
			}
		}
	}

	httpServer := server.StartServer(fmt.Sprintf("127.0.0.1:%d", cfg.WebPort), webServer)

	procMonitor := monitor.New(cfg.PollInterval, func(procs []profile.DetectedProcess) {
		state.SetDetected(procs)

		if state.HasOverride() || !state.AutoDetectEnabled() {
			return
		}

		match, proc := profileMgr.Match(procs)
		if match != nil && proc != nil {
			if currentProfile == nil || currentProfile.Name != match.Name {
				currentProfile = match
				state.SetActive(match.Name, proc.Name)
			if rpcClient != nil {
				setRichPresence(rpcClient, match)
			}
				tray.UpdateStatus(match.Name)
			}
		} else if currentProfile != nil {
			currentProfile = nil
			state.ClearActive()
			if rpcClient != nil {
				rpcClient.ClearActivity()
			}
			tray.UpdateStatus("Idle")
		}
	})

	procMonitor.Start()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cleanup(rpcClient, httpServer, configMgr, procMonitor)
		os.Exit(0)
	}()

	tray.Run(tray.Actions{
		OpenGUI: func() {
			tray.OpenBrowser(fmt.Sprintf("http://127.0.0.1:%d", cfg.WebPort))
		},
		ReloadConfig: func() {
			newCfg, err := config.Load(configPath)
			if err == nil {
				cfg = newCfg
				profileMgr.MergeUser(newCfg.Profiles)
				log.Println("Config reloaded from tray")
			}
		},
		SetAutoDetect: func(enabled bool) {
			state.SetAutoDetect(enabled)
			if !enabled && rpcClient != nil {
				rpcClient.ClearActivity()
			}
		},
		SetOverride: func(p *profile.Profile) {
			state.SetOverride(p)
			if p != nil && rpcClient != nil {
				setRichPresence(rpcClient, p)
			}
		},
		ClearOverride: func() {
			state.SetOverride(nil)
			currentProfile = nil
			if rpcClient != nil {
				rpcClient.ClearActivity()
			}
			tray.UpdateStatus("Idle")
		},
		Quit: func() {
			cleanup(rpcClient, httpServer, configMgr, procMonitor)
			os.Exit(0)
		},
	})
}

func configDirPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return xdg
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".config")
}

func defaultConfig() config.AppConfig {
	return config.AppConfig{
		AppID:        "",
		PollInterval: 2,
		WebPort:      17970,
	}
}

func setRichPresence(client *rpc.Client, p *profile.Profile) {
	if client == nil {
		return
	}

	activity := &rpc.RichActivity{
		Details:  p.Activity.Details,
		State:    p.Activity.State,
		Instance: false,
	}

	if p.Activity.LargeImage != "" || p.Activity.LargeText != "" ||
		p.Activity.SmallImage != "" || p.Activity.SmallText != "" {
		activity.Assets = &rpc.RichAssets{
			LargeImage: p.Activity.LargeImage,
			LargeText:  p.Activity.LargeText,
			SmallImage: p.Activity.SmallImage,
			SmallText:  p.Activity.SmallText,
		}
	}

	if len(p.Activity.Buttons) > 0 {
		activity.Buttons = make([]rpc.RichButton, len(p.Activity.Buttons))
		for i, b := range p.Activity.Buttons {
			activity.Buttons[i] = rpc.RichButton{
				Label: b.Label,
				URL:   b.URL,
			}
		}
	}

	if err := client.SetActivity(activity); err != nil {
		log.Printf("Error setting activity: %v", err)
	}
}

func cleanup(client *rpc.Client, httpServer *http.Server, configMgr *config.Manager, mon *monitor.Monitor) {
	if mon != nil {
		mon.Stop()
	}
	if client != nil {
		client.ClearActivity()
		client.Close()
	}
	if httpServer != nil {
		httpServer.Close()
	}
	if configMgr != nil {
		configMgr.Close()
	}
	log.Println("Picord stopped")
}
