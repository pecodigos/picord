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
	if cfg.AppID != "" {
		t.Errorf("expected empty app_id, got %q", cfg.AppID)
	}
	if !cfg.ScanAllProcesses {
		t.Error("expected default scan_all_processes to be true")
	}
	if !cfg.Catalog.Enabled {
		t.Error("expected default catalog.enabled to be true")
	}
	if cfg.Images.Mode != "generic" {
		t.Errorf("expected default images.mode generic, got %q", cfg.Images.Mode)
	}
	if cfg.Images.GenericAssetKey != "picord_game" {
		t.Errorf("expected default images.generic_asset_key picord_game, got %q", cfg.Images.GenericAssetKey)
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
