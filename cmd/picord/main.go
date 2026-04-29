package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/pecodigos/picord/internal/catalog"
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

	os.Exit(runCLI(args, debug))
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

var rpcNewClient = rpc.NewClient

type rpcManager struct {
	mu              sync.Mutex
	client          *rpc.Client
	appID           string
	desiredActivity *rpc.RichActivity
}

func newRPCManager(appID string) *rpcManager {
	return &rpcManager{appID: appID}
}

func (rm *rpcManager) connect() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if rm.client != nil && rm.client.IsConnected() {
		return nil
	}
	if rm.client != nil {
		rm.client.Close()
	}
	c, err := rpcNewClient(rm.appID)
	if err != nil {
		return err
	}
	rm.client = c
	// Replay the last desired activity so presence appears after reconnect.
	if rm.desiredActivity != nil {
		_ = c.SetActivity(rm.desiredActivity)
	}
	return nil
}

func (rm *rpcManager) isConnected() bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.client != nil && rm.client.IsConnected()
}

func (rm *rpcManager) setActivity(a *rpc.RichActivity) error {
	rm.mu.Lock()
	c := rm.client
	rm.desiredActivity = a
	rm.mu.Unlock()
	if c == nil {
		return fmt.Errorf("not connected")
	}
	return c.SetActivity(a)
}

func (rm *rpcManager) clearActivity() {
	rm.mu.Lock()
	c := rm.client
	rm.desiredActivity = nil
	rm.mu.Unlock()
	if c != nil {
		c.ClearActivity()
	}
}

func (rm *rpcManager) close() {
	rm.mu.Lock()
	c := rm.client
	rm.client = nil
	rm.desiredActivity = nil
	rm.mu.Unlock()
	if c != nil {
		c.ClearActivity()
		c.Close()
	}
}

func runDaemon(debug bool) int {
	configDir := configDirPath()
	configPath := filepath.Join(configDir, "picord", "config.yaml")

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Printf("Error loading config: %v, using defaults", err)
		cfg = defaultConfig()
	}

	rpcMgr := newRPCManager(cfg.AppID)
	if err := rpcMgr.connect(); err != nil {
		log.Printf("Warning: Cannot connect to Discord: %v", err)
		log.Println("Picord will run but Rich Presence won't work until Discord is available.")
	}

	state := server.NewAppState()
	state.SetAutoDetect(true)

	defaultProfiles := profile.DefaultProfiles()
	profileMgr := profile.NewManager(cfg.Profiles, defaultProfiles)

	var currentProfile *profile.Profile

	// Open catalog store if enabled.
	var catalogStore *catalog.Store
	var catalogMatcher *catalog.Matcher
	var imgResolver catalog.ImageResolver
	var catalogRefresher *catalog.Refresher
	if cfg.Catalog.Enabled {
		dataDir := os.Getenv("XDG_DATA_HOME")
		if dataDir == "" {
			home, _ := os.UserHomeDir()
			if home != "" {
				dataDir = filepath.Join(home, ".local", "share")
			}
		}
		dbPath := filepath.Join(dataDir, "picord", "catalog.db")
		_ = os.MkdirAll(filepath.Dir(dbPath), 0755)
		cs, err := catalog.Open(dbPath)
		if err != nil {
			log.Printf("Warning: cannot open catalog store: %v", err)
		} else {
			catalogStore = cs
			catalogMatcher = catalog.NewMatcher(catalogStore)
			imgResolver = catalog.ImageResolver{
				Mode:            catalog.ImageMode(cfg.Images.Mode),
				GenericAssetKey: cfg.Images.GenericAssetKey,
				ExternalEnabled: false, // only enabled after live validation
			}
			if cfg.Catalog.AutoRefresh {
				sources, err := catalog.BuildSources(cfg.Catalog.Sources)
				if err != nil {
					log.Printf("Warning: invalid catalog sources: %v", err)
				} else {
					catalogRefresher = catalog.NewRefresher(catalogStore, sources, time.Duration(cfg.Catalog.RefreshHours)*time.Hour)
					catalogRefresher.Start()
				}
			}
		}
	}

	configMgr, configErr := config.NewManager(configPath, func(newCfg config.AppConfig) {
		cfg = newCfg
		profileMgr.MergeUser(newCfg.Profiles)
		if catalogStore != nil {
			imgResolver = catalog.ImageResolver{
				Mode:            catalog.ImageMode(newCfg.Images.Mode),
				GenericAssetKey: newCfg.Images.GenericAssetKey,
				ExternalEnabled: false,
			}
		}
		log.Println("Config auto-reloaded")
	})
	if configErr != nil {
		log.Printf("Config watcher error: %v", configErr)
	}

	webServer := server.New(state, profileMgr, catalogStore)
	webServer.OnOverrideSet = func(p *profile.Profile) {
		state.SetOverride(p)
		if p != nil {
			setRichPresence(rpcMgr, p, nil)
			tray.UpdateStatus("Manual: " + p.Name)
		}
	}
	webServer.OnOverrideClear = func() {
		state.SetOverride(nil)
		currentProfile = nil
		rpcMgr.clearActivity()
		tray.UpdateStatus("Idle")
	}
	webServer.OnAutoDetectSet = func(enabled bool) {
		state.SetAutoDetect(enabled)
		tray.SetAutoDetectState(enabled)
		if !enabled {
			rpcMgr.clearActivity()
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

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		winner := selectBestPresence(ctx, profileMgr, catalogMatcher, imgResolver, procs)
		cancel()

		if winner != nil {
			if currentProfile == nil || currentProfile.Name != winner.Profile.Name {
				log.Printf("[presence] matched %s=%q process=%q", winner.source, winner.Profile.Name, winner.proc.Name)
				currentProfile = winner.Profile
				state.SetActive(winner.Profile.Name, winner.proc.Name)
				setRichPresence(rpcMgr, winner.Profile, winner.proc)
				tray.UpdateStatus(winner.Profile.Name)
			}
			return
		}

		if currentProfile != nil {
			log.Println("[presence] no match, clearing activity")
			currentProfile = nil
			state.ClearActive()
			rpcMgr.clearActivity()
			tray.UpdateStatus("Idle")
		}
	})
	procMonitor.SetDebug(debug)
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
				if !rpcMgr.isConnected() {
					if _, err := rpc.DiscoverSocket(); err == nil {
						if rerr := rpcMgr.connect(); rerr == nil {
							log.Println("Connected to Discord")
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
		cleanup(rpcMgr, httpServer, configMgr, procMonitor, reconnectStopCh, catalogStore, catalogRefresher)
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
			if !enabled {
				rpcMgr.clearActivity()
			}
		},
		SetOverride: func(p *profile.Profile) {
			state.SetOverride(p)
			if p != nil {
				setRichPresence(rpcMgr, p, nil)
			}
		},
		ClearOverride: func() {
			state.SetOverride(nil)
			currentProfile = nil
			rpcMgr.clearActivity()
			tray.UpdateStatus("Idle")
		},
		Quit: func() {
			cleanup(rpcMgr, httpServer, configMgr, procMonitor, reconnectStopCh, catalogStore, catalogRefresher)
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
		Catalog: config.CatalogConfig{
			Enabled:      true,
			AutoRefresh:  true,
			Sources:      config.DefaultCatalogSources,
			RefreshHours: 24,
		},
		Images: config.ImageConfig{
			Mode:            "generic",
			CacheEnabled:    true,
			MaxCacheMB:      512,
			GenericAssetKey: "picord_game",
		},
	}
}

func findBestCatalogMatch(
	ctx context.Context,
	matcher *catalog.Matcher,
	procs []profile.DetectedProcess,
) (*catalog.MatchResult, *profile.DetectedProcess) {
	var best *catalog.MatchResult
	var bestProc *profile.DetectedProcess
	for i := range procs {
		result := matcher.Match(ctx, procs[i])
		if result != nil {
			if best == nil || result.Confidence > best.Confidence {
				best = result
				bestProc = &procs[i]
			}
		}
	}
	return best, bestProc
}

type presenceWinner struct {
	Profile *profile.Profile
	proc    *profile.DetectedProcess
	source  string
}

func selectBestPresence(
	ctx context.Context,
	profileMgr *profile.Manager,
	catalogMatcher *catalog.Matcher,
	imgResolver catalog.ImageResolver,
	procs []profile.DetectedProcess,
) *presenceWinner {
	profileMatch, profileProc := profileMgr.Match(procs)

	var catResult *catalog.MatchResult
	var catProc *profile.DetectedProcess
	if catalogMatcher != nil {
		catResult, catProc = findBestCatalogMatch(ctx, catalogMatcher, procs)
	}

	if profileMatch == nil && catResult == nil {
		return nil
	}
	if profileMatch == nil {
		p := catResult.ToProfile(imgResolver)
		return &presenceWinner{Profile: &p, proc: catProc, source: "catalog"}
	}
	if catResult == nil {
		return &presenceWinner{Profile: profileMatch, proc: profileProc, source: "profile"}
	}

	// Both matched: compare scores on a 0-100 scale.
	// Profile score = priority * 10, capped at 100.
	profileScore := profileMatch.Priority * 10
	if profileScore > 100 {
		profileScore = 100
	}
	catalogScore := catResult.Confidence

	if catalogScore > profileScore {
		p := catResult.ToProfile(imgResolver)
		return &presenceWinner{Profile: &p, proc: catProc, source: "catalog"}
	}
	return &presenceWinner{Profile: profileMatch, proc: profileProc, source: "profile"}
}

func buildRichActivity(p *profile.Profile, proc *profile.DetectedProcess) *rpc.RichActivity {
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

	return activity
}

func setRichPresence(rm *rpcManager, p *profile.Profile, proc *profile.DetectedProcess) {
	activity := buildRichActivity(p, proc)

	// Always store the desired activity so it can be replayed on reconnect.
	if err := rm.setActivity(activity); err != nil {
		log.Printf("Error setting activity: %v, attempting reconnect", err)
		if rerr := rm.connect(); rerr == nil {
			if err2 := rm.setActivity(activity); err2 != nil {
				log.Printf("Error setting activity after reconnect: %v", err2)
			}
		} else {
			log.Printf("Reconnect failed: %v", rerr)
		}
	}
}

func cleanup(rm *rpcManager, httpServer *http.Server, configMgr *config.Manager, mon *monitor.Monitor, reconnectStopCh chan struct{}, catalogStore *catalog.Store, catalogRefresher *catalog.Refresher) {
	if reconnectStopCh != nil {
		close(reconnectStopCh)
	}
	if mon != nil {
		mon.Stop()
	}
	if catalogRefresher != nil {
		catalogRefresher.Stop()
	}
	rm.close()
	if httpServer != nil {
		httpServer.Close()
	}
	if configMgr != nil {
		configMgr.Close()
	}
	if catalogStore != nil {
		catalogStore.Close()
	}
	log.Println("Picord stopped")
}
