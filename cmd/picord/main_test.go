package main

import "testing"

func TestDefaultConfigEnablesScanAllProcesses(t *testing.T) {
	cfg := defaultConfig()
	if !cfg.ScanAllProcesses {
		t.Fatal("expected fallback default config to enable scan_all_processes")
	}
}
