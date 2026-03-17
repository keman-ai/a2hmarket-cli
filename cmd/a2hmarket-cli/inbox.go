package main

// inbox.go — inbox CLI commands (pull, ack, peek, get, check).
// Mirrors JS runtime/js/src/inbox/inbox-service.js.

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/a2a"
	"github.com/keman-ai/a2hmarket-cli/internal/openclaw"
	"github.com/keman-ai/a2hmarket-cli/internal/store"
	"github.com/urfave/cli/v2"
)

// ─────────────────────────────────────────────────────────────────────────────
// Command constructor
// ─────────────────────────────────────────────────────────────────────────────

func inboxCommand() *cli.Command {
	return &cli.Command{
		Name:  "inbox",
		Usage: "Manage the A2A message inbox",
		Subcommands: []*cli.Command{
			{
				Name:   "pull",
				Usage:  "Pull un-consumed events (supports long-polling via --wait)",
				Action: inboxPullCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
					&cli.StringFlag{Name: "consumer-id", Value: "default", Usage: "consumer identifier for ack tracking"},
					&cli.Int64Flag{Name: "cursor", Value: 0, Usage: "return events with seq > cursor"},
					&cli.IntFlag{Name: "limit", Value: 20, Usage: "maximum events to return (1–200)"},
					&cli.IntFlag{Name: "wait", Value: 0, Usage: "long-poll: max seconds to wait for events (0 = no wait)"},
					&cli.StringFlag{Name: "source-session-key", Value: "", Usage: "session key for openclaw consumer peer binding"},
					&cli.StringFlag{Name: "peer-id", Value: "", Usage: "filter events by peer ID (optional)"},
				},
			},
			{
				Name:   "ack",
				Usage:  "Acknowledge (mark consumed) an event",
				Action: inboxAckCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
					&cli.StringFlag{Name: "event-id", Usage: "event ID to ack", Required: true},
					&cli.StringFlag{Name: "consumer-id", Value: "default", Usage: "consumer identifier"},
					&cli.StringFlag{Name: "source-session-key", Value: "", Usage: "session key for reply routing (e.g. agent:feishu:channel:xxx)"},
					&cli.StringFlag{Name: "source-session-id", Value: "", Usage: "session ID for reply routing"},
					&cli.BoolFlag{Name: "notify-external", Value: false, Usage: "enqueue media_outbox notification after ack"},
					&cli.StringFlag{Name: "summary-text", Value: "", Usage: "summary text for external notification"},
					&cli.StringFlag{Name: "media-url", Value: "", Usage: "media URL for external notification (auto-filled from payment_qr if omitted)"},
					&cli.StringFlag{Name: "channel", Value: "", Usage: "external channel (e.g. feishu); auto-inferred from session key if omitted"},
					&cli.StringFlag{Name: "to", Value: "", Usage: "recipient for external notification; auto-inferred from session key if omitted"},
					&cli.StringFlag{Name: "account-id", Value: "", Usage: "account ID for external notification"},
					&cli.StringFlag{Name: "thread-id", Value: "", Usage: "thread ID for external notification"},
				},
			},
			{
				Name:   "peek",
				Usage:  "Return unread count and pending push count",
				Action: inboxPeekCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
					&cli.StringFlag{Name: "consumer-id", Value: "default", Usage: "consumer identifier"},
				},
			},
			{
				Name:   "get",
				Usage:  "Get the full content of a single event",
				Action: inboxGetCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
					&cli.StringFlag{Name: "event-id", Usage: "event ID to retrieve", Required: true},
				},
			},
		{
			Name:   "check",
			Usage:  "Health check: unread count, pending push, listener liveness",
			Action: inboxCheckCmd,
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
				&cli.StringFlag{Name: "consumer-id", Value: "default", Usage: "consumer identifier"},
			},
		},
		inboxHistoryCommand(),
	},
}
}

// ─────────────────────────────────────────────────────────────────────────────
// pull
// ─────────────────────────────────────────────────────────────────────────────

func inboxPullCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))
	ensureListenerRunning(configDir)
	consumerID := normalizeStr(c.String("consumer-id"), "default")
	cursor := c.Int64("cursor")
	limit := clamp(c.Int("limit"), 1, 200, 20)
	waitSec := c.Int("wait")
	sessKey := strings.TrimSpace(c.String("source-session-key"))
	peerID := strings.TrimSpace(c.String("peer-id"))
	isOpenClaw := consumerID == "openclaw"

	es, err := openStore(configDir)
	if err != nil {
		return outputError("inbox.pull", err)
	}
	defer es.Close()

	ctx := context.Background()
	deadline := time.Now().Add(time.Duration(waitSec) * time.Second)

	for {
		events, err := es.PullEvents(ctx, consumerID, cursor, limit, store.PullEventsOpts{PeerID: peerID})
		if err != nil {
			return outputError("inbox.pull", err)
		}

		if len(events) > 0 {
			routeBoundCount := 0
			// OpenClaw consumer: bind peer session during pull for reply routing.
			if isOpenClaw && sessKey != "" {
				for _, ev := range events {
					if ev.PeerID != "" {
						bound, _ := es.BindPeerSessionForEvent(ctx, ev.EventID, "", sessKey, "openclaw", ev.MsgTs)
						if bound {
							routeBoundCount++
						}
					}
				}
			}

			nextCursor := int64(0)
			if len(events) > 0 {
				nextCursor = events[len(events)-1].Seq
			}
			return outputOK("inbox.pull", map[string]interface{}{
				"consumer_id":       consumerID,
				"cursor":            nextCursor,
				"events":            events,
				"count":             len(events),
				"route_bound_count": routeBoundCount,
			})
		}

		if waitSec <= 0 || time.Now().After(deadline) {
			return outputOK("inbox.pull", map[string]interface{}{
				"consumer_id":       consumerID,
				"cursor":            cursor,
				"events":            []interface{}{},
				"count":             0,
				"route_bound_count": 0,
			})
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ack
// ─────────────────────────────────────────────────────────────────────────────

func inboxAckCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))
	eventID := strings.TrimSpace(c.String("event-id"))
	consumerID := normalizeStr(c.String("consumer-id"), "default")
	sessKey := strings.TrimSpace(c.String("source-session-key"))
	sessID := strings.TrimSpace(c.String("source-session-id"))
	notifyExternal := c.Bool("notify-external")
	summaryText := strings.TrimSpace(c.String("summary-text"))
	mediaURL := strings.TrimSpace(c.String("media-url"))
	channel := strings.TrimSpace(c.String("channel"))
	to := strings.TrimSpace(c.String("to"))
	accountID := strings.TrimSpace(c.String("account-id"))
	threadID := strings.TrimSpace(c.String("thread-id"))
	isOpenClaw := consumerID == "openclaw"

	if eventID == "" {
		return outputError("inbox.ack", fmt.Errorf("event_id is required"))
	}

	es, err := openStore(configDir)
	if err != nil {
		return outputError("inbox.ack", err)
	}
	defer es.Close()

	ctx := context.Background()

	// Fetch the event for media_url auto-fill and session key inference.
	event, _ := es.GetEvent(ctx, eventID)

	// Auto-fill media_url from payload.payment_qr when notify_external=true but no explicit media-url.
	mediaURLAutoFilled := false
	if notifyExternal && mediaURL == "" && event != nil {
		if qr := extractPaymentQrFromPayload(event.Payload); qr != "" {
			mediaURL = qr
			mediaURLAutoFilled = true
		}
	}

	doNotify := notifyExternal && (summaryText != "" || mediaURL != "")

	// Route binding: only authoritative for the "openclaw" consumer.
	var rb *store.RouteBinding
	nonAuthoritativeReason := ""
	if isOpenClaw && sessKey != "" {
		rb = &store.RouteBinding{
			SessionID:  "", // openclaw only uses session key
			SessionKey: sessKey,
			Source:     "openclaw",
		}
	} else if !isOpenClaw && sessKey != "" {
		nonAuthoritativeReason = "non_authoritative_consumer"
		// Still accept sessID for non-openclaw consumers (for legacy compatibility).
		if sessID != "" {
			rb = &store.RouteBinding{
				SessionID:  sessID,
				SessionKey: sessKey,
				Source:     "inbox-ack",
			}
		}
	} else if sessID != "" || sessKey != "" {
		rb = &store.RouteBinding{
			SessionID:  sessID,
			SessionKey: sessKey,
			Source:     "inbox-ack",
		}
	}

	ackResult, err := es.AckEvent(ctx, consumerID, eventID, rb)
	if err != nil {
		return outputError("inbox.ack", err)
	}

	// Enqueue media_outbox notification on FIRST ack only.
	summaryEnqueued := false
	summarySkipReason := ""

	if doNotify {
		if !ackResult.Inserted {
			summarySkipReason = "already_acked"
		} else {
			// Infer channel and to from session key if not explicitly set.
			resolvedChannel := channel
			resolvedTo := to
			if resolvedChannel == "" || resolvedTo == "" {
				inferKey := sessKey
				if inferKey == "" && event != nil {
					inferKey = event.TargetSessionKey
				}
				if hints := a2a.ParseDeliveryHintsFromSessionKey(inferKey); hints != nil {
					if resolvedChannel == "" {
						resolvedChannel = hints.Channel
					}
					if resolvedTo == "" {
						resolvedTo = hints.To
					}
				}
			}

			// Fallback: query OpenClaw sessions, pick the most recently active
			// one that has a deliverable channel (e.g. feishu).
			if resolvedChannel == "" || resolvedTo == "" {
				if ds, _ := openclaw.FindMostRecentDeliverableSession(); ds != nil {
					ch, tgt := openclaw.ParseSessionKey(ds.Key)
					if resolvedChannel == "" {
						resolvedChannel = ch
					}
					if resolvedTo == "" {
						resolvedTo = tgt
					}
				}
			}

			if resolvedChannel != "" && resolvedTo != "" {
				inferKey := sessKey
				if inferKey == "" && event != nil {
					inferKey = event.TargetSessionKey
				}
				// Fallback: use session key from the most active deliverable session.
				if inferKey == "" {
					if ds, _ := openclaw.FindMostRecentDeliverableSession(); ds != nil {
						inferKey = ds.Key
					}
				}
				enqResult, enqErr := es.EnqueueMediaOutbox(ctx, store.MediaEnqueueInput{
					EventID:     eventID,
					SessionKey:  inferKey,
					Channel:     resolvedChannel,
					To:          resolvedTo,
					AccountID:   accountID,
					ThreadID:    threadID,
					MessageText: summaryText,
					MediaURL:    mediaURL,
				})
				if enqErr != nil {
					fmt.Fprintf(os.Stderr, "warn: enqueue media_outbox: %v\n", enqErr)
					summarySkipReason = "enqueue_error"
				} else if enqResult.Inserted {
					summaryEnqueued = true
				} else {
					summarySkipReason = enqResult.Reason
				}
			} else {
				summarySkipReason = "no_delivery_target"
			}
		}
	} else if notifyExternal && !doNotify {
		summarySkipReason = "no_summary_text"
	}

	routeBound := ackResult.RouteBound
	routeBindReason := ackResult.RouteBindReason
	if nonAuthoritativeReason != "" {
		routeBound = false
		routeBindReason = nonAuthoritativeReason
	}

	return outputOK("inbox.ack", map[string]interface{}{
		"event_id":             eventID,
		"consumer_id":          consumerID,
		"acked_at":             ackResult.AckedAt,
		"inserted":             ackResult.Inserted,
		"route_bound":          routeBound,
		"route_bind_reason":    omitEmpty(routeBindReason),
		"summary_enqueued":     summaryEnqueued,
		"summary_skip_reason":  omitEmpty(summarySkipReason),
		"media_url_auto_filled": mediaURLAutoFilled,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// peek
// ─────────────────────────────────────────────────────────────────────────────

func inboxPeekCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))
	consumerID := normalizeStr(c.String("consumer-id"), "default")

	es, err := openStore(configDir)
	if err != nil {
		return outputError("inbox.peek", err)
	}
	defer es.Close()

	r, err := es.PeekUnread(context.Background(), consumerID)
	if err != nil {
		return outputError("inbox.peek", err)
	}
	return outputOK("inbox.peek", map[string]interface{}{
		"consumer_id":  consumerID,
		"unread":       r.Unread,
		"pending_push": r.PendingPush,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// get
// ─────────────────────────────────────────────────────────────────────────────

func inboxGetCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))
	eventID := strings.TrimSpace(c.String("event-id"))
	if eventID == "" {
		return outputError("inbox.get", fmt.Errorf("event_id is required"))
	}

	es, err := openStore(configDir)
	if err != nil {
		return outputError("inbox.get", err)
	}
	defer es.Close()

	ev, err := es.GetEvent(context.Background(), eventID)
	if err != nil {
		return outputError("inbox.get", err)
	}
	if ev == nil {
		return outputError("inbox.get", fmt.Errorf("event not found: %s", eventID))
	}

	// Fill delivery target from OpenClaw sessions if not set on the event.
	targetSessionKey := ev.TargetSessionKey
	deliveryChannel := ""
	deliveryTo := ""
	if targetSessionKey != "" {
		deliveryChannel, deliveryTo = openclaw.ParseSessionKey(targetSessionKey)
	}
	if deliveryChannel == "" || deliveryTo == "" {
		if ds, _ := openclaw.FindMostRecentDeliverableSession(); ds != nil {
			if targetSessionKey == "" {
				targetSessionKey = ds.Key
			}
			ch, tgt := openclaw.ParseSessionKey(ds.Key)
			if deliveryChannel == "" {
				deliveryChannel = ch
			}
			if deliveryTo == "" {
				deliveryTo = tgt
			}
		}
	}

	return outputOK("inbox.get", map[string]interface{}{
		"seq":                ev.Seq,
		"event_id":           ev.EventID,
		"peer_id":            ev.PeerID,
		"message_id":         ev.MessageID,
		"msg_ts":             ev.MsgTs,
		"preview":            ev.Preview,
		"state":              ev.State,
		"source":             ev.Source,
		"a2a_message_id":     ev.A2AMessageID,
		"target_session_id":  ev.TargetSessionID,
		"target_session_key": targetSessionKey,
		"delivery_channel":   omitEmpty(deliveryChannel),
		"delivery_to":        omitEmpty(deliveryTo),
		"created_at":         ev.CreatedAt,
		"updated_at":         ev.UpdatedAt,
		"payload":            ev.Payload,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// check
// ─────────────────────────────────────────────────────────────────────────────

func inboxCheckCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))
	consumerID := normalizeStr(c.String("consumer-id"), "default")

	es, err := openStore(configDir)
	if err != nil {
		return outputError("inbox.check", err)
	}
	defer es.Close()

	status, err := es.CheckStatus(context.Background(), consumerID)
	if err != nil {
		return outputError("inbox.check", err)
	}

	listenerAlive := isListenerAlive(pidPath(configDir))

	tsNow := time.Now().UnixMilli()
	var oldestAgeMs *int64
	if status.OldestUnreadAt > 0 {
		age := tsNow - status.OldestUnreadAt
		if age < 0 {
			age = 0
		}
		oldestAgeMs = &age
	}

	// Compose human-readable summary (matches JS version).
	var parts []string
	if status.Unread > 0 {
		ageStr := ""
		if oldestAgeMs != nil {
			ageStr = fmt.Sprintf(" (oldest %ds ago)", *oldestAgeMs/1000)
		}
		parts = append(parts, fmt.Sprintf("%d unread%s", status.Unread, ageStr))
	} else {
		parts = append(parts, "0 unread")
	}
	if status.PendingPush > 0 {
		parts = append(parts, fmt.Sprintf("%d pending push", status.PendingPush))
	}
	if listenerAlive {
		parts = append(parts, "listener running")
	} else {
		parts = append(parts, "listener not running")
	}

	return outputOK("inbox.check", map[string]interface{}{
		"has_pending":          status.Unread > 0,
		"unread_count":         status.Unread,
		"pending_push_count":   status.PendingPush,
		"oldest_unread_age_ms": oldestAgeMs,
		"listener_alive":       listenerAlive,
		"summary":              strings.Join(parts, ", "),
		"consumer_id":          consumerID,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Inbox-local helpers
// ─────────────────────────────────────────────────────────────────────────────

// openStore opens the EventStore for the given config directory.
func openStore(configDir string) (*store.EventStore, error) {
	return store.Open(dbPath(configDir))
}

// isListenerAlive reads the PID file and sends signal 0 to verify the process is running.
func isListenerAlive(pidFilePath string) bool {
	data, err := os.ReadFile(pidFilePath)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// extractPaymentQrFromPayload extracts payment_qr (or legacy image) from event payload.
func extractPaymentQrFromPayload(payload map[string]interface{}) string {
	if payload == nil {
		return ""
	}
	// Try full envelope format (envelope.payload.payment_qr).
	if inner, ok := payload["payload"].(map[string]interface{}); ok {
		if v, ok := inner["payment_qr"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		if v, ok := inner["image"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	// Bare payload format.
	if v, ok := payload["payment_qr"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := payload["image"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return ""
}

// normalizeStr returns fallback when s is empty.
func normalizeStr(s, fallback string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	return s
}

// clamp constrains v to [min, max]; if v == 0 returns def.
func clamp(v, minVal, maxVal, def int) int {
	if v <= 0 {
		v = def
	}
	if v < minVal {
		v = minVal
	}
	if v > maxVal {
		v = maxVal
	}
	return v
}

// omitEmpty returns nil when s is empty (for clean JSON output).
func omitEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
