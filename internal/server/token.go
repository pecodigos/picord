package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

const tokenFileName = "api-token"

// GenerateToken creates a random 32-byte hex token and persists it to stateDir.
func GenerateToken(stateDir string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(b)
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, tokenFileName), []byte(token), 0600); err != nil {
		return "", fmt.Errorf("write token: %w", err)
	}
	return token, nil
}

// LoadToken reads the persisted token from stateDir.
func LoadToken(stateDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, tokenFileName))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// TokenStateDir returns the default state directory for the token.
func TokenStateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "picord")
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		return filepath.Join(home, ".local", "state", "picord")
	}
	return ".picord-state"
}
