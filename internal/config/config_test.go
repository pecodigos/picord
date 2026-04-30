package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pecodigos/picord/internal/profile"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := AppConfig{
		AppID:            "123456",
		PollInterval:     5,
		WebPort:          8080,
		ScanAllProcesses: false,
		Profiles: []profile.Profile{
			{Name: "test", Enabled: true, Priority: 5, Match: profile.MatchRule{Type: profile.MatchProcessName, Value: "firefox"}},
		},
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if loaded.AppID != cfg.AppID {
		t.Errorf("app_id mismatch: got %q, want %q", loaded.AppID, cfg.AppID)
	}
	if loaded.PollInterval != cfg.PollInterval {
		t.Errorf("poll_interval mismatch: got %d, want %d", loaded.PollInterval, cfg.PollInterval)
	}
	if loaded.WebPort != cfg.WebPort {
		t.Errorf("web_port mismatch: got %d, want %d", loaded.WebPort, cfg.WebPort)
	}
	if loaded.ScanAllProcesses != cfg.ScanAllProcesses {
		t.Errorf("scan_all_processes mismatch: got %v, want %v", loaded.ScanAllProcesses, cfg.ScanAllProcesses)
	}
	if len(loaded.Profiles) != 1 || loaded.Profiles[0].Name != "test" {
		t.Errorf("profiles mismatch: got %+v", loaded.Profiles)
	}
}

func TestLoad_DefaultsOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if cfg.PollInterval != 2 {
		t.Errorf("expected default poll_interval 2, got %d", cfg.PollInterval)
	}
	if cfg.WebPort != 17970 {
		t.Errorf("expected default web_port 17970, got %d", cfg.WebPort)
	}
	if cfg.AppID != DefaultDiscordAppID {
		t.Errorf("expected default app_id %q, got %q", DefaultDiscordAppID, cfg.AppID)
	}
	if cfg.ResolveDiscordApp("main") != DefaultDiscordAppID {
		t.Errorf("expected main Discord app %q, got %q", DefaultDiscordAppID, cfg.ResolveDiscordApp("main"))
	}
	if !cfg.ScanAllProcesses {
		t.Error("expected default scan_all_processes to be true")
	}
	if !cfg.Catalog.Enabled {
		t.Error("expected default catalog.enabled to be true")
	}
	if cfg.Images.Mode != "external_url" {
		t.Errorf("expected default images.mode external_url, got %q", cfg.Images.Mode)
	}
	if !cfg.Images.ExternalValidated {
		t.Error("expected default images.external_validated to be true")
	}
	if cfg.Images.GenericAssetKey != "picord" {
		t.Errorf("expected default images.generic_asset_key picord, got %q", cfg.Images.GenericAssetKey)
	}
}

func TestLoad_EmptyAppIDUsesPicordDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte("app_id: \"\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if cfg.AppID != DefaultDiscordAppID {
		t.Errorf("expected empty app_id to use Picord default %q, got %q", DefaultDiscordAppID, cfg.AppID)
	}
	if cfg.ResolveDiscordApp("main") != DefaultDiscordAppID {
		t.Errorf("expected main Discord app %q, got %q", DefaultDiscordAppID, cfg.ResolveDiscordApp("main"))
	}
}

func TestLoad_DiscordAppsMainBackfillsLegacyAppID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	data := []byte("discord_apps:\n  main:\n    id: custom-main\n    name: Custom\n")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if cfg.AppID != "custom-main" {
		t.Errorf("expected app_id backfilled from discord_apps.main, got %q", cfg.AppID)
	}
}

func TestLoad_BackwardCompatAppID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte("app_id: legacy123\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if cfg.AppID != "legacy123" {
		t.Errorf("expected legacy app_id preserved, got %q", cfg.AppID)
	}
	if len(cfg.DiscordApps) != 1 {
		t.Fatalf("expected 1 discord_app entry for backward compat, got %d", len(cfg.DiscordApps))
	}
	if cfg.DiscordApps["main"].ID != "legacy123" {
		t.Errorf("expected main app ID legacy123, got %q", cfg.DiscordApps["main"].ID)
	}
}

func TestResolveDiscordApp(t *testing.T) {
	cfg := AppConfig{
		AppID: "legacy123",
		DiscordApps: map[string]DiscordApp{
			"main":  {ID: "main456"},
			"steam": {ID: "steam789"},
		},
	}

	if got := cfg.ResolveDiscordApp(""); got != "main456" {
		t.Errorf("expected empty key to resolve to main, got %q", got)
	}
	if got := cfg.ResolveDiscordApp("main"); got != "main456" {
		t.Errorf("expected main key to resolve to main456, got %q", got)
	}
	if got := cfg.ResolveDiscordApp("steam"); got != "steam789" {
		t.Errorf("expected steam key to resolve to steam789, got %q", got)
	}
	if got := cfg.ResolveDiscordApp("unknown"); got != "" {
		t.Errorf("expected unknown key to resolve to empty, got %q", got)
	}
}

func TestResolveDiscordApp_LegacyFallback(t *testing.T) {
	cfg := AppConfig{AppID: "legacy123"}

	if got := cfg.ResolveDiscordApp("main"); got != "legacy123" {
		t.Errorf("expected main to fall back to legacy app_id, got %q", got)
	}
}

func TestLoad_PartialConfigKeepsScanAllDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte("app_id: abc123\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if !cfg.ScanAllProcesses {
		t.Error("expected omitted scan_all_processes to default to true")
	}
}

func TestLoad_ExplicitScanAllFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte("scan_all_processes: false\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if cfg.ScanAllProcesses {
		t.Error("expected explicit scan_all_processes false to be preserved")
	}
}

func TestLoad_Validation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Write invalid values
	data := []byte("poll_interval: 0\nweb_port: 99999\n")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if cfg.PollInterval != 2 {
		t.Errorf("expected poll_interval clamped to 2, got %d", cfg.PollInterval)
	}
	if cfg.WebPort != 17970 {
		t.Errorf("expected web_port clamped to 17970, got %d", cfg.WebPort)
	}
}

func TestLoad_BackwardCompatShowTrayIcon(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte("show_tray_icon: false\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if cfg.ShowTrayIcon {
		t.Error("expected show_tray_icon=false to be preserved from config")
	}
}

func TestLoad_DefaultShowTrayIcon(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if !cfg.ShowTrayIcon {
		t.Error("expected default show_tray_icon to be true")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte("not: valid: yaml: ["), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}
