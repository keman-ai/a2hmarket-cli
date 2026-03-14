package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCredentials_IsExpired_WhenExpired(t *testing.T) {
	creds := &Credentials{
		AgentID:  "ag_test",
		AgentKey: "test_key",
		ExpireAt: time.Now().Add(-1 * time.Hour), // 1 hour ago
	}

	if !creds.IsExpired() {
		t.Error("IsExpired() = false, want true for past expiry time")
	}
}

func TestCredentials_IsExpired_WhenNotExpired(t *testing.T) {
	creds := &Credentials{
		AgentID:  "ag_test",
		AgentKey: "test_key",
		ExpireAt: time.Now().Add(24 * time.Hour), // 24 hours from now
	}

	if creds.IsExpired() {
		t.Error("IsExpired() = true, want false for future expiry time")
	}
}

func TestCredentials_IsExpired_ZeroTime(t *testing.T) {
	creds := &Credentials{
		AgentID:  "ag_test",
		AgentKey: "test_key",
		ExpireAt: time.Time{}, // zero value
	}

	// Zero time is in the past, should be expired
	if !creds.IsExpired() {
		t.Error("IsExpired() = false, want true for zero time")
	}
}

func TestCredentials_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, "credentials.json")

	expireAt := time.Date(2027, 12, 31, 23, 59, 59, 0, time.UTC)
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	original := &Credentials{
		AgentID:   "ag_t6PowP7DhseW8oBl",
		AgentKey:  "GdLTcvnUbwyDbxZlAy6DKHAa5EeVrN5K",
		APIURL:    "https://api.a2hmarket.ai",
		MQTTURL:   "mqtts://mqtt.a2hmarket.ai:8883",
		ExpireAt:  expireAt,
		CreatedAt: createdAt,
	}

	// Save
	err := original.Save(credPath)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file exists
	info, err := os.Stat(credPath)
	if err != nil {
		t.Fatalf("file not found after Save(): %v", err)
	}

	// Verify file permissions (0600)
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %o, want 0600", info.Mode().Perm())
	}

	// Verify file content is valid JSON
	data, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var parsed Credentials
	err = json.Unmarshal(data, &parsed)
	if err != nil {
		t.Fatalf("JSON unmarshal error = %v", err)
	}

	if parsed.AgentID != original.AgentID {
		t.Errorf("AgentID = %q, want %q", parsed.AgentID, original.AgentID)
	}
	if parsed.AgentKey != original.AgentKey {
		t.Errorf("AgentKey = %q, want %q", parsed.AgentKey, original.AgentKey)
	}
	if parsed.APIURL != original.APIURL {
		t.Errorf("APIURL = %q, want %q", parsed.APIURL, original.APIURL)
	}
	if parsed.MQTTURL != original.MQTTURL {
		t.Errorf("MQTTURL = %q, want %q", parsed.MQTTURL, original.MQTTURL)
	}
}

func TestLoadCredentials_Success(t *testing.T) {
	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, "credentials.json")

	// Write a CredentialsConfig-format file (the format LoadCredentials expects)
	cfg := CredentialsConfig{
		AgentID:   "ag_t6PowP7DhseW8oBl",
		AgentKey:  "GdLTcvnUbwyDbxZlAy6DKHAa5EeVrN5K",
		APIURL:    "https://api.a2hmarket.ai",
		MQTTURL:   "mqtts://mqtt.a2hmarket.ai:8883",
		ExpiresAt: "2027-12-31T23:59:59Z",
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	err = os.WriteFile(credPath, data, 0600)
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Load
	loaded, err := LoadCredentials(credPath)
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}

	if loaded.AgentID != cfg.AgentID {
		t.Errorf("AgentID = %q, want %q", loaded.AgentID, cfg.AgentID)
	}
	if loaded.AgentKey != cfg.AgentKey {
		t.Errorf("AgentKey = %q, want %q", loaded.AgentKey, cfg.AgentKey)
	}
	if loaded.APIURL != cfg.APIURL {
		t.Errorf("APIURL = %q, want %q", loaded.APIURL, cfg.APIURL)
	}
	if loaded.MQTTURL != cfg.MQTTURL {
		t.Errorf("MQTTURL = %q, want %q", loaded.MQTTURL, cfg.MQTTURL)
	}

	expectedExpire := time.Date(2027, 12, 31, 23, 59, 59, 0, time.UTC)
	if !loaded.ExpireAt.Equal(expectedExpire) {
		t.Errorf("ExpireAt = %v, want %v", loaded.ExpireAt, expectedExpire)
	}
}

func TestLoadCredentials_FileNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, "nonexistent.json")

	_, err := LoadCredentials(credPath)
	if err == nil {
		t.Fatal("LoadCredentials() should return error for nonexistent file")
	}
}

func TestLoadCredentials_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, "invalid.json")

	err := os.WriteFile(credPath, []byte("not valid json {{{"), 0600)
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err = LoadCredentials(credPath)
	if err == nil {
		t.Fatal("LoadCredentials() should return error for invalid JSON")
	}
}

func TestLoadCredentials_EmptyExpiresAt(t *testing.T) {
	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, "credentials.json")

	cfg := CredentialsConfig{
		AgentID:   "ag_test",
		AgentKey:  "test_key",
		APIURL:    "https://api.example.com",
		MQTTURL:   "mqtts://mqtt.example.com:8883",
		ExpiresAt: "", // empty
	}

	data, _ := json.Marshal(cfg)
	err := os.WriteFile(credPath, data, 0600)
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	loaded, err := LoadCredentials(credPath)
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}

	// ExpireAt should be zero time when ExpiresAt is empty
	if !loaded.ExpireAt.IsZero() {
		t.Errorf("ExpireAt should be zero when ExpiresAt is empty, got %v", loaded.ExpireAt)
	}
}

func TestDeleteCredentials_ExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, "credentials.json")

	// Create the file first
	err := os.WriteFile(credPath, []byte("{}"), 0600)
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Delete
	err = DeleteCredentials(credPath)
	if err != nil {
		t.Fatalf("DeleteCredentials() error = %v", err)
	}

	// Verify file is gone
	_, err = os.Stat(credPath)
	if !os.IsNotExist(err) {
		t.Error("file should not exist after DeleteCredentials()")
	}
}

func TestDeleteCredentials_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, "nonexistent.json")

	// Should not error when file doesn't exist
	err := DeleteCredentials(credPath)
	if err != nil {
		t.Fatalf("DeleteCredentials() should not error for nonexistent file, got %v", err)
	}
}

func TestCredentialsConfig_JSONTags(t *testing.T) {
	cfg := CredentialsConfig{
		AgentID:   "test_id",
		AgentKey:  "test_key",
		APIURL:    "https://api.example.com",
		MQTTURL:   "mqtts://mqtt.example.com",
		ExpiresAt: "2027-12-31T23:59:59Z",
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	if err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	expectedKeys := []string{"agent_id", "agent_key", "api_url", "mqtt_url", "expires_at"}
	for _, key := range expectedKeys {
		if _, ok := parsed[key]; !ok {
			t.Errorf("JSON key %q not found in serialized output", key)
		}
	}
}
