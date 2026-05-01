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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pecodigos/picord/internal/catalog"
	"github.com/pecodigos/picord/internal/config"
	"github.com/pecodigos/picord/internal/monitor"
	"github.com/pecodigos/picord/internal/profile"
	"github.com/pecodigos/picord/internal/rpc"
	"github.com/pecodigos/picord/internal/server"
	"github.com/pecodigos/picord/internal/settings"
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
	os.MkdirAll(logDir, 0700)
	logPath := filepath.Join(logDir, "picord.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
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
		if rerr := c.SetActivity(rm.desiredActivity); rerr != nil {
			log.Printf("Error replaying activity after reconnect: %v", rerr)
		} else {
			log.Println("Replayed desired activity after reconnect")
		}
	}
	return nil
}

func (rm *rpcManager) isConnected() bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.client != nil && rm.client.IsConnected()
}

func (rm *rpcManager) switchApp(appID string) error {
	rm.mu.Lock()
	if appID == "" || appID == rm.appID {
		rm.mu.Unlock()
		return nil
	}
	c := rm.client
	rm.client = nil
	rm.appID = appID
	rm.mu.Unlock()
	if c != nil {
		c.ClearActivity()
		c.Close()
	}
	return rm.connect()
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

func (rm *rpcManager) setIdleActivity(assetKey string) error {
	act := &rpc.RichActivity{
		Details: "Idle",
		Assets: &rpc.RichAssets{
			LargeImage: assetKey,
			LargeText:  "Picord",
		},
	}
	return rm.setActivity(act)
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

type daemonOptions struct {
	TrayOverride *bool
}

func boolPtr(v bool) *bool {
	return &v
}

func runDaemon(debug bool) int {
	return runDaemonWithOptions(debug, daemonOptions{})
}

func runDaemonWithOptions(debug bool, opts daemonOptions) int {
	configDir := configDirPath()
	configPath := filepath.Join(configDir, "picord", "config.yaml")

	// Acquire singleton lock so only one Picord daemon runs at a time.
	stateDir := os.Getenv("XDG_STATE_HOME")
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			stateDir = filepath.Join(home, ".local", "state")
		}
	}
	pidFile := filepath.Join(stateDir, "picord", "picord.pid")
	releaseLock, err := acquireLock(pidFile)
	if err != nil {
		log.Printf("Picord is already running (pid: %s)", err)
		return 1
	}
	defer releaseLock()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Printf("Error loading config: %v, using defaults", err)
		cfg = defaultConfig()
	}

	showTray := cfg.ShowTrayIcon
	if opts.TrayOverride != nil {
		showTray = *opts.TrayOverride
	}

	appID := cfg.ResolveDiscordApp("main")
	log.Printf("[discord] using app ID: %s", appID)
	rpcMgr := newRPCManager(appID)
	state := server.NewAppState()
	state.SetAutoDetect(true)
	state.SetAppID(appID)
	if err := rpcMgr.connect(); err != nil {
		log.Printf("[discord] connection failed: %v", err)
		log.Println("[discord] Picord will retry every 10s. Rich Presence won't work until Discord is available.")
		state.SetRPCConnected(false)
	} else {
		log.Println("[discord] connected successfully")
		state.SetRPCConnected(true)
		if err := rpcMgr.setIdleActivity(cfg.Images.GenericAssetKey); err != nil {
			log.Printf("Error setting idle presence: %v", err)
		}
	}

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
				ExternalEnabled: cfg.Images.ExternalValidated,
				LocalAssetBase:  fmt.Sprintf("http://127.0.0.1:%d", cfg.WebPort),
			}
			if cfg.Catalog.AutoRefresh {
				sources, skipped := catalog.BuildSources(cfg.Catalog.Sources)
				for _, s := range skipped {
					log.Printf("Warning: skipping unknown catalog source: %s", s)
				}
				if len(sources) > 0 {
					catalogRefresher = catalog.NewRefresher(catalogStore, sources, time.Duration(cfg.Catalog.RefreshHours)*time.Hour)
					if cfg.Catalog.SteamGridDBAPIKey != "" {
						catalogRefresher.SetEnricher(catalog.NewEnricher(catalogStore, cfg.Catalog.SteamGridDBAPIKey))
					}
					catalogRefresher.Start()
				}
			}
		}
	}

	applyConfig := func(newCfg config.AppConfig, source string) {
		// Restart-only fields: log a warning if they changed.
		if newCfg.WebPort != cfg.WebPort {
			log.Printf("[%s] api_port changed to %d (requires restart)", source, newCfg.WebPort)
		}
		if newCfg.PollInterval != cfg.PollInterval {
			log.Printf("[%s] poll_interval changed to %d (requires restart)", source, newCfg.PollInterval)
		}
		if newCfg.ResolveDiscordApp("main") != cfg.ResolveDiscordApp("main") {
			log.Printf("[%s] app_id changed (requires restart)", source)
		}
		if newCfg.ScanAllProcesses != cfg.ScanAllProcesses {
			log.Printf("[%s] scan_all_processes changed to %v (requires restart)", source, newCfg.ScanAllProcesses)
		}

		cfg = newCfg
		profileMgr.ReplaceUser(newCfg.Profiles)

		if catalogStore != nil {
			imgResolver = catalog.ImageResolver{
				Mode:            catalog.ImageMode(newCfg.Images.Mode),
				GenericAssetKey: newCfg.Images.GenericAssetKey,
				ExternalEnabled: newCfg.Images.ExternalValidated,
				LocalAssetBase:  fmt.Sprintf("http://127.0.0.1:%d", newCfg.WebPort),
			}
			// Restart refresher if sources or interval changed.
			if catalogRefresher != nil {
				catalogRefresher.Stop()
				catalogRefresher = nil
			}
			if newCfg.Catalog.AutoRefresh {
				sources, skipped := catalog.BuildSources(newCfg.Catalog.Sources)
				for _, s := range skipped {
					log.Printf("[%s] skipping unknown catalog source: %s", source, s)
				}
				if len(sources) > 0 {
					catalogRefresher = catalog.NewRefresher(catalogStore, sources, time.Duration(newCfg.Catalog.RefreshHours)*time.Hour)
					if newCfg.Catalog.SteamGridDBAPIKey != "" {
						catalogRefresher.SetEnricher(catalog.NewEnricher(catalogStore, newCfg.Catalog.SteamGridDBAPIKey))
					}
					catalogRefresher.Start()
				}
			}
		}
		log.Printf("[%s] Config reloaded", source)
	}

	var settingsDialog *settings.Dialog

	configMgr, configErr := config.NewManager(configPath, func(newCfg config.AppConfig) {
		applyConfig(newCfg, "watcher")
		if settingsDialog != nil {
			settingsDialog.UpdateConfig(newCfg)
		}
	})
	if configErr != nil {
		log.Printf("Config watcher error: %v", configErr)
	}

	settingsDialog = settings.NewDialog(cfg, func(newCfg config.AppConfig) error {
		if configMgr != nil {
			return configMgr.Update(newCfg)
		}
		return config.Save(configPath, newCfg)
	}, func() {})

	stateDir = server.TokenStateDir()
	apiToken, tokenErr := server.GenerateToken(stateDir)
	if tokenErr != nil {
		log.Printf("Warning: cannot generate API token: %v", tokenErr)
	}

	webServer := server.New(state, profileMgr, catalogStore)
	webServer.SetToken(apiToken)

	var procMonitor *monitor.Monitor
	webServer.SetSettingsProvider(func() config.AppConfig {
		if configMgr != nil {
			return configMgr.Config()
		}
		return cfg
	})
	if cfg.Catalog.SteamGridDBAPIKey != "" {
		webServer.SetCatalogEnricher(catalog.NewEnricher(catalogStore, cfg.Catalog.SteamGridDBAPIKey))
	}
	webServer.OnOverrideSet = func(p *profile.Profile) {
		state.SetOverride(p)
		if p != nil {
			appID := cfg.ResolveDiscordApp("main")
			if p.DiscordApp != "" {
				appID = cfg.ResolveDiscordApp(p.DiscordApp)
			}
			setRichPresence(rpcMgr, appID, p, nil)
			state.SetAppID(appID)
			tray.UpdateStatus("Manual: " + p.Name)
			state.SetMatchInfo(profile.MatchInfo{
				Source:       "override",
				ProfileName:  p.Name,
				DiscordAppID: appID,
				RPConnected:  rpcMgr.isConnected(),
			})
		}
	}
	webServer.OnOverrideClear = func() {
		state.SetOverride(nil)
		currentProfile = nil
		rpcMgr.clearActivity()
		tray.UpdateStatus("Idle")
		state.SetMatchInfo(profile.MatchInfo{
			Source:      "none",
			RPConnected: rpcMgr.isConnected(),
		})
	}
	webServer.OnAutoDetectSet = func(enabled bool) {
		state.SetAutoDetect(enabled)
		tray.SetAutoDetectState(enabled)
		if !enabled {
			rpcMgr.clearActivity()
			currentProfile = nil
			state.ClearActive()
			tray.UpdateStatus("Disabled")
		} else {
			tray.UpdateStatus("Idle")
			if procMonitor != nil {
				procMonitor.ForceScan()
			}
		}
	}
	webServer.OnSettingsSaved = func(newCfg config.AppConfig) error {
		if configMgr != nil {
			return configMgr.Update(newCfg)
		}
		applyConfig(newCfg, "settings")
		return config.Save(configPath, newCfg)
	}
	webServer.OnReloadConfig = func() {
		newCfg, err := config.Load(configPath)
		if err == nil {
			applyConfig(newCfg, "gui")
			if newCfg.Catalog.SteamGridDBAPIKey != "" {
				webServer.SetCatalogEnricher(catalog.NewEnricher(catalogStore, newCfg.Catalog.SteamGridDBAPIKey))
			} else {
				webServer.SetCatalogEnricher(nil)
			}
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

	procMonitor = monitor.NewWithOptions(cfg.PollInterval, cfg.ScanAllProcesses, func(procs []profile.DetectedProcess) {
		mode := server.ScanModeIPCCandidates
		if cfg.ScanAllProcesses {
			mode = server.ScanModeAll
		}
		state.SetScanSnapshot(server.ScanSnapshot{
			Procs: procs,
			Time:  time.Now(),
			Mode:  mode,
			State: server.ScanStateScanned,
		})

		if state.HasOverride() || !state.AutoDetectEnabled() {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		winner := selectBestPresence(ctx, profileMgr, catalogMatcher, imgResolver, cfg.Detection, procs)
		cancel()

		if winner != nil {
			appID := cfg.ResolveDiscordApp("main")
			if winner.Profile.DiscordApp != "" {
				appID = cfg.ResolveDiscordApp(winner.Profile.DiscordApp)
			}
			if currentProfile == nil || currentProfile.Name != winner.Profile.Name {
				log.Printf("[presence] matched %s=%q process=%q reason=%s confidence=%d", winner.source, winner.Profile.Name, winner.proc.Name, winner.reason, winner.confidence)
				currentProfile = winner.Profile
				state.SetActive(winner.Profile.Name, winner.proc.Name)
				setRichPresence(rpcMgr, appID, winner.Profile, winner.proc)
				state.SetAppID(appID)
				tray.UpdateStatus(winner.Profile.Name)
			}
			state.SetRPCConnected(rpcMgr.isConnected())
			state.SetMatchInfo(profile.MatchInfo{
				Source:       winner.source,
				ProfileName:  winner.Profile.Name,
				ProcessName:  winner.proc.Name,
				Reason:       winner.reason,
				Confidence:   winner.confidence,
				DiscordAppID: appID,
				RPConnected:  rpcMgr.isConnected(),
			})
			return
		}

		if currentProfile != nil {
			log.Println("[presence] no match, setting idle presence")
			currentProfile = nil
			state.ClearActive()
			state.SetAppID(cfg.ResolveDiscordApp("main"))
			if err := rpcMgr.setIdleActivity(cfg.Images.GenericAssetKey); err != nil {
				log.Printf("Error setting idle presence: %v", err)
			}
			tray.UpdateStatus("Idle")
		} else {
			log.Printf("[presence] no match among %d process(es)", len(procs))
			if err := rpcMgr.setIdleActivity(cfg.Images.GenericAssetKey); err != nil {
				log.Printf("Error setting idle presence: %v", err)
			}
		}
		state.SetRPCConnected(rpcMgr.isConnected())
		state.SetMatchInfo(profile.MatchInfo{
			Source:      "none",
			RPConnected: rpcMgr.isConnected(),
		})
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
							log.Println("[discord] reconnected")
							state.SetRPCConnected(true)
						} else {
							state.SetRPCConnected(false)
						}
					} else {
						state.SetRPCConnected(false)
					}
				}
			}
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	if showTray {
		go func() {
			<-sigCh
			cleanup(rpcMgr, httpServer, configMgr, procMonitor, reconnectStopCh, catalogStore, catalogRefresher)
			releaseLock()
			os.Exit(0)
		}()

		tray.Run(tray.Actions{
			OpenSettings: func() {
				settingsDialog.UpdateConfig(configMgr.Config())
				settingsDialog.Show()
			},
			ReloadConfig: func() {
				newCfg, err := config.Load(configPath)
				if err == nil {
					applyConfig(newCfg, "tray")
				}
			},
			SetAutoDetect: func(enabled bool) {
				webServer.OnAutoDetectSet(enabled)
			},
			SetOverride: func(p *profile.Profile) {
				webServer.OnOverrideSet(p)
			},
			ClearOverride: func() {
				webServer.OnOverrideClear()
			},
			Quit: func() {
				cleanup(rpcMgr, httpServer, configMgr, procMonitor, reconnectStopCh, catalogStore, catalogRefresher)
				releaseLock()
				os.Exit(0)
			},
		}, cfg.TrayIconPath)
	} else {
		<-sigCh
		cleanup(rpcMgr, httpServer, configMgr, procMonitor, reconnectStopCh, catalogStore, catalogRefresher)
	}
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
	return config.DefaultConfig()
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
		if result != nil && result.IsBetterThan(best) {
			best = result
			bestProc = &procs[i]
		}
	}
	return best, bestProc
}

type presenceWinner struct {
	Profile    *profile.Profile
	proc       *profile.DetectedProcess
	source     string
	reason     string
	confidence int
}

func selectBestPresence(
	ctx context.Context,
	profileMgr *profile.Manager,
	catalogMatcher *catalog.Matcher,
	imgResolver catalog.ImageResolver,
	detection config.DetectionConfig,
	procs []profile.DetectedProcess,
) *presenceWinner {
	profileMatch, profileProc := profileMgr.Match(procs)

	var catResult *catalog.MatchResult
	var catProc *profile.DetectedProcess
	if catalogMatcher != nil {
		catResult, catProc = findBestCatalogMatch(ctx, catalogMatcher, procs)
		// Final safety net: reject catalog matches for excluded apps (browsers,
		// Discord, file managers) even if they somehow entered the catalog.
		if catResult != nil && isExcludedCatalogEntry(catResult.Entry.Title) {
			catResult = nil
			catProc = nil
		}
		// Apply detection filters.
		if catResult != nil {
			switch catResult.Entry.Kind {
			case catalog.EntryKindGame:
				if !detection.ShowGames {
					catResult = nil
					catProc = nil
				}
			case catalog.EntryKindApplication:
				if !detection.ShowTools {
					catResult = nil
					catProc = nil
				}
			}
		}
	}

	if profileMatch == nil && catResult == nil {
		return nil
	}
	if profileMatch == nil {
		p := catResult.ToProfile(imgResolver)
		return &presenceWinner{
			Profile: &p, proc: catProc, source: "catalog",
			reason: catResult.Reason, confidence: catResult.Confidence,
		}
	}
	if catResult == nil {
		return &presenceWinner{
			Profile: profileMatch, proc: profileProc, source: "profile",
			reason: string(profileMatch.Match.Type), confidence: profileMatch.Priority,
		}
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
		return &presenceWinner{
			Profile: &p, proc: catProc, source: "catalog",
			reason: catResult.Reason, confidence: catResult.Confidence,
		}
	}
	return &presenceWinner{
		Profile: profileMatch, proc: profileProc, source: "profile",
		reason: string(profileMatch.Match.Type), confidence: profileMatch.Priority,
	}
}

// isExcludedCatalogEntry returns true for catalog entry titles that should never
// be shown as Rich Presence: browsers, Discord, file managers, etc.
func isExcludedCatalogEntry(title string) bool {
	lower := strings.ToLower(title)
	excludedTitles := []string{
		"discord", "discord canary", "discord ptb", "discord development",
		"firefox", "firefox esr", "firefox developer edition",
		"firefox nightly", "firefox beta",
		"librewolf", "waterfox", "floorp", "palemoon", "basilisk",
		"icecat", "seamonkey",
		"google chrome", "google-chrome", "chromium", "chromium browser",
		"brave", "brave browser", "opera", "microsoft edge", "vivaldi",
		"thorium", "iridium", "ungoogled-chromium",
		"epiphany", "falkon", "midori", "qutebrowser",
		"konqueror", "luakit", "surf", "nyxt", "lagrange", "badwolf",
		"netsurf", "dooble", "tor browser", "torbrowser",
		"zen", "zen browser",
		"dolphin", "nautilus", "nemo", "thunar", "pcmanfm",
		"caja", "spacefm", "krusader", "doublecmd",
		"xfdesktop", "plasmashell", "gnome-shell", "cinnamon",
		// Launchers (safety net — desktop source now filters these)
		"steam", "steam linux", "steam runtime", "steam web helper",
		"epic games launcher", "epicgameslauncher", "heroic games launcher",
		"lutris", "gog galaxy", "itch.io", "itch", "bottles", "playnite",
		// Terminal emulators
		"kitty", "alacritty", "wezterm", "foot", "gnome terminal",
		"konsole", "xfce4-terminal", "lxterminal", "terminator",
		"tilix", "guake", "yakuake", "tilda", "qterminal",
		"st", "xterm", "urxvt", "rxvt", "eterm",
		"hyper", "tabby", "warp", "rio",
		// Shells
		"bash", "zsh", "fish", "sh", "dash", "csh", "tcsh",
		// Screenshot / audio utility tools
		"flameshot", "ksnip", "spectacle", "grim", "slurp",
		"pavucontrol", "volume control", "gnome-screenshot",
	}
	for _, e := range excludedTitles {
		if lower == e {
			return true
		}
	}
	return false
}

func buildRichActivity(p *profile.Profile, proc *profile.DetectedProcess) *rpc.RichActivity {
	act := p.Activity
	if proc != nil {
		act = profile.RenderActivity(act, *proc)
	}

	activity := &rpc.RichActivity{
		Name:     p.Name,
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

func setRichPresence(rm *rpcManager, appID string, p *profile.Profile, proc *profile.DetectedProcess) {
	if err := rm.switchApp(appID); err != nil {
		log.Printf("Error switching Discord app: %v", err)
	}
	activity := buildRichActivity(p, proc)

	// Always store the desired activity so it can be replayed on reconnect.
	if err := rm.setActivity(activity); err != nil {
		log.Printf("Error setting activity: %v, attempting reconnect", err)
		if rerr := rm.connect(); rerr != nil {
			log.Printf("Reconnect failed: %v", rerr)
		}
		// connect() already replays desiredActivity on success;
		// do not send a duplicate here.
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

func acquireLock(pidFile string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(pidFile), 0700); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}

	data, err := os.ReadFile(pidFile)
	if err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil && pid > 0 {
			if process, err := os.FindProcess(pid); err == nil {
				if err := process.Signal(syscall.Signal(0)); err == nil {
					return nil, fmt.Errorf("%d", pid)
				}
			}
		}
	}

	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		return nil, fmt.Errorf("write pid file: %w", err)
	}

	return func() {
		os.Remove(pidFile)
	}, nil
}
