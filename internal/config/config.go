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

type AppConfig struct {
	AppID        string            `yaml:"app_id" json:"app_id"`
	PollInterval int               `yaml:"poll_interval" json:"poll_interval"`
	WebPort      int               `yaml:"web_port" json:"web_port"`
	Profiles     []profile.Profile `yaml:"profiles" json:"profiles"`
}

var defaultConfig = AppConfig{
	AppID:        "1354481585976385573",
	PollInterval: 2,
	WebPort:      17970,
	Profiles:     []profile.Profile{},
}

type Manager struct {
	mu       sync.RWMutex
	path     string
	config   AppConfig
	watcher  *fsnotify.Watcher
	onChange func(AppConfig)
}

func Load(path string) (AppConfig, error) {
	cfg := defaultConfig

	data, err := os.ReadFile(path)
	if err != nil {
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

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	if cfg.PollInterval < 1 {
		cfg.PollInterval = 2
	}
	if cfg.WebPort < 1 || cfg.WebPort > 65535 {
		cfg.WebPort = 17970
	}

	return cfg, nil
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
