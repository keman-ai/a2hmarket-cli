package main

// auth.go — gen-auth-code, get-auth, status, api-call commands.

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/api"
	"github.com/keman-ai/a2hmarket-cli/internal/auth"
	"github.com/keman-ai/a2hmarket-cli/internal/common"
	"github.com/keman-ai/a2hmarket-cli/internal/config"
	"github.com/urfave/cli/v2"
)

// ─────────────────────────────────────────────────────────────────────────────
// Command constructors
// ─────────────────────────────────────────────────────────────────────────────

func genAuthCodeCommand() *cli.Command {
	return &cli.Command{
		Name:   "gen-auth-code",
		Usage:  "Generate temporary auth code with timestamp and MAC address",
		Action: genAuthCodeCmd,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "login-url", Value: "https://a2hmarket.ai", Usage: "Login page base URL"},
			&cli.StringFlag{Name: "auth-api-url", Value: "https://api.qianmiao.life", Usage: "Auth API base URL"},
			&cli.StringFlag{Name: "feishu-user-id", Value: "", Usage: "Feishu user ID"},
		},
	}
}

func getAuthCommand() *cli.Command {
	return &cli.Command{
		Name:   "get-auth",
		Usage:  "Query auth status and fetch credentials if authorized",
		Action: getAuthCmd,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "code", Usage: "auth code to check", Required: true},
			&cli.StringFlag{Name: "base-url", Value: "https://api.qianmiao.life", Usage: "Auth API base URL"},
			&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
			&cli.BoolFlag{Name: "poll", Value: false, Usage: "poll until authorized"},
		},
	}
}

func statusCommand() *cli.Command {
	return &cli.Command{
		Name:   "status",
		Usage:  "Show current authentication status",
		Action: statusCmd,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
		},
	}
}

func apiCallCommand() *cli.Command {
	return &cli.Command{
		Name:   "api-call",
		Usage:  "Call API with signed HTTP request (GET or POST)",
		Action: apiCallCmd,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
			&cli.StringFlag{Name: "method", Value: "GET", Usage: "HTTP method: GET or POST"},
			&cli.StringFlag{Name: "path", Usage: "API path", Required: true},
			&cli.StringFlag{Name: "sign-path", Value: "", Usage: "path used for signing (omit query string)"},
			&cli.StringFlag{Name: "body", Value: "", Usage: "JSON body for POST requests"},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// gen-auth-code
// ─────────────────────────────────────────────────────────────────────────────

func genAuthCodeCmd(c *cli.Context) error {
	authAPIURL := c.String("auth-api-url")
	loginURL := c.String("login-url")
	feishuUserID := c.String("feishu-user-id")

	timestamp := time.Now().Unix()
	macAddr, err := getMACAddress()
	if err != nil {
		macAddr = "unknown"
	}

	common.Infof("Generating auth code: auth_api_url=%s login_url=%s timestamp=%d mac=%s",
		authAPIURL, loginURL, timestamp, macAddr)

	client := &http.Client{Timeout: 30 * time.Second}
	initLoginURL := fmt.Sprintf("%s/v1/auth/init-login", authAPIURL)
	reqBody := fmt.Sprintf("timestamp=%d&mac=%s", timestamp, macAddr)
	if feishuUserID != "" {
		reqBody += fmt.Sprintf("&feishu_user_id=%s", feishuUserID)
	}

	resp, err := client.Post(initLoginURL, "application/x-www-form-urlencoded",
		strings.NewReader(reqBody))
	if err != nil {
		return generateLocalAuthCode(loginURL, timestamp, macAddr)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return generateLocalAuthCode(loginURL, timestamp, macAddr)
	}

	var loginResp auth.InitLoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return generateLocalAuthCode(loginURL, timestamp, macAddr)
	}
	if loginResp.Code == "" {
		return generateLocalAuthCode(loginURL, timestamp, macAddr)
	}

	fmt.Printf("Auth code generated: %s\n", loginResp.Code)
	fmt.Printf("Login URL: %s\n", loginResp.URL)
	fmt.Printf("Timestamp: %d\n", timestamp)
	fmt.Printf("MAC Address: %s\n", macAddr)
	fmt.Println("\nPlease open the URL in PC browser to complete login")
	fmt.Println("Then run 'a2hmarket-cli get-auth --code <code>' to fetch credentials")
	return nil
}

func generateLocalAuthCode(loginURL string, timestamp int64, macAddr string) error {
	rawCode, err := generateAuthCode()
	if err != nil {
		return fmt.Errorf("failed to generate auth code: %w", err)
	}
	cleanMac := strings.ReplaceAll(macAddr, ":", "")
	rawString := fmt.Sprintf("%s_%d_%s", rawCode, timestamp, cleanMac)
	hash := md5.Sum([]byte(rawString))
	authCode := hex.EncodeToString(hash[:])
	url := fmt.Sprintf("%s/authcode?code=%s", loginURL, authCode)

	fmt.Printf("Auth code generated: %s\n", authCode)
	fmt.Printf("Login URL: %s\n", url)
	fmt.Printf("Raw string (before MD5): %s\n", rawString)
	fmt.Println("\nPlease open the URL in PC browser to complete login")
	fmt.Println("Then run 'a2hmarket-cli get-auth --code <code>' to fetch credentials")
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// get-auth
// ─────────────────────────────────────────────────────────────────────────────

func getAuthCmd(c *cli.Context) error {
	code := c.String("code")
	baseURL := c.String("base-url")
	configDir := expandHome(c.String("config-dir"))
	poll := c.Bool("poll")

	common.Infof("Getting auth: code=%s base_url=%s poll=%v", code, baseURL, poll)

	if poll {
		return pollForAuth(code, baseURL, configDir)
	}
	return checkAuthOnce(code, baseURL, configDir)
}

func pollForAuth(code, baseURL, configDir string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	checkURL := fmt.Sprintf("%s/findu-user/api/v1/public/user/agent/auth?code=%s", baseURL, code)

	delay := 2 * time.Second
	maxDelay := 30 * time.Second

	fmt.Println("Starting poll for auth status...")

	for attempt := 1; attempt <= 30; attempt++ {
		resp, err := client.Get(checkURL)
		if err != nil {
			fmt.Printf("Attempt %d: Error - %v\n", attempt, err)
			time.Sleep(delay)
			delay = minDur(delay*2, maxDelay)
			continue
		}

		var authResp auth.CheckAuthResponse
		if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
			resp.Body.Close()
			fmt.Printf("Attempt %d: Parse error - %v\n", attempt, err)
			time.Sleep(delay)
			delay = minDur(delay*2, maxDelay)
			continue
		}
		resp.Body.Close()

		switch authResp.Status {
		case "pending":
			fmt.Printf("Attempt %d: Status: pending - waiting for login...\n", attempt)
			time.Sleep(delay)
			delay = minDur(delay*2, maxDelay)

		case "authorized":
			creds := authResp.GetCredentials()
			if creds == nil {
				return fmt.Errorf("authorized but no credentials received")
			}
			fmt.Println("Status: authorized")
			fmt.Printf("Agent ID: %s\n", creds.AgentID)
			if err := saveCredentials(configDir, creds); err != nil {
				return fmt.Errorf("failed to save credentials: %w", err)
			}
			fmt.Printf("\nCredentials saved to: %s/credentials.json\n", configDir)
			fmt.Println("Authentication successful!")
			return nil

		case "expired":
			return fmt.Errorf("auth code has expired, please re-initiate login")
		case "used":
			return fmt.Errorf("auth code already used")
		case "not_found":
			return fmt.Errorf("auth code not found")
		default:
			fmt.Printf("Attempt %d: Unknown status: %s\n", attempt, authResp.Status)
			time.Sleep(delay)
			delay = minDur(delay*2, maxDelay)
		}
	}
	return fmt.Errorf("polling timeout - please try again")
}

func checkAuthOnce(code, baseURL, configDir string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	checkURL := fmt.Sprintf("%s/findu-user/api/v1/public/user/agent/auth?code=%s", baseURL, code)

	resp, err := client.Get(checkURL)
	if err != nil {
		return fmt.Errorf("failed to check auth status: %w", err)
	}
	defer resp.Body.Close()

	var authResp auth.CheckAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	switch authResp.Status {
	case "pending":
		fmt.Println("Status: pending - Please complete login in PC browser")
		fmt.Println("Hint: Use --poll flag to wait for authorization")
		return nil

	case "authorized":
		creds := authResp.GetCredentials()
		if creds == nil {
			return fmt.Errorf("authorized but no credentials received")
		}
		fmt.Println("Status: authorized")
		fmt.Printf("Agent ID: %s\n", creds.AgentID)
		fmt.Printf("API URL: %s\n", creds.APIURL)
		fmt.Printf("MQTT URL: %s\n", creds.MQTTURL)
		if err := saveCredentials(configDir, creds); err != nil {
			return fmt.Errorf("failed to save credentials: %w", err)
		}
		fmt.Printf("\nCredentials saved to: %s/credentials.json\n", configDir)
		fmt.Println("Authentication successful!")
		return nil

	case "expired":
		return fmt.Errorf("auth code has expired, please re-initiate login")
	case "used":
		return fmt.Errorf("auth code already used")
	case "not_found":
		return fmt.Errorf("auth code not found")
	default:
		return fmt.Errorf("unknown status: %s", authResp.Status)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// status
// ─────────────────────────────────────────────────────────────────────────────

func statusCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))

	credPath := filepath.Join(configDir, "credentials.json")
	creds, err := config.LoadCredentials(credPath)
	if err != nil {
		fmt.Println("Not authenticated")
		fmt.Println("Run 'a2hmarket-cli gen-auth-code' to generate auth code")
		fmt.Println("Run 'a2hmarket-cli get-auth --code <code>' to get credentials")
		return nil
	}

	fmt.Println("Authenticated")
	fmt.Printf("Agent ID:   %s\n", creds.AgentID)
	fmt.Printf("API URL:    %s\n", creds.APIURL)
	fmt.Printf("MQTT URL:   %s\n", creds.MQTTURL)
	fmt.Printf("Expires at: %s\n", creds.ExpireAt)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// api-call
// ─────────────────────────────────────────────────────────────────────────────

func apiCallCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))

	credPath := filepath.Join(configDir, "credentials.json")
	creds, err := config.LoadCredentials(credPath)
	if err != nil {
		return fmt.Errorf("not authenticated: %w", err)
	}

	method := strings.ToUpper(c.String("method"))
	apiPath := c.String("path")
	signPath := c.String("sign-path")
	bodyStr := c.String("body")

	apiCreds := api.Credentials{
		AgentID:  creds.AgentID,
		AgentKey: creds.AgentKey,
		BaseURL:  strings.TrimRight(creds.APIURL, "/"),
	}
	client := api.NewClient(apiCreds, 30*time.Second)

	var result interface{}
	switch method {
	case "GET":
		if err := client.GetJSON(apiPath, signPath, &result); err != nil {
			return fmt.Errorf("api call failed: %w", err)
		}
	case "POST":
		var body interface{}
		if bodyStr != "" {
			if err := json.Unmarshal([]byte(bodyStr), &body); err != nil {
				return fmt.Errorf("invalid JSON body: %w", err)
			}
		}
		if err := client.PostJSON(apiPath, body, &result); err != nil {
			return fmt.Errorf("api call failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported method: %s (only GET and POST supported)", method)
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to format response: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Local helpers (auth-specific)
// ─────────────────────────────────────────────────────────────────────────────

func saveCredentials(dir string, creds *auth.Credentials) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	configData := config.CredentialsConfig{
		AgentID:   creds.AgentID,
		AgentKey:  creds.AgentKey,
		APIURL:    creds.APIURL,
		MQTTURL:   creds.MQTTURL,
		ExpiresAt: creds.ExpireAt,
	}
	data, err := json.MarshalIndent(configData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "credentials.json"), data, 0600)
}

func generateAuthCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func getMACAddress() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if mac := iface.HardwareAddr.String(); mac != "" {
			return mac, nil
		}
	}
	return "", fmt.Errorf("no valid MAC address found")
}

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
