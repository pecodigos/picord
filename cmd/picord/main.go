package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pecodigos/picord/internal/config"
	"github.com/pecodigos/picord/internal/monitor"
	"github.com/pecodigos/picord/internal/profile"
	"github.com/pecodigos/picord/internal/rpc"
	"github.com/pecodigos/picord/internal/server"
	"github.com/pecodigos/picord/internal/tray"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	args, debug := parseGlobalFlags(os.Args[1:])
	if debug {
		setupDebugLogging()
	}

	os.Exit(runCLI(args))
}

func parseGlobalFlags(args []string) ([]string, bool) {
	var filtered []string
	debug := false
	for _, a := range args {
		if a == "--debug" || a == "-debug" {
			debug = true
			continue
		}
		filtered = append(filtered, a)
	}
	return filtered, debug
}

func setupDebugLogging() {
	stateDir := os.Getenv("XDG_STATE_HOME")
	if stateDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			stateDir = filepath.Join(home, ".local", "state")
		}
	}
	logDir := filepath.Join(stateDir, "picord")
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, "picord.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("Cannot open debug log: %v", err)
		return
	}
	log.SetOutput(io.MultiWriter(os.Stdout, f))
	log.Println("Debug logging enabled")
}

func runDaemon() int {
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
			setRichPresence(rpcClient, p, nil)
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

	procMonitor := monitor.NewWithOptions(cfg.PollInterval, cfg.ScanAllProcesses, func(procs []profile.DetectedProcess) {
		state.SetDetected(procs)

		if state.HasOverride() || !state.AutoDetectEnabled() {
			return
		}

		match, proc := profileMgr.Match(procs)
		if match != nil && proc != nil {
			if currentProfile == nil || currentProfile.Name != match.Name {
				log.Printf("[presence] matched profile=%q process=%q", match.Name, proc.Name)
				currentProfile = match
				state.SetActive(match.Name, proc.Name)
				if rpcClient != nil {
					setRichPresence(rpcClient, match, proc)
				}
				tray.UpdateStatus(match.Name)
			}
		} else if currentProfile != nil {
			log.Println("[presence] no match, clearing activity")
			currentProfile = nil
			state.ClearActive()
			if rpcClient != nil {
				rpcClient.ClearActivity()
			}
			tray.UpdateStatus("Idle")
		}
	})

	procMonitor.Start()

	reconnectStopCh := make(chan struct{})
	// Background reconnect goroutine
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-reconnectStopCh:
				return
			case <-ticker.C:
				if rpcClient != nil && !rpcClient.IsConnected() {
					if _, err := rpc.DiscoverSocket(); err == nil {
						if rerr := rpcClient.Reconnect(); rerr == nil {
							log.Println("Reconnected to Discord")
						}
					}
				}
			}
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cleanup(rpcClient, httpServer, configMgr, procMonitor, reconnectStopCh)
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
				setRichPresence(rpcClient, p, nil)
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
			cleanup(rpcClient, httpServer, configMgr, procMonitor, reconnectStopCh)
			os.Exit(0)
		},
	})
	return 0
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
		AppID:            "",
		PollInterval:     2,
		WebPort:          17970,
		ScanAllProcesses: true,
	}
}

func setRichPresence(client *rpc.Client, p *profile.Profile, proc *profile.DetectedProcess) {
	if client == nil {
		return
	}

	act := p.Activity
	if proc != nil {
		act = profile.RenderActivity(act, *proc)
	}

	activity := &rpc.RichActivity{
		Details:  act.Details,
		State:    act.State,
		Instance: false,
	}

	if act.LargeImage != "" || act.LargeText != "" ||
		act.SmallImage != "" || act.SmallText != "" {
		activity.Assets = &rpc.RichAssets{
			LargeImage: act.LargeImage,
			LargeText:  act.LargeText,
			SmallImage: act.SmallImage,
			SmallText:  act.SmallText,
		}
	}

	if len(act.Buttons) > 0 {
		activity.Buttons = make([]rpc.RichButton, len(act.Buttons))
		for i, b := range act.Buttons {
			activity.Buttons[i] = rpc.RichButton{
				Label: b.Label,
				URL:   b.URL,
			}
		}
	}

	if err := client.SetActivity(activity); err != nil {
		log.Printf("Error setting activity: %v, attempting reconnect", err)
		if rerr := client.Reconnect(); rerr == nil {
			if err2 := client.SetActivity(activity); err2 != nil {
				log.Printf("Error setting activity after reconnect: %v", err2)
			}
		} else {
			log.Printf("Reconnect failed: %v", rerr)
		}
	}
}

func cleanup(client *rpc.Client, httpServer *http.Server, configMgr *config.Manager, mon *monitor.Monitor, reconnectStopCh chan struct{}) {
	if reconnectStopCh != nil {
		close(reconnectStopCh)
	}
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
