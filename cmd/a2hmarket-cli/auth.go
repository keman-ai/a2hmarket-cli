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
			&cli.StringFlag{Name: "auth-api-url", Value: "https://web.a2hmarket.ai", Usage: "Auth API base URL"},
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
			&cli.StringFlag{Name: "base-url", Value: "https://web.a2hmarket.ai", Usage: "Auth API base URL"},
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

	fmt.Printf("Auth code: %s\n", loginResp.Code)
	fmt.Println("Please open the URL in PC browser to complete login, then run 'a2hmarket-cli get-auth --code <code>' to fetch credentials")
	fmt.Println()
	fmt.Println(loginResp.URL)
	return nil
}

func generateLocalAuthCode(loginURL string, timestamp int64, macAddr string) error {
	rawCode, err := generateAuthCode()
	if err != nil {
		return outputError("gen-auth-code", fmt.Errorf("failed to generate auth code: %w", err))
	}
	cleanMac := strings.ReplaceAll(macAddr, ":", "")
	rawString := fmt.Sprintf("%s_%d_%s", rawCode, timestamp, cleanMac)
	hash := md5.Sum([]byte(rawString))
	authCode := hex.EncodeToString(hash[:])
	url := fmt.Sprintf("%s/authcode?code=%s", loginURL, authCode)

	fmt.Printf("Auth code: %s\n", authCode)
	fmt.Println("Please open the URL in PC browser to complete login, then run 'a2hmarket-cli get-auth --code <code>' to fetch credentials")
	fmt.Println()
	fmt.Println(url)
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

	common.Infof("Starting poll for auth status...")

	for attempt := 1; attempt <= 30; attempt++ {
		resp, err := client.Get(checkURL)
		if err != nil {
			common.Warnf("Attempt %d: Error - %v", attempt, err)
			time.Sleep(delay)
			delay = minDur(delay*2, maxDelay)
			continue
		}

		var authResp auth.CheckAuthResponse
		if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
			resp.Body.Close()
			common.Warnf("Attempt %d: Parse error - %v", attempt, err)
			time.Sleep(delay)
			delay = minDur(delay*2, maxDelay)
			continue
		}
		resp.Body.Close()

		if !authResp.IsSuccess() {
			return outputError("get-auth", fmt.Errorf("server error (code=%s): %s", authResp.Code, authResp.Message))
		}
		if authResp.IsAuthorized() {
			creds := authResp.Data
			if creds.APIURL == "" {
				creds.APIURL = defaultAPIURL
			}
			if creds.MQTTURL == "" {
				creds.MQTTURL = defaultMQTTURL
			}
			if err := saveCredentials(configDir, creds); err != nil {
				return outputError("get-auth", fmt.Errorf("failed to save credentials: %w", err))
			}
			ensureListenerRunning(configDir)
			return outputOK("get-auth", map[string]interface{}{
				"status":           "authorized",
				"agent_id":         creds.AgentID,
				"api_url":          creds.APIURL,
				"mqtt_url":         creds.MQTTURL,
				"config_dir":       configDir,
				"listener_running": isListenerAlive(pidPath(configDir)),
			})
		}
		// code==200 但 data 为空 → 用户尚未扫码/授权
		common.Infof("Attempt %d: pending - waiting for login...", attempt)
		time.Sleep(delay)
		delay = minDur(delay*2, maxDelay)
	}
	return outputError("get-auth", fmt.Errorf("polling timeout - please try again"))
}

func checkAuthOnce(code, baseURL, configDir string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	checkURL := fmt.Sprintf("%s/findu-user/api/v1/public/user/agent/auth?code=%s", baseURL, code)

	resp, err := client.Get(checkURL)
	if err != nil {
		return outputError("get-auth", fmt.Errorf("failed to check auth status: %w", err))
	}
	defer resp.Body.Close()

	var authResp auth.CheckAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return outputError("get-auth", fmt.Errorf("failed to parse response: %w", err))
	}

	if !authResp.IsSuccess() {
		return outputError("get-auth", fmt.Errorf("server error (code=%s): %s", authResp.Code, authResp.Message))
	}
	if authResp.IsAuthorized() {
		creds := authResp.Data
		// 先补默认值再打印，保持显示与保存一致
		if creds.APIURL == "" {
			creds.APIURL = defaultAPIURL
		}
		if creds.MQTTURL == "" {
			creds.MQTTURL = defaultMQTTURL
		}
		if err := saveCredentials(configDir, creds); err != nil {
			return outputError("get-auth", fmt.Errorf("failed to save credentials: %w", err))
		}
		ensureListenerRunning(configDir)
		return outputOK("get-auth", map[string]interface{}{
			"status":           "authorized",
			"agent_id":         creds.AgentID,
			"api_url":          creds.APIURL,
			"mqtt_url":         creds.MQTTURL,
			"config_dir":       configDir,
			"listener_running": isListenerAlive(pidPath(configDir)),
		})
	}
	// code==200 但 data 为空 → 用户尚未授权
	return outputOK("get-auth", map[string]interface{}{
		"status": "pending",
		"hint":   "Please complete login in PC browser. Use --poll flag to wait for authorization.",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// status
// ─────────────────────────────────────────────────────────────────────────────

func statusCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))

	credPath := filepath.Join(configDir, "credentials.json")
	creds, err := config.LoadCredentials(credPath)
	if err != nil {
		return outputOK("status", map[string]interface{}{
			"authenticated": false,
			"hint":          "Run 'a2hmarket-cli gen-auth-code' to generate auth code, then 'a2hmarket-cli get-auth --code <code>' to get credentials",
		})
	}

	return outputOK("status", map[string]interface{}{
		"authenticated": true,
		"agent_id":      creds.AgentID,
		"api_url":       creds.APIURL,
		"mqtt_url":      creds.MQTTURL,
		"expires_at":    creds.ExpireAt,
	})
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

const (
	defaultAPIURL  = "https://api.a2hmarket.ai"
	defaultMQTTURL = "mqtts://post-cn-e4k4o78q702.mqtt.aliyuncs.com:8883"
)

func saveCredentials(dir string, creds *auth.Credentials) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	apiURL := creds.APIURL
	if apiURL == "" {
		apiURL = defaultAPIURL
	}
	mqttURL := creds.MQTTURL
	if mqttURL == "" {
		mqttURL = defaultMQTTURL
	}
	configData := config.CredentialsConfig{
		AgentID:   creds.AgentID,
		AgentKey:  creds.AgentKey,
		APIURL:    apiURL,
		MQTTURL:   mqttURL,
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

