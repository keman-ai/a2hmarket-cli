package main

// auth_email.go — register, login, reset-password commands (email-based).

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/auth"
	"github.com/keman-ai/a2hmarket-cli/internal/common"
	"github.com/urfave/cli/v2"
	"golang.org/x/term"
)

// ─────────────────────────────────────────────────────────────────────────────
// Command constructors
// ─────────────────────────────────────────────────────────────────────────────

func registerCommand() *cli.Command {
	return &cli.Command{
		Name:   "register",
		Usage:  "Register a new agent account with email",
		Action: registerCmd,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "email", Usage: "email address", Required: true},
			&cli.StringFlag{Name: "base-url", Value: "https://web.a2hmarket.ai", Usage: "API base URL"},
			&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
		},
	}
}

func loginCommand() *cli.Command {
	return &cli.Command{
		Name:   "login",
		Usage:  "Login with email and password",
		Action: loginCmd,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "email", Usage: "email address", Required: true},
			&cli.StringFlag{Name: "base-url", Value: "https://web.a2hmarket.ai", Usage: "API base URL"},
			&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
		},
	}
}

func resetPasswordCommand() *cli.Command {
	return &cli.Command{
		Name:   "reset-password",
		Usage:  "Reset password via email verification code",
		Action: resetPasswordCmd,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "email", Usage: "email address", Required: true},
			&cli.StringFlag{Name: "base-url", Value: "https://web.a2hmarket.ai", Usage: "API base URL"},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// register
// ─────────────────────────────────────────────────────────────────────────────

func registerCmd(c *cli.Context) error {
	email := c.String("email")
	baseURL := strings.TrimRight(c.String("base-url"), "/")
	configDir := expandHome(c.String("config-dir"))

	// 1. Prompt for password (no echo)
	password, err := promptPassword("Enter password: ")
	if err != nil {
		return outputError("register", fmt.Errorf("failed to read password: %w", err))
	}

	// 2. Send verification code
	common.Infof("Sending verification code to %s", email)
	sendCodeURL := fmt.Sprintf("%s/findu-user/api/v1/public/user/agent/email/send-code", baseURL)
	sendCodeBody := map[string]string{
		"email": email,
		"type":  "register",
	}
	if err := postJSON(sendCodeURL, sendCodeBody, nil); err != nil {
		return outputError("register", fmt.Errorf("failed to send verification code: %w", err))
	}
	fmt.Println("Verification code sent to your email.")

	// 3. Prompt for verification code
	code, err := promptLine("Enter verification code: ")
	if err != nil {
		return outputError("register", fmt.Errorf("failed to read verification code: %w", err))
	}

	// 4. Call register API
	registerURL := fmt.Sprintf("%s/findu-user/api/v1/public/user/agent/email/register", baseURL)
	registerBody := map[string]string{
		"email":    email,
		"password": password,
		"code":     code,
	}
	var resp emailAuthResponse
	if err := postJSON(registerURL, registerBody, &resp); err != nil {
		return outputError("register", fmt.Errorf("register failed: %w", err))
	}
	if !resp.IsSuccess() {
		return outputError("register", fmt.Errorf("register failed (code=%s): %s", resp.Code, resp.Message))
	}

	// 5. Save credentials
	if err := saveEmailAuthCredentials(configDir, resp.Data); err != nil {
		return outputError("register", fmt.Errorf("failed to save credentials: %w", err))
	}
	ensureListenerRunning(configDir)

	// 6. Output JSON result
	return outputOK("register", map[string]interface{}{
		"status":           "registered",
		"agent_id":         resp.Data.AgentID,
		"config_dir":       configDir,
		"listener_running": isListenerAlive(pidPath(configDir)),
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// login
// ─────────────────────────────────────────────────────────────────────────────

func loginCmd(c *cli.Context) error {
	email := c.String("email")
	baseURL := strings.TrimRight(c.String("base-url"), "/")
	configDir := expandHome(c.String("config-dir"))

	// 1. Prompt for password (no echo)
	password, err := promptPassword("Enter password: ")
	if err != nil {
		return outputError("login", fmt.Errorf("failed to read password: %w", err))
	}

	// 2. Call login API
	common.Infof("Logging in with email %s", email)
	loginURL := fmt.Sprintf("%s/findu-user/api/v1/public/user/agent/email/login", baseURL)
	loginBody := map[string]string{
		"email":    email,
		"password": password,
	}
	var resp emailAuthResponse
	if err := postJSON(loginURL, loginBody, &resp); err != nil {
		return outputError("login", fmt.Errorf("login failed: %w", err))
	}
	if !resp.IsSuccess() {
		return outputError("login", fmt.Errorf("login failed (code=%s): %s", resp.Code, resp.Message))
	}

	// 3. Save credentials
	if err := saveEmailAuthCredentials(configDir, resp.Data); err != nil {
		return outputError("login", fmt.Errorf("failed to save credentials: %w", err))
	}
	ensureListenerRunning(configDir)

	// 4. Output JSON result
	return outputOK("login", map[string]interface{}{
		"status":           "logged_in",
		"agent_id":         resp.Data.AgentID,
		"config_dir":       configDir,
		"listener_running": isListenerAlive(pidPath(configDir)),
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// reset-password
// ─────────────────────────────────────────────────────────────────────────────

func resetPasswordCmd(c *cli.Context) error {
	email := c.String("email")
	baseURL := strings.TrimRight(c.String("base-url"), "/")

	// 1. Send verification code (type=reset)
	common.Infof("Sending password reset code to %s", email)
	sendCodeURL := fmt.Sprintf("%s/findu-user/api/v1/public/user/agent/email/send-code", baseURL)
	sendCodeBody := map[string]string{
		"email": email,
		"type":  "reset",
	}
	if err := postJSON(sendCodeURL, sendCodeBody, nil); err != nil {
		return outputError("reset-password", fmt.Errorf("failed to send verification code: %w", err))
	}
	fmt.Println("Verification code sent to your email.")

	// 2. Prompt for verification code
	code, err := promptLine("Enter verification code: ")
	if err != nil {
		return outputError("reset-password", fmt.Errorf("failed to read verification code: %w", err))
	}

	// 3. Prompt for new password (no echo)
	password, err := promptPassword("Enter new password: ")
	if err != nil {
		return outputError("reset-password", fmt.Errorf("failed to read password: %w", err))
	}

	// 4. Call reset-password API
	resetURL := fmt.Sprintf("%s/findu-user/api/v1/public/user/agent/email/reset-password", baseURL)
	resetBody := map[string]string{
		"email":    email,
		"password": password,
		"code":     code,
	}
	var resp emailAuthResponse
	if err := postJSON(resetURL, resetBody, &resp); err != nil {
		return outputError("reset-password", fmt.Errorf("reset password failed: %w", err))
	}
	if !resp.IsSuccess() {
		return outputError("reset-password", fmt.Errorf("reset password failed (code=%s): %s", resp.Code, resp.Message))
	}

	// 5. Output result
	return outputOK("reset-password", map[string]interface{}{
		"status":  "password_reset",
		"message": "Password has been reset successfully. Please login with your new password.",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers (email auth specific)
// ─────────────────────────────────────────────────────────────────────────────

// emailAuthResponse is the common response format for email auth APIs.
type emailAuthResponse struct {
	Code    string           `json:"code"`
	Message string           `json:"message"`
	Data    *auth.Credentials `json:"data,omitempty"`
}

func (r *emailAuthResponse) IsSuccess() bool {
	return r.Code == "200"
}

// saveEmailAuthCredentials saves agentId/secret returned from email auth APIs.
func saveEmailAuthCredentials(configDir string, creds *auth.Credentials) error {
	if creds == nil {
		return fmt.Errorf("no credentials in response")
	}
	if creds.APIURL == "" {
		creds.APIURL = defaultAPIURL
	}
	if creds.MQTTURL == "" {
		creds.MQTTURL = defaultMQTTURL
	}
	return saveCredentials(configDir, creds)
}

// promptPassword reads a password from the terminal without echoing.
func promptPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	b, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // newline after hidden input
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// promptLine reads a single line of input from stdin.
func promptLine(prompt string) (string, error) {
	fmt.Print(prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text()), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no input")
}

// postJSON sends a POST request with JSON body and decodes the response.
func postJSON(url string, body interface{}, result interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
	}
	return nil
}
