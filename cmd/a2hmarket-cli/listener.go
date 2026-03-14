package main

// listener.go — listen (debug subscriber) and listener daemon commands.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/a2a"
	"github.com/keman-ai/a2hmarket-cli/internal/common"
	"github.com/keman-ai/a2hmarket-cli/internal/dispatcher"
	"github.com/keman-ai/a2hmarket-cli/internal/lease"
	mqttpkg "github.com/keman-ai/a2hmarket-cli/internal/mqtt"
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

	tc := mqttpkg.NewTokenClient(creds.APIURL, creds.AgentID, creds.AgentKey)
	transport := mqttpkg.NewTransport(creds.MQTTURL, tc, creds.AgentID, instanceID)

	transport.OnMessage(func(msg mqttpkg.Message) {
		printMessage(msg, verbose)
	})
	transport.OnReconnect(func() {
		fmt.Println("[reconnected]")
	})

	if err := transport.Connect(); err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}
	defer transport.Close()

	if err := transport.Subscribe(); err != nil {
		return fmt.Errorf("mqtt subscribe: %w", err)
	}

	fmt.Printf("Listening for messages on %s (agent=%s, instance=%s)\n",
		mqttpkg.IncomingTopic(creds.AgentID), creds.AgentID, instanceID)
	fmt.Println("Press Ctrl+C to stop.")

	waitForSignal()
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// listener run (daemon with lease management + SQLite inbox + A2A dispatcher)
// ─────────────────────────────────────────────────────────────────────────────

func listenerRunCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))
	verbose := c.Bool("verbose")
	pushEnabled := c.Bool("push-enabled")
	sharedSecret := c.String("a2a-shared-secret")

	creds, err := loadCreds(configDir)
	if err != nil {
		return err
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
	leaseClient := lease.NewClient(creds.APIURL, creds.AgentID, creds.AgentKey)
	acquireReq := lease.AcquireRequest{
		InstanceID: instanceID,
		ClientID:   connClientID,
		Hostname:   hostname,
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
	}

	// MQTT transport setup.
	tc := mqttpkg.NewTokenClient(creds.APIURL, creds.AgentID, creds.AgentKey)

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
		fmt.Println("[reconnected — resubscribing]")
	})

	if err := transport.Connect(); err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}
	defer transport.Close()

	if role != lease.RoleFollower {
		if err := transport.Subscribe(); err != nil {
			return fmt.Errorf("mqtt subscribe: %w", err)
		}
	}

	fmt.Printf("Listener started  instance=%s  role=%s  agent=%s  push=%v\n",
		instanceID, role, creds.AgentID, pushEnabled)

	// Heartbeat ticker (15s) — sends heartbeat to lease control plane.
	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()

	// Flush ticker (5s) — flushes a2a_outbox when leader.
	flushTicker := time.NewTicker(5 * time.Second)
	defer flushTicker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	dispatchCfg := dispatcher.A2ADispatchConfig{
		BatchSize:  50,
		MaxRetries: 10,
		MaxDelayMs: 120_000,
	}

	for {
		select {
		case <-sigCh:
			fmt.Println("\nShutting down listener...")
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
				common.Warnf("heartbeat revoked (reason=%s) — demoting to follower", hbResult.Reason)
				role = lease.RoleFollower
				continue
			}
			epoch = hbResult.Epoch
			leaseUntil = hbResult.LeaseUntil
			common.Debugf("heartbeat ok epoch=%d leaseUntil=%d", epoch, leaseUntil)

		case <-flushTicker.C:
			// Only the leader (or standalone) flushes the A2A outbox.
			if role == lease.RoleFollower {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			stats, err := dispatcher.FlushA2AOutbox(ctx, es, a2aSvc.PublishEnvelope, dispatchCfg)
			cancel()
			if err != nil {
				common.Warnf("a2a flush error: %v", err)
			} else if stats.Sent > 0 || stats.Retried > 0 {
				common.Infof("a2a flush: sent=%d retried=%d skipped=%d", stats.Sent, stats.Retried, stats.Skipped)
			}
		}
	}
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

	if !result.OK {
		fmt.Println("Takeover failed — already leader or control plane error")
		return nil
	}

	fmt.Printf("Takeover successful!\n")
	fmt.Printf("  New epoch:      %d\n", result.Epoch)
	fmt.Printf("  Prev leader:    %s\n", orDash(result.PrevLeaderID))
	fmt.Printf("  Lease until:    %s\n", time.UnixMilli(result.LeaseUntil).Local().Format(time.DateTime))
	fmt.Println("\nNote: the former leader will demote automatically on its next heartbeat.")
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Local helpers
// ─────────────────────────────────────────────────────────────────────────────

func printMessage(msg mqttpkg.Message, verbose bool) {
	env, err := protocol.ParseEnvelope(msg.Payload)
	if err != nil {
		fmt.Printf("[%s] raw topic=%s payload=%s\n",
			time.Now().Format(time.TimeOnly), msg.Topic, trimStr(msg.Payload, 200))
		return
	}

	ts := time.Now().Format(time.DateTime)
	if verbose {
		out, _ := json.MarshalIndent(env, "", "  ")
		fmt.Printf("[%s] envelope:\n%s\n", ts, string(out))
		return
	}

	text, _ := env.Payload["text"].(string)
	fmt.Printf("[%s] from=%s type=%s msg_id=%s", ts, env.SenderID, env.MessageType, env.MessageID)
	if text != "" {
		fmt.Printf(" text=%q", text)
	}
	fmt.Println()
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
