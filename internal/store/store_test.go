package store

import (
	"context"
	"testing"
)

// openMemory opens an in-memory SQLite database for testing.
func openMemory(t *testing.T) *EventStore {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open :memory: failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertIncomingEvent_Basic(t *testing.T) {
	s := openMemory(t)
	ctx := context.Background()

	input := InsertEventInput{
		EventID:     "evt_001",
		PeerID:      "agent_alice",
		MessageID:   "msg_001",
		MsgTs:       1000,
		Hash:        "hash_001",
		UnreadCount: 1,
		Preview:     "Hello",
		Payload:     map[string]interface{}{"text": "Hello"},
		State:       "NEW",
		Source:      "MQTT",
	}

	res, err := s.InsertIncomingEvent(ctx, input)
	if err != nil {
		t.Fatalf("InsertIncomingEvent: %v", err)
	}
	if !res.Created {
		t.Errorf("expected Created=true, got false (reason=%s)", res.Reason)
	}
	if res.EventID != "evt_001" {
		t.Errorf("expected EventID=evt_001, got %s", res.EventID)
	}
}

func TestInsertIncomingEvent_Dedup(t *testing.T) {
	s := openMemory(t)
	ctx := context.Background()

	input := InsertEventInput{
		EventID: "evt_dup",
		PeerID:  "agent_alice",
		Hash:    "hash_dup",
		State:   "NEW",
		Source:  "MQTT",
	}

	_, err := s.InsertIncomingEvent(ctx, input)
	if err != nil {
		t.Fatal(err)
	}

	// Insert same event_id again.
	res2, err := s.InsertIncomingEvent(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Created {
		t.Error("expected Created=false on duplicate event_id")
	}
	if res2.Reason != "event_exists" {
		t.Errorf("expected reason=event_exists, got %s", res2.Reason)
	}
}

func TestInsertIncomingEvent_PushOutbox(t *testing.T) {
	s := openMemory(t)
	ctx := context.Background()

	input := InsertEventInput{
		EventID:     "evt_push",
		PeerID:      "agent_bob",
		Hash:        "hash_push",
		State:       "NEW",
		Source:      "MQTT",
		PushEnabled: true,
		PushTarget:  "openclaw",
	}

	res, err := s.InsertIncomingEvent(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created {
		t.Errorf("expected Created=true")
	}

	// Verify push_outbox row was created.
	rows, err := s.ListPendingPushOutbox(ctx, nowMs(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].EventID != "evt_push" {
		t.Errorf("expected 1 push_outbox row for evt_push, got %v", rows)
	}
}

func TestGetEvent(t *testing.T) {
	s := openMemory(t)
	ctx := context.Background()

	input := InsertEventInput{
		EventID: "evt_get",
		PeerID:  "peer_x",
		Hash:    "hashX",
		Preview: "preview text",
		State:   "NEW",
		Source:  "MQTT",
		Payload: map[string]interface{}{"text": "hi"},
	}
	if _, err := s.InsertIncomingEvent(ctx, input); err != nil {
		t.Fatal(err)
	}

	ev, err := s.GetEvent(ctx, "evt_get")
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.PeerID != "peer_x" {
		t.Errorf("PeerID mismatch: %s", ev.PeerID)
	}
	if ev.Preview != "preview text" {
		t.Errorf("Preview mismatch: %s", ev.Preview)
	}
	if p, ok := ev.Payload["text"].(string); !ok || p != "hi" {
		t.Errorf("Payload mismatch: %v", ev.Payload)
	}

	// Non-existent event returns nil, not error.
	ev2, err := s.GetEvent(ctx, "no_such")
	if err != nil {
		t.Fatal(err)
	}
	if ev2 != nil {
		t.Errorf("expected nil for missing event, got %+v", ev2)
	}
}

func TestPullEvents_CursorAndAck(t *testing.T) {
	s := openMemory(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		_, err := s.InsertIncomingEvent(ctx, InsertEventInput{
			EventID: "evt_" + string(rune('0'+i)),
			PeerID:  "peer",
			Hash:    "h" + string(rune('0'+i)),
			State:   "NEW",
			Source:  "MQTT",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Pull all 3 events.
	events, err := s.PullEvents(ctx, "default", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// Ack the first event.
	_, err = s.AckEvent(ctx, "default", "evt_1", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Pull again — should return only 2.
	events2, err := s.PullEvents(ctx, "default", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events2) != 2 {
		t.Errorf("expected 2 events after ack, got %d", len(events2))
	}
}

func TestPeekUnread(t *testing.T) {
	s := openMemory(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		s.InsertIncomingEvent(ctx, InsertEventInput{
			EventID: "evt_" + string(rune('a'+i)),
			PeerID:  "p",
			Hash:    "h" + string(rune('a'+i)),
			State:   "NEW",
			Source:  "MQTT",
		})
	}

	r, err := s.PeekUnread(ctx, "consumer1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Unread != 5 {
		t.Errorf("expected 5 unread, got %d", r.Unread)
	}

	// Ack 2 events.
	s.AckEvent(ctx, "consumer1", "evt_a", nil)
	s.AckEvent(ctx, "consumer1", "evt_b", nil)

	r2, err := s.PeekUnread(ctx, "consumer1")
	if err != nil {
		t.Fatal(err)
	}
	if r2.Unread != 3 {
		t.Errorf("expected 3 unread, got %d", r2.Unread)
	}
}

func TestEnqueueA2aOutbox(t *testing.T) {
	s := openMemory(t)
	ctx := context.Background()

	env := map[string]interface{}{"message_type": "chat.request", "payload": map[string]interface{}{"text": "hi"}}
	res, err := s.EnqueueA2aOutbox(ctx, EnqueueA2aInput{
		MessageID:     "msg_outbox_1",
		TraceID:       "trace_1",
		TargetAgentID: "agent_bob",
		MessageType:   "chat.request",
		QoS:           1,
		Envelope:      env,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created {
		t.Error("expected Created=true")
	}

	// Duplicate.
	res2, err := s.EnqueueA2aOutbox(ctx, EnqueueA2aInput{
		MessageID:     "msg_outbox_1",
		TargetAgentID: "agent_bob",
		MessageType:   "chat.request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Created {
		t.Error("expected Created=false on duplicate")
	}
}

func TestA2aOutboxDispatchCycle(t *testing.T) {
	s := openMemory(t)
	ctx := context.Background()

	env := map[string]interface{}{"msg": "test"}
	s.EnqueueA2aOutbox(ctx, EnqueueA2aInput{
		MessageID:     "dispatch_1",
		TargetAgentID: "agent_x",
		MessageType:   "chat.request",
		Envelope:      env,
	})

	rows, err := s.ListPendingA2aOutbox(ctx, nowMs(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 pending row, got %d", len(rows))
	}

	// Mark sent.
	if err := s.MarkA2aOutboxSent(ctx, rows[0].ID); err != nil {
		t.Fatalf("MarkA2aOutboxSent: %v", err)
	}

	// Should be empty now.
	rows2, _ := s.ListPendingA2aOutbox(ctx, nowMs(), 10)
	if len(rows2) != 0 {
		t.Errorf("expected 0 pending after sent, got %d", len(rows2))
	}
}

func TestA2aOutboxRetry(t *testing.T) {
	s := openMemory(t)
	ctx := context.Background()

	s.EnqueueA2aOutbox(ctx, EnqueueA2aInput{
		MessageID:     "retry_1",
		TargetAgentID: "agent_y",
		MessageType:   "chat.request",
	})

	rows, _ := s.ListPendingA2aOutbox(ctx, nowMs(), 10)
	if len(rows) != 1 {
		t.Fatal("expected 1 row")
	}

	// Mark retry with far-future next_retry_at.
	futureMs := nowMs() + 60_000
	s.MarkA2aOutboxRetry(ctx, rows[0].ID, 1, futureMs, "connection refused")

	// Should not be returned (not due yet).
	rows2, _ := s.ListPendingA2aOutbox(ctx, nowMs(), 10)
	if len(rows2) != 0 {
		t.Errorf("expected 0 due rows (retry in future), got %d", len(rows2))
	}

	// Should be returned when we advance time.
	rows3, _ := s.ListPendingA2aOutbox(ctx, futureMs+1, 10)
	if len(rows3) != 1 {
		t.Errorf("expected 1 due row after advancing time, got %d", len(rows3))
	}
}

func TestPushOutboxCycle(t *testing.T) {
	s := openMemory(t)
	ctx := context.Background()

	s.InsertIncomingEvent(ctx, InsertEventInput{
		EventID:     "evt_p1",
		PeerID:      "peer",
		Hash:        "h1",
		State:       "NEW",
		Source:      "MQTT",
		PushEnabled: true,
		PushTarget:  "openclaw",
	})

	rows, err := s.ListPendingPushOutbox(ctx, nowMs(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 pending push, got %d", len(rows))
	}

	// Mark sent.
	if err := s.MarkPushSent(ctx, rows[0].OutboxID, "evt_p1", nowMs()+15000); err != nil {
		t.Fatal(err)
	}

	sentRows, err := s.ListSentPushOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sentRows) != 1 {
		t.Errorf("expected 1 SENT row, got %d", len(sentRows))
	}

	// Mark acked.
	if err := s.MarkPushAcked(ctx, sentRows[0].OutboxID, "evt_p1"); err != nil {
		t.Fatal(err)
	}

	// Should be gone from SENT list.
	sentRows2, _ := s.ListSentPushOutbox(ctx, 10)
	if len(sentRows2) != 0 {
		t.Errorf("expected 0 SENT rows after ack, got %d", len(sentRows2))
	}
}

func TestMediaOutboxCycle(t *testing.T) {
	s := openMemory(t)
	ctx := context.Background()

	// Need a parent message_event for FK reference.
	s.InsertIncomingEvent(ctx, InsertEventInput{
		EventID: "evt_m1",
		PeerID:  "peer",
		Hash:    "hm1",
		State:   "NEW",
		Source:  "MQTT",
	})

	r, err := s.EnqueueMediaOutbox(ctx, MediaEnqueueInput{
		EventID:     "evt_m1",
		SessionKey:  "agent:feishu:channel:abc",
		Channel:     "feishu",
		To:          "oc_123",
		MessageText: "Payment QR ready",
		MediaURL:    "https://example.com/qr.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Inserted {
		t.Error("expected Inserted=true")
	}

	rows, err := s.ListPendingMediaOutbox(ctx, nowMs(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 media row, got %d", len(rows))
	}
	if rows[0].Channel != "feishu" {
		t.Errorf("channel mismatch: %s", rows[0].Channel)
	}
	if rows[0].MediaURL != "https://example.com/qr.png" {
		t.Errorf("media_url mismatch: %s", rows[0].MediaURL)
	}

	if err := s.MarkMediaSent(ctx, rows[0].ID); err != nil {
		t.Fatal(err)
	}

	rows2, _ := s.ListPendingMediaOutbox(ctx, nowMs(), 10)
	if len(rows2) != 0 {
		t.Errorf("expected 0 pending after sent, got %d", len(rows2))
	}
}

func TestBindPeerSession(t *testing.T) {
	s := openMemory(t)
	ctx := context.Background()

	updated, reason := s.BindPeerSession(ctx, "agent_alice", "sess_id_1", "agent:main:main", "manual", nowMs())
	if !updated {
		t.Errorf("expected updated=true, reason=%s", reason)
	}

	// Find by peer (no trace).
	route, err := s.FindA2aReplyRoute(ctx, "agent_alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if route == nil {
		t.Fatal("expected route, got nil")
	}
	if route.SessionKey != "agent:main:main" {
		t.Errorf("session key mismatch: %s", route.SessionKey)
	}
	if route.MatchedBy != "peer-binding" {
		t.Errorf("matchedBy mismatch: %s", route.MatchedBy)
	}
}

func TestIsEventAcked(t *testing.T) {
	s := openMemory(t)
	ctx := context.Background()

	s.InsertIncomingEvent(ctx, InsertEventInput{
		EventID: "evt_ack",
		PeerID:  "peer",
		Hash:    "hack",
		State:   "NEW",
		Source:  "MQTT",
	})

	acked, err := s.IsEventAckedByConsumer(ctx, "consumer1", "evt_ack")
	if err != nil {
		t.Fatal(err)
	}
	if acked {
		t.Error("expected not acked yet")
	}

	s.AckEvent(ctx, "consumer1", "evt_ack", nil)

	acked2, _ := s.IsEventAckedByConsumer(ctx, "consumer1", "evt_ack")
	if !acked2 {
		t.Error("expected acked=true after AckEvent")
	}

	// Different consumer — not acked.
	acked3, _ := s.IsEventAckedByConsumer(ctx, "consumer2", "evt_ack")
	if acked3 {
		t.Error("consumer2 should not be acked")
	}
}
