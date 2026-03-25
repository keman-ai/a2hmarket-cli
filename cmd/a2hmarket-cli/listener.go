package main

// listener.go — listen (debug subscriber) and listener daemon commands.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/a2a"
	"github.com/keman-ai/a2hmarket-cli/internal/common"
	"github.com/keman-ai/a2hmarket-cli/internal/dispatcher"
	"github.com/keman-ai/a2hmarket-cli/internal/lease"
	mqttpkg "github.com/keman-ai/a2hmarket-cli/internal/mqtt"
	"github.com/keman-ai/a2hmarket-cli/internal/openclaw"
	"github.com/keman-ai/a2hmarket-cli/internal/protocol"
	"github.com/keman-ai/a2hmarket-cli/internal/store"
	"github.com/urfave/cli/v2"
)

// ─────────────────────────────────────────────────────────────────────────────
// Command constructors
// ─────────────────────────────────────────────────────────────────────────────

func listenCommand() *cli.Command {
	return &cli.Command{
		Name:   "listen",
		Usage:  "Subscribe to incoming A2A messages and print them (Ctrl+C to stop)",
		Action: listenCmd,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
			&cli.BoolFlag{Name: "verbose", Value: false, Usage: "print full envelope JSON"},
		},
	}
}

func listenerCommand() *cli.Command {
	return &cli.Command{
		Name:  "listener",
		Usage: "Listener daemon management (role-aware multi-instance)",
		Subcommands: []*cli.Command{
			{
				Name:   "run",
				Usage:  "Start the listener daemon (blocks; Ctrl+C to stop)",
				Action: listenerRunCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
					&cli.BoolFlag{Name: "verbose", Value: false, Usage: "print full envelope JSON"},
					&cli.BoolFlag{Name: "push-enabled", Value: false, Usage: "enable push to OpenClaw gateway"},
					&cli.StringFlag{Name: "a2a-shared-secret", Value: "", Usage: "shared secret for A2A envelope signature verification"},
				},
			},
			{
				Name:   "role",
				Usage:  "Show current role of this instance (requires control plane)",
				Action: listenerRoleCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
				},
			},
		{
			Name:   "takeover",
			Usage:  "Explicitly seize the leader role for this instance",
			Action: listenerTakeoverCmd,
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
			},
		},
		{
			Name:   "reload",
			Usage:  "Hot-reload credentials (e.g. push_enabled) without restarting the listener",
			Action: listenerReloadCmd,
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
			},
		},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// listen (simple subscriber, no daemon — for debugging only)
// ─────────────────────────────────────────────────────────────────────────────

func listenCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))
	verbose := c.Bool("verbose")

	creds, err := loadCreds(configDir)
	if err != nil {
		return err
	}

	instanceID, err := store.LoadOrCreateInstanceID(configDir)
	if err != nil {
		return fmt.Errorf("instance-id: %w", err)
	}

	tc := mqttpkg.NewTokenClient(creds.APIURL, creds.AgentID, creds.AgentKey, version)
	transport := mqttpkg.NewTransport(creds.MQTTURL, tc, creds.AgentID, instanceID)

	transport.OnMessage(func(msg mqttpkg.Message) {
		printMessage(msg, verbose)
	})
	transport.OnReconnect(func() {
		common.Infof("reconnected")
	})

	if err := transport.Connect(); err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}
	defer transport.Close()

	if err := transport.Subscribe(); err != nil {
		return fmt.Errorf("mqtt subscribe: %w", err)
	}

	common.Infof("Listening for messages on %s (agent=%s, instance=%s)",
		mqttpkg.IncomingTopic(creds.AgentID), creds.AgentID, instanceID)
	common.Infof("Press Ctrl+C to stop.")

	waitForSignal()
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// listener run (daemon with lease management + SQLite inbox + A2A dispatcher)
// ─────────────────────────────────────────────────────────────────────────────

func listenerRunCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))
	verbose := c.Bool("verbose")
	sharedSecret := c.String("a2a-shared-secret")

	creds, err := loadCreds(configDir)
	if err != nil {
		return err
	}

	// push_enabled: CLI flag --push-enabled 显式传入时优先，否则采用 credentials.json 中的配置项。
	// 默认值为 false（心跳拉取模式），设为 true 时 listener 每条消息到达后立即推送到 OpenClaw。
	pushEnabled := creds.PushEnabled
	if c.IsSet("push-enabled") {
		pushEnabled = c.Bool("push-enabled")
	}

	instanceID, err := store.LoadOrCreateInstanceID(configDir)
	if err != nil {
		return fmt.Errorf("instance-id: %w", err)
	}

	// Open SQLite event store.
	es, err := store.Open(dbPath(configDir))
	if err != nil {
		return fmt.Errorf("open event store: %w", err)
	}
	defer es.Close()

	// Write PID lock file so inbox check can verify listener is alive.
	if err := writePIDFile(pidPath(configDir)); err != nil {
		common.Warnf("could not write PID file: %v", err)
	}
	defer removePIDFile(pidPath(configDir))

	hostname, _ := os.Hostname()
	connClientID := mqttpkg.BuildConnectionClientID(creds.AgentID, instanceID)

	// Acquire lease (best-effort; fall back to standalone).
	// Default forceTakeover=true: last device wins, old instance auto-exits.
	leaseClient := lease.NewClient(creds.APIURL, creds.AgentID, creds.AgentKey)
	acquireReq := lease.AcquireRequest{
		InstanceID:    instanceID,
		ClientID:      connClientID,
		Hostname:      hostname,
		ForceTakeover: true,
	}

	role := lease.RoleStandalone
	var epoch int64
	var leaseUntil int64

	result, acquireErr := leaseClient.Acquire(acquireReq)
	if acquireErr != nil {
		common.Warnf("lease acquire failed (standalone mode): %v", acquireErr)
	} else {
		role = result.Role
		epoch = result.Epoch
		leaseUntil = result.LeaseUntil
		common.Infof("lease acquired role=%s epoch=%d leaseUntil=%d", role, epoch, leaseUntil)
		if result.OldLease != nil && result.OldLease.Hostname != "" {
			fmt.Printf("⚠ 已从 %s 接管 Agent，旧实例将自动退出\n", result.OldLease.Hostname)
		}
	}

	// MQTT transport setup.
	tc := mqttpkg.NewTokenClient(creds.APIURL, creds.AgentID, creds.AgentKey, version)

	var transport *mqttpkg.Transport
	if role == lease.RoleFollower {
		transport = mqttpkg.NewTransport(creds.MQTTURL, tc, creds.AgentID, instanceID)
	} else {
		transport = mqttpkg.NewTransportWithClientID(creds.MQTTURL, tc, creds.AgentID,
			mqttpkg.BuildClientID(creds.AgentID))
		// CleanSession=false enables QoS-1 persistence for the base clientId.
		transport.SetCleanSession(false)
	}

	// Build A2AService and register it as the MQTT message handler.
	routerCfg := a2a.RouterConfig{
		AgentID:         creds.AgentID,
		MQTTTopicPrefix: "P2P_TOPIC",
		A2ASharedSecret: sharedSecret,
		PushEnabled:     pushEnabled,
		PushTarget:      "openclaw",
	}
	a2aSvc := a2a.NewA2AService(es, transport, routerCfg)

	// Register the combined handler: A2AService routing + optional verbose print.
	// Note: we register ONE handler; Start() must not be called separately.
	transport.OnMessage(func(msg mqttpkg.Message) {
		a2aSvc.Route(msg)
		if verbose {
			printMessage(msg, true)
		}
	})

	transport.OnReconnect(func() {
		common.Infof("reconnected — resubscribing")
	})

	// Reconnect guard: leader verifies epoch via heartbeat before reconnecting.
	// Prevents ping-pong when a new leader has taken over.
	shutdownCh := make(chan string, 1)
	if role == lease.RoleLeader {
		transport.OnReconnectGuard(func() bool {
			hb, err := leaseClient.Heartbeat(instanceID, epoch)
			if err != nil {
				// Network error — allow reconnect (may be transient).
				return true
			}
			if !hb.OK {
				common.Warnf("reconnect guard: lease revoked (reason=%s), aborting reconnect", hb.Reason)
				select {
				case shutdownCh <- hb.Reason:
				default:
				}
				return false
			}
			return true
		})
	}

	if err := transport.Connect(); err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}
	defer transport.Close()

	if role != lease.RoleFollower {
		if err := transport.Subscribe(); err != nil {
			return fmt.Errorf("mqtt subscribe: %w", err)
		}
	}

	common.Infof("Listener started  instance=%s  role=%s  agent=%s  push=%v",
		instanceID, role, creds.AgentID, pushEnabled)

	// Heartbeat ticker (15s) — sends heartbeat to lease control plane.
	heartbeatTicker := time.NewTicker(5 * time.Second)
	defer heartbeatTicker.Stop()

	// Follower poll ticker (20s) — follower periodically calls acquire so it
	// can detect when the current leader's lease expires or a takeover targets
	// this instance. When acquire returns leader, exit cleanly so the process
	// manager restarts us in leader mode (fresh clientId + subscribe).
	followerPollTicker := time.NewTicker(20 * time.Second)
	defer followerPollTicker.Stop()

	// Flush ticker — flushes outbox tables when leader.
	flushInterval := creds.Listener.ParseFlushInterval()
	flushTicker := time.NewTicker(flushInterval)
	defer flushTicker.Stop()

	// Update check ticker — periodically check for new CLI version.
	updateCheckInterval := creds.Listener.ParseUpdateCheckInterval()
	updateCheckTicker := time.NewTicker(updateCheckInterval)
	defer updateCheckTicker.Stop()
	common.Infof("update check interval=%s  flush interval=%s", updateCheckInterval, flushInterval)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	reloadCh := make(chan os.Signal, 1)
	reloadEnabled := registerReloadSignal(reloadCh)

	a2aDispatchCfg := dispatcher.A2ADispatchConfig{
		BatchSize:  50,
		MaxRetries: 10,
		MaxDelayMs: 120_000,
	}
	var pushWg sync.WaitGroup
	pushDispatchCfg := dispatcher.PushDispatchConfig{
		BatchSize:  20,
		MaxDelayMs: 300_000,
		WaitGroup:  &pushWg,
	}
	mediaDispatchCfg := dispatcher.MediaDispatchConfig{
		BatchSize:  20,
		MaxRetries: 10,
	}

	for {
		select {
		case <-sigCh:
			common.Infof("Shutting down listener...")
			waitDone := make(chan struct{})
			go func() {
				pushWg.Wait()
				close(waitDone)
			}()
			select {
			case <-waitDone:
				common.Infof("all push goroutines finished")
			case <-time.After(5 * time.Second):
				common.Warnf("push goroutine drain timeout (5s), exiting anyway")
			}
			return nil

		case <-reloadCh:
			if !reloadEnabled {
				continue
			}
			newCreds, err := loadCreds(configDir)
			if err != nil {
				common.Warnf("reload: failed to read credentials: %v", err)
				continue
			}
			pushEnabled = newCreds.PushEnabled
			common.Infof("reload: push_enabled=%v", pushEnabled)

		case reason := <-shutdownCh:
			fmt.Println("Agent 已转移到新设备，当前实例已停止。如需恢复，请重新运行 listener run")
			common.Infof("Shutting down listener (lease revoked via reconnect guard: %s)", reason)
			return nil

		case <-heartbeatTicker.C:
			if role != lease.RoleLeader {
				continue
			}
			hbResult, err := leaseClient.Heartbeat(instanceID, epoch)
			if err != nil {
				common.Warnf("heartbeat failed: %v", err)
				continue
			}
			if !hbResult.OK {
				// Another instance has taken over. Unsubscribe from MQTT so we
				// stop receiving messages immediately, then exit cleanly.
				// The process manager (systemd/supervisor/nohup-restart) will
				// restart us; on the next acquire we'll get role=follower and
				// connect with a suffixed clientId — no more MQTT contention.
				common.Warnf("heartbeat revoked (reason=%s) — unsubscribing and shutting down", hbResult.Reason)
				transport.Unsubscribe()
				fmt.Println("Agent 已转移到新设备，当前实例已停止。如需恢复，请重新运行 listener run")
				common.Infof("Shutting down listener (lease revoked, restart to run as follower)")
				return nil
			}
			epoch = hbResult.Epoch
			leaseUntil = hbResult.LeaseUntil
			common.Debugf("heartbeat ok epoch=%d leaseUntil=%d", epoch, leaseUntil)

		case <-followerPollTicker.C:
			// Only followers poll. Leaders use the heartbeat path above.
			if role != lease.RoleFollower {
				continue
			}
			pollResult, err := leaseClient.Acquire(acquireReq)
			if err != nil {
				common.Debugf("follower poll failed: %v", err)
				continue
			}
			if pollResult.Role == lease.RoleLeader {
				// We won the lease (previous leader expired or takeover targeted us).
				// Exit cleanly so the daemon restarts us in leader mode with the
				// base clientId and an active MQTT subscription.
				common.Infof("follower promoted to leader (epoch=%d) — restarting as leader", pollResult.Epoch)
				return nil
			}
			common.Debugf("follower poll: still follower, leader=%s", pollResult.LeaderInstanceID)

		case <-flushTicker.C:
			// Only the leader (or standalone) flushes outboxes.
			if role == lease.RoleFollower {
				continue
			}

			// Flush A2A outbound messages.
			{
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				stats, err := dispatcher.FlushA2AOutbox(ctx, es, a2aSvc.PublishEnvelope, a2aDispatchCfg)
				cancel()
				if err != nil {
					common.Warnf("a2a flush error: %v", err)
				} else if stats.Sent > 0 || stats.Retried > 0 {
					common.Infof("a2a flush: sent=%d retried=%d skipped=%d", stats.Sent, stats.Retried, stats.Skipped)
				}
			}

			// Flush push_outbox → OpenClaw (only when push_enabled).
			if pushEnabled {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				pushStats, err := dispatcher.FlushPushOutbox(ctx, es, pushDispatchCfg)
				cancel()
				if err != nil {
					common.Warnf("push flush error: %v", err)
				} else if pushStats.SessionUnavailable {
					common.Warnf("push flush: session unavailable, skipped=%d (will retry next tick)", pushStats.Skipped)
				} else if pushStats.Sent > 0 || pushStats.Retried > 0 {
					common.Infof("push flush: sent=%d retried=%d", pushStats.Sent, pushStats.Retried)
				}
			}

			// Flush media_outbox → external channel (e.g. Feishu).
			{
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				mediaStats, err := dispatcher.FlushMediaOutbox(ctx, es, mediaDispatchCfg)
				cancel()
				if err != nil {
					common.Warnf("media flush error: %v", err)
				} else if mediaStats.Sent > 0 || mediaStats.Retried > 0 || mediaStats.Failed > 0 {
					common.Infof("media flush: sent=%d retried=%d failed=%d", mediaStats.Sent, mediaStats.Retried, mediaStats.Failed)
				}
			}

		case <-updateCheckTicker.C:
			checkAndNotifyUpdate()
		}
	}
}

// checkAndNotifyUpdate checks for new CLI and skill versions, notifies via feishu.
func checkAndNotifyUpdate() {
	var notifications []string

	// Check CLI version.
	cliLatest, err := fetchLatestTag()
	if err != nil {
		common.Debugf("update check: failed to fetch CLI latest tag: %v", err)
	} else {
		current := normalizeVersion(version)
		latest := normalizeVersion(cliLatest)
		if isNewer(latest, current) {
			common.Infof("update check: CLI new version %s (current %s)", cliLatest, version)
			// Only notify user for major/minor bumps (e.g. 1.1.x → 1.2.x), not patch-only.
			if isMajorMinorNewer(latest, current) {
				notifications = append(notifications, fmt.Sprintf(
					"🔄 a2hmarket-cli 有新版本\n"+
						"· 当前：%s → 最新：%s\n"+
						"· 更新命令：a2hmarket-cli update", version, cliLatest))
			}
		} else {
			common.Debugf("update check: CLI %s is up to date", version)
		}
	}

	// Check skill version.
	skillDir := filepath.Join(findOpenclawStateDir(), "workspace", "skills", skillName)
	localSkillVer := readSkillVersion(skillDir)
	remoteSkillVer := fetchLatestSkillTag()
	if remoteSkillVer != "" && localSkillVer != "" {
		local := normalizeVersion(localSkillVer)
		remote := normalizeVersion(remoteSkillVer)
		if isNewer(remote, local) {
			common.Infof("update check: skill new version %s (current %s)", remoteSkillVer, localSkillVer)
			if isMajorMinorNewer(remote, local) {
				notifications = append(notifications, fmt.Sprintf(
					"📦 a2hmarket skill 有新版本\n"+
						"· 当前：%s → 最新：%s\n"+
						"· 更新命令：a2hmarket-cli update-skill", localSkillVer, remoteSkillVer))
			}
		} else {
			common.Debugf("update check: skill %s is up to date", localSkillVer)
		}
	} else if remoteSkillVer != "" && localSkillVer == "" {
		common.Debugf("update check: skill not installed locally, skipping")
	}

	if len(notifications) == 0 {
		return
	}

	// Send combined notification.
	ds, err := openclaw.FindMostRecentDeliverableSession()
	if err != nil || ds == nil {
		common.Debugf("update check: no deliverable session, skipping notification")
		return
	}

	msg := strings.Join(notifications, "\n\n")
	ch, tgt := openclaw.ParseSessionKey(ds.Key)
	if err := openclaw.SendMediaToChannel(ch, tgt, msg, ""); err != nil {
		common.Warnf("update check: failed to notify: %v", err)
	} else {
		common.Infof("update check: notified user via %s", ch)
	}
}

// fetchLatestSkillTag fetches the latest release tag for the a2hmarket skill repo.
func fetchLatestSkillTag() string {
	urls := []string{
		fmt.Sprintf("%s/api/repos/keman-ai/a2hmarket/releases/latest", a2hProxy),
		"https://api.github.com/repos/keman-ai/a2hmarket/releases/latest",
	}
	client := &http.Client{Timeout: 15 * time.Second}
	for _, u := range urls {
		resp, err := client.Get(u)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		var rel latestRelease
		if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()
		if rel.TagName != "" {
			return rel.TagName
		}
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// listener role
// ─────────────────────────────────────────────────────────────────────────────

func listenerRoleCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))

	creds, err := loadCreds(configDir)
	if err != nil {
		return err
	}

	instanceID, err := store.LoadOrCreateInstanceID(configDir)
	if err != nil {
		return fmt.Errorf("instance-id: %w", err)
	}

	leaseClient := lease.NewClient(creds.APIURL, creds.AgentID, creds.AgentKey)
	status, err := leaseClient.Status(instanceID)
	if err != nil {
		fmt.Printf("Instance ID:    %s\n", instanceID)
		fmt.Printf("Role:           standalone (control plane unreachable: %v)\n", err)
		return nil
	}

	leaseExp := "—"
	if status.LeaseUntil > 0 {
		t := time.UnixMilli(status.LeaseUntil)
		leaseExp = t.Local().Format(time.DateTime)
	}

	fmt.Printf("Instance ID:    %s\n", instanceID)
	fmt.Printf("Role:           %s\n", status.MyRole)
	fmt.Printf("Leader:         %s\n", orDash(status.LeaderInstanceID))
	fmt.Printf("Epoch:          %d\n", status.Epoch)
	fmt.Printf("Lease until:    %s\n", leaseExp)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// listener takeover
// ─────────────────────────────────────────────────────────────────────────────

func listenerTakeoverCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))

	creds, err := loadCreds(configDir)
	if err != nil {
		return err
	}

	instanceID, err := store.LoadOrCreateInstanceID(configDir)
	if err != nil {
		return fmt.Errorf("instance-id: %w", err)
	}

	hostname, _ := os.Hostname()
	connClientID := mqttpkg.BuildConnectionClientID(creds.AgentID, instanceID)

	leaseClient := lease.NewClient(creds.APIURL, creds.AgentID, creds.AgentKey)
	result, err := leaseClient.Takeover(lease.TakeoverRequest{
		InstanceID: instanceID,
		ClientID:   connClientID,
		Hostname:   hostname,
	})
	if err != nil {
		return fmt.Errorf("takeover failed: %w", err)
	}

	fmt.Printf("Takeover successful!\n")
	fmt.Printf("  Role:           %s\n", result.Role)
	fmt.Printf("  New epoch:      %d\n", result.Epoch)
	fmt.Printf("  Prev leader:    %s\n", orDash(result.PrevLeaderID))
	if result.LeaseUntil > 0 {
		fmt.Printf("  Lease until:    %s\n", time.UnixMilli(result.LeaseUntil).Local().Format(time.DateTime))
	}
	fmt.Println("\nNote: the former leader will demote automatically on its next heartbeat.")
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// listener reload
// ─────────────────────────────────────────────────────────────────────────────

func listenerReloadCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))

	pid, err := readPIDFile(pidPath(configDir))
	if err != nil {
		return fmt.Errorf("listener not running (no PID file): %w", err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process not found (pid=%d): %w", pid, err)
	}

	if err := sendReloadSignal(proc); err != nil {
		return fmt.Errorf("failed to send reload signal (pid=%d): %w", pid, err)
	}

	fmt.Printf("Reload signal sent to listener (pid=%d).\n", pid)
	fmt.Println("Check log with: tail ~/.a2hmarket/store/listener.log")
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Local helpers
// ─────────────────────────────────────────────────────────────────────────────

func printMessage(msg mqttpkg.Message, verbose bool) {
	env, err := protocol.ParseEnvelope(msg.Payload)
	if err != nil {
		common.Debugf("raw topic=%s payload=%s", msg.Topic, trimStr(msg.Payload, 200))
		return
	}

	if verbose {
		out, _ := json.MarshalIndent(env, "", "  ")
		common.Debugf("envelope: %s", string(out))
		return
	}

	text, _ := env.Payload["text"].(string)
	if text != "" {
		common.Debugf("from=%s type=%s msg_id=%s text=%q", env.SenderID, env.MessageType, env.MessageID, text)
	} else {
		common.Debugf("from=%s type=%s msg_id=%s", env.SenderID, env.MessageType, env.MessageID)
	}
}

func waitForSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}

// writePIDFile writes the current process PID to path (creating parent dirs).
func writePIDFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	pid := strconv.Itoa(os.Getpid())
	return os.WriteFile(path, []byte(pid), 0644)
}

// removePIDFile deletes the PID file (best-effort).
func removePIDFile(path string) {
	_ = os.Remove(path)
}
