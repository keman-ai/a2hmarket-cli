package main

// doctor.go — doctor command: run all pre-condition checks in one shot.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/keman-ai/a2hmarket-cli/internal/config"
	mqttpkg "github.com/keman-ai/a2hmarket-cli/internal/mqtt"
	"github.com/urfave/cli/v2"
)

// ─────────────────────────────────────────────────────────────────────────────
// Command constructor
// ─────────────────────────────────────────────────────────────────────────────

func doctorCommand() *cli.Command {
	return &cli.Command{
		Name:   "doctor",
		Usage:  "Run all pre-condition checks and report status",
		Action: doctorCmd,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// doctor action
// ─────────────────────────────────────────────────────────────────────────────

func doctorCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))

	checks := make([]map[string]interface{}, 0, 6)
	allPassed := true

	// 1. binary_version
	checks = append(checks, checkBinaryVersion())

	// 2. credentials
	credCheck, creds := checkCredentials(configDir)
	checks = append(checks, credCheck)
	if credCheck["status"] == "fail" {
		allPassed = false
	}

	hasCreds := creds != nil

	// 3. credentials_permissions
	permCheck := checkCredentialsPermissions(configDir)
	checks = append(checks, permCheck)
	if permCheck["status"] == "fail" {
		allPassed = false
	}

	// 4. listener
	listenerCheck := checkListener(configDir, hasCreds)
	checks = append(checks, listenerCheck)
	if listenerCheck["status"] == "fail" {
		allPassed = false
	}

	// 5. mqtt_connectivity
	mqttCheck := checkMQTTConnectivity(creds, hasCreds)
	checks = append(checks, mqttCheck)
	if mqttCheck["status"] == "fail" {
		allPassed = false
	}

	// 6. database
	dbCheck := checkDatabase(configDir, hasCreds)
	checks = append(checks, dbCheck)
	if dbCheck["status"] == "fail" {
		allPassed = false
	}

	return outputOK("doctor", map[string]interface{}{
		"all_passed": allPassed,
		"checks":     checks,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Individual check functions
// ─────────────────────────────────────────────────────────────────────────────

func checkBinaryVersion() map[string]interface{} {
	return map[string]interface{}{
		"name":   "binary_version",
		"status": "ok",
		"value":  version,
	}
}

func checkCredentials(configDir string) (map[string]interface{}, *config.Credentials) {
	credPath := filepath.Join(configDir, "credentials.json")
	creds, err := config.LoadCredentials(credPath)
	if err != nil {
		return map[string]interface{}{
			"name":    "credentials",
			"status":  "fail",
			"message": fmt.Sprintf("cannot load credentials: %v", err),
		}, nil
	}
	if strings.TrimSpace(creds.AgentID) == "" {
		return map[string]interface{}{
			"name":    "credentials",
			"status":  "fail",
			"message": "agent_id is empty",
		}, nil
	}
	return map[string]interface{}{
		"name":     "credentials",
		"status":   "ok",
		"agent_id": creds.AgentID,
	}, creds
}

func checkCredentialsPermissions(configDir string) map[string]interface{} {
	credPath := filepath.Join(configDir, "credentials.json")
	fi, err := os.Stat(credPath)
	if err != nil {
		return map[string]interface{}{
			"name":    "credentials_permissions",
			"status":  "skip",
			"message": "credentials file not found",
		}
	}
	perm := fi.Mode().Perm()
	permStr := fmt.Sprintf("%04o", perm)
	if perm == 0600 {
		return map[string]interface{}{
			"name":   "credentials_permissions",
			"status": "ok",
			"value":  permStr,
		}
	}
	return map[string]interface{}{
		"name":    "credentials_permissions",
		"status":  "warn",
		"message": fmt.Sprintf("permissions %s, recommend 0600", permStr),
	}
}

func checkListener(configDir string, hasCreds bool) map[string]interface{} {
	if !hasCreds {
		return map[string]interface{}{
			"name":    "listener",
			"status":  "skip",
			"message": "skipped (no credentials)",
		}
	}
	pidFile := pidPath(configDir)
	pid, err := readPIDFile(pidFile)
	if err != nil {
		return map[string]interface{}{
			"name":    "listener",
			"status":  "fail",
			"message": "listener not running (no PID file)",
		}
	}
	if !isListenerAlive(pidFile) {
		return map[string]interface{}{
			"name":    "listener",
			"status":  "fail",
			"message": fmt.Sprintf("listener not running (stale PID %d)", pid),
		}
	}
	return map[string]interface{}{
		"name":   "listener",
		"status": "ok",
		"pid":    pid,
	}
}

func checkMQTTConnectivity(creds *config.Credentials, hasCreds bool) map[string]interface{} {
	if !hasCreds {
		return map[string]interface{}{
			"name":    "mqtt_connectivity",
			"status":  "skip",
			"message": "skipped (no credentials)",
		}
	}
	tc := mqttpkg.NewTokenClient(creds.APIURL, creds.AgentID, creds.AgentKey, version)
	clientID := mqttpkg.BuildClientID(creds.AgentID)
	_, err := tc.GetToken(clientID, true)
	if err != nil {
		return map[string]interface{}{
			"name":    "mqtt_connectivity",
			"status":  "fail",
			"message": fmt.Sprintf("token request failed: %v", err),
		}
	}
	return map[string]interface{}{
		"name":   "mqtt_connectivity",
		"status": "ok",
	}
}

func checkDatabase(configDir string, hasCreds bool) map[string]interface{} {
	if !hasCreds {
		return map[string]interface{}{
			"name":    "database",
			"status":  "skip",
			"message": "skipped (no credentials)",
		}
	}
	dbFile := dbPath(configDir)
	fi, err := os.Stat(dbFile)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]interface{}{
				"name":    "database",
				"status":  "warn",
				"message": "database file does not exist yet (will be created on first listener run)",
				"path":    dbFile,
			}
		}
		return map[string]interface{}{
			"name":    "database",
			"status":  "fail",
			"message": fmt.Sprintf("cannot access database: %v", err),
			"path":    dbFile,
		}
	}
	if fi.IsDir() {
		return map[string]interface{}{
			"name":    "database",
			"status":  "fail",
			"message": "database path is a directory, not a file",
			"path":    dbFile,
		}
	}
	return map[string]interface{}{
		"name":   "database",
		"status": "ok",
		"path":   dbFile,
	}
}
