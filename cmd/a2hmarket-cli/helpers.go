package main

// helpers.go — shared helpers for all command files.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/api"
	"github.com/keman-ai/a2hmarket-cli/internal/config"
)

// buildAPIClient builds an api.Client from a loaded Credentials value.
func buildAPIClient(creds *config.Credentials) *api.Client {
	return api.NewClient(api.Credentials{
		AgentID:  creds.AgentID,
		AgentKey: creds.AgentKey,
		BaseURL:  strings.TrimRight(creds.APIURL, "/"),
	}, 30*time.Second)
}

// outputOK prints a JSON success envelope to stdout and returns nil.
// Mirrors JS outputOk().
func outputOK(action string, data interface{}) error {
	out, err := json.MarshalIndent(map[string]interface{}{
		"ok":     true,
		"action": action,
		"data":   data,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal output: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

// outputError prints a JSON error envelope to stdout and returns a sentinel error.
// Mirrors JS outputError().
func outputError(action string, err error) error {
	out, _ := json.MarshalIndent(map[string]interface{}{
		"ok":     false,
		"action": action,
		"error":  err.Error(),
	}, "", "  ")
	fmt.Println(string(out))
	// Return a short sentinel — the caller should propagate this to trigger os.Exit(1).
	return fmt.Errorf("%s failed", action)
}

// loadCreds reads credentials from configDir/credentials.json.
func loadCreds(configDir string) (*config.Credentials, error) {
	credPath := filepath.Join(configDir, "credentials.json")
	creds, err := config.LoadCredentials(credPath)
	if err != nil {
		return nil, fmt.Errorf("not authenticated — run 'a2hmarket-cli get-auth' first: %w", err)
	}
	return creds, nil
}

// expandHome replaces a leading ~ with $HOME.
func expandHome(p string) string {
	return strings.ReplaceAll(p, "~", os.Getenv("HOME"))
}

// orDash returns s if non-empty, otherwise "—".
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// trimStr truncates s to max bytes, appending "..." if truncated.
func trimStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// dbPath returns the path to the SQLite database file inside configDir.
func dbPath(configDir string) string {
	return filepath.Join(configDir, "store", "a2hmarket_listener.db")
}

// pidPath returns the path to the listener PID file inside configDir.
func pidPath(configDir string) string {
	return filepath.Join(configDir, "store", "listener.pid")
}
