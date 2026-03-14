// Package store provides local persistence utilities.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	instanceIDFile    = "instance-id"
	instanceIDPattern = `^[0-9a-f]{16}$`
)

var instanceIDRe = regexp.MustCompile(instanceIDPattern)

// LoadOrCreateInstanceID loads the persisted instance ID from stateDir or
// creates a new 16-char hex ID and persists it.
//
// stateDir is typically ~/.a2hmarket.  The file is stored at
// {stateDir}/store/instance-id with mode 0600.
func LoadOrCreateInstanceID(stateDir string) (string, error) {
	storeDir := filepath.Join(stateDir, "store")
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		return "", fmt.Errorf("instance-id: mkdir %s: %w", storeDir, err)
	}

	filePath := filepath.Join(storeDir, instanceIDFile)

	if existing, err := os.ReadFile(filePath); err == nil {
		id := strings.TrimSpace(string(existing))
		if instanceIDRe.MatchString(id) {
			return id, nil
		}
	}

	// Generate new 16-char hex (8 random bytes)
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("instance-id: rand: %w", err)
	}
	id := hex.EncodeToString(b)

	if err := os.WriteFile(filePath, []byte(id+"\n"), 0600); err != nil {
		return "", fmt.Errorf("instance-id: write %s: %w", filePath, err)
	}

	return id, nil
}
