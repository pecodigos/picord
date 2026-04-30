package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"

	"github.com/pecodigos/picord/internal/profile"
)

type CatalogConfig struct {
	Enabled      bool     `yaml:"enabled" json:"enabled"`
	AutoRefresh  bool     `yaml:"auto_refresh" json:"auto_refresh"`
	Sources      []string `yaml:"sources" json:"sources"`
	RefreshHours int      `yaml:"refresh_hours" json:"refresh_hours"`
}

type ImageConfig struct {
	Mode              string `yaml:"mode" json:"mode"`
	CacheEnabled      bool   `yaml:"cache_enabled" json:"cache_enabled"`
	MaxCacheMB        int    `yaml:"max_cache_mb" json:"max_cache_mb"`
	GenericAssetKey   string `yaml:"generic_asset_key" json:"generic_asset_key"`
	ExternalValidated bool   `yaml:"external_validated" json:"external_validated"`
}

type DiscordApp struct {
	ID   string `yaml:"id" json:"id"`
	Name string `yaml:"name" json:"name"`
}

type AppConfig struct {
	AppID            string                `yaml:"app_id" json:"app_id"`
	PollInterval     int                   `yaml:"poll_interval" json:"poll_interval"`
	WebPort          int                   `yaml:"web_port" json:"web_port"`
	ScanAllProcesses bool                  `yaml:"scan_all_processes" json:"scan_all_processes"`
	Profiles         []profile.Profile     `yaml:"profiles" json:"profiles"`
	Catalog          CatalogConfig         `yaml:"catalog" json:"catalog"`
	Images           ImageConfig           `yaml:"images" json:"images"`
	DiscordApps      map[string]DiscordApp `yaml:"discord_apps,omitempty" json:"discord_apps,omitempty"`
}

const DefaultDiscordAppID = "1499058229571752148"

var DefaultCatalogSources = []string{"steam_local", "steam_shortcuts", "desktop"}

var defaultConfig = AppConfig{
	AppID:            DefaultDiscordAppID,
	PollInterval:     2,
	WebPort:          17970,
	ScanAllProcesses: true,
	Profiles:         []profile.Profile{},
	Catalog: CatalogConfig{
		Enabled:      true,
		AutoRefresh:  true,
		Sources:      DefaultCatalogSources,
		RefreshHours: 24,
	},
	Images: ImageConfig{
		Mode:              "external_url",
		CacheEnabled:      true,
		MaxCacheMB:        512,
		GenericAssetKey:   "picord",
		ExternalValidated: true,
	},
	DiscordApps: map[string]DiscordApp{
		"main": {ID: DefaultDiscordAppID, Name: "Picord"},
	},
}

func defaultConfigCopy() AppConfig {
	cfg := defaultConfig
	cfg.Catalog.Sources = append([]string(nil), defaultConfig.Catalog.Sources...)
	cfg.DiscordApps = make(map[string]DiscordApp, len(defaultConfig.DiscordApps))
	for key, app := range defaultConfig.DiscordApps {
		cfg.DiscordApps[key] = app
	}
	return cfg
}

func defaultConfigForUnmarshal() AppConfig {
	cfg := defaultConfigCopy()
	// yaml.v3 merges into existing maps. Keep this nil so user config replaces
	// discord_apps instead of inheriting the built-in main app entry.
	cfg.DiscordApps = nil
	return cfg
}

type Manager struct {
	mu       sync.RWMutex
	path     string
	config   AppConfig
	watcher  *fsnotify.Watcher
	onChange func(AppConfig)
}

func Load(path string) (AppConfig, error) {
	cfg := defaultConfigForUnmarshal()

	data, err := os.ReadFile(path)
	if err != nil {
		cfg = defaultConfigCopy()
		if os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return cfg, fmt.Errorf("create config dir: %w", err)
			}
			if saveErr := Save(path, cfg); saveErr != nil {
				return cfg, fmt.Errorf("save default config: %w", saveErr)
			}
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	_, hasAppID := raw["app_id"]

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	if cfg.PollInterval < 1 {
		cfg.PollInterval = 2
	}
	if cfg.WebPort < 1 || cfg.WebPort > 65535 {
		cfg.WebPort = 17970
	}

	// Fresh installs and old generated configs should be ready to run with the
	// public Picord Discord application. A user-provided app_id still wins.
	if !hasAppID && len(cfg.DiscordApps) > 0 {
		if app, ok := cfg.DiscordApps["main"]; ok && app.ID != "" {
			cfg.AppID = app.ID
		}
	}
	if cfg.AppID == "" {
		if app, ok := cfg.DiscordApps["main"]; ok && app.ID != "" {
			cfg.AppID = app.ID
		} else {
			cfg.AppID = DefaultDiscordAppID
		}
	}

	// Backward compatibility: if discord_apps is empty but app_id is set,
	// create a "main" entry so the multi-app logic works uniformly.
	if len(cfg.DiscordApps) == 0 && cfg.AppID != "" {
		cfg.DiscordApps = map[string]DiscordApp{
			"main": {ID: cfg.AppID, Name: "Picord"},
		}
	}
	if cfg.AppID == "" {
		if app, ok := cfg.DiscordApps["main"]; ok {
			cfg.AppID = app.ID
		}
	}

	return cfg, nil
}

// ResolveDiscordApp returns the Discord app ID for the given app key.
// If the key is empty, it falls back to "main". If no mapping exists,
// it falls back to the legacy AppID field, or empty string.
func (cfg AppConfig) ResolveDiscordApp(key string) string {
	if key == "" {
		key = "main"
	}
	if app, ok := cfg.DiscordApps[key]; ok {
		return app.ID
	}
	if key == "main" {
		return cfg.AppID
	}
	return ""
}

func Save(path string, cfg AppConfig) error {
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func NewManager(path string, onChange func(AppConfig)) (*Manager, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}

	if err := watcher.Add(filepath.Dir(path)); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("watch config dir: %w", err)
	}

	m := &Manager{
		path:     path,
		config:   cfg,
		watcher:  watcher,
		onChange: onChange,
	}

	go m.watch()

	return m, nil
}

func (m *Manager) Config() AppConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

func (m *Manager) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Save(m.path, m.config)
}

func (m *Manager) UpdateProfiles(profiles []profile.Profile) error {
	m.mu.Lock()
	m.config.Profiles = profiles
	m.mu.Unlock()
	return m.Save()
}

func (m *Manager) Update(cfg AppConfig) error {
	m.mu.Lock()
	m.config = cfg
	m.mu.Unlock()
	return m.Save()
}

func (m *Manager) watch() {
	for {
		select {
		case event, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			if event.Name != m.path {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				cfg, err := Load(m.path)
				if err != nil {
					continue
				}
				m.mu.Lock()
				m.config = cfg
				m.mu.Unlock()
				if m.onChange != nil {
					m.onChange(cfg)
				}
			}
		case err, ok := <-m.watcher.Errors:
			if !ok || err == nil {
				continue
			}
		}
	}
}

func (m *Manager) Close() {
	m.watcher.Close()
}
