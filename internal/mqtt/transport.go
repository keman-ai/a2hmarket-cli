package mqtt

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

const (
	defaultQoS     = byte(1)
	keepAlive      = 60 * time.Second
	connectTimeout = 15 * time.Second
)

// Message is a received MQTT message.
type Message struct {
	Topic   string
	Payload string
}

// Transport manages the MQTT connection lifecycle.
type Transport struct {
	brokerURL   string
	tokenClient *TokenClient
	agentID     string
	instanceID  string

	mu                 sync.Mutex
	client             paho.Client
	connectionClientID string
	cleanSession       bool // default false; true for one-shot publishers

	onMessage   func(msg Message)
	onReconnect func()
}

// SetCleanSession overrides the CleanSession flag before Connect is called.
func (t *Transport) SetCleanSession(clean bool) {
	t.cleanSession = clean
}

// NewTransport creates an MQTT transport.
// brokerURL may be bare "host:port", "ssl://host:port", or "mqtts://host:port".
func NewTransport(brokerURL string, tokenClient *TokenClient, agentID, instanceID string) *Transport {
	return &Transport{
		brokerURL:   normalizeBrokerURL(brokerURL),
		tokenClient: tokenClient,
		agentID:     agentID,
		instanceID:  instanceID,
	}
}

// NewTransportWithClientID creates a transport with an explicit clientId.
// Default CleanSession=true (one-shot publish); call SetCleanSession(false) to override.
func NewTransportWithClientID(brokerURL string, tokenClient *TokenClient, agentID, clientID string) *Transport {
	return &Transport{
		brokerURL:          normalizeBrokerURL(brokerURL),
		tokenClient:        tokenClient,
		agentID:            agentID,
		instanceID:         "",
		connectionClientID: clientID,
		cleanSession:       true, // override with SetCleanSession(false) for persistent listeners
	}
}

// OnMessage registers a handler for incoming messages.
func (t *Transport) OnMessage(handler func(msg Message)) {
	t.onMessage = handler
}

// OnReconnect registers a callback invoked after each successful reconnect.
func (t *Transport) OnReconnect(handler func()) {
	t.onReconnect = handler
}

// Connect connects to the MQTT broker.
// Leader/standalone: use the pre-set connectionClientID (base clientId) with CleanSession=false.
// Follower standby / one-shot publisher: cleanSession=true is set via SetCleanSession or
// NewTransportWithClientID.
func (t *Transport) Connect() error {
	connClientID := t.connectionClientID
	if connClientID == "" {
		connClientID = BuildConnectionClientID(t.agentID, t.instanceID)
	}

	cred, err := t.tokenClient.GetToken(connClientID, false)
	if err != nil {
		return fmt.Errorf("mqtt connect: get token: %w", err)
	}

	opts := paho.NewClientOptions()
	opts.AddBroker(t.brokerURL)
	opts.SetClientID(connClientID)
	opts.SetUsername(cred.Username)
	opts.SetPassword(cred.Password)
	opts.SetTLSConfig(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec
	opts.SetCleanSession(t.cleanSession)
	opts.SetKeepAlive(keepAlive)
	opts.SetConnectTimeout(connectTimeout)
	opts.SetAutoReconnect(false)

	opts.SetDefaultPublishHandler(func(_ paho.Client, msg paho.Message) {
		if t.onMessage != nil {
			t.onMessage(Message{Topic: msg.Topic(), Payload: string(msg.Payload())})
		}
	})

	opts.SetConnectionLostHandler(func(_ paho.Client, connErr error) {
		// Background reconnect with exponential backoff
		go t.reconnectLoop(connClientID, connErr)
	})

	client := paho.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(connectTimeout) {
		return fmt.Errorf("mqtt connect: timeout")
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}

	t.mu.Lock()
	t.client = client
	t.connectionClientID = connClientID
	t.mu.Unlock()

	return nil
}

// Subscribe subscribes to the incoming P2P topic for this agent.
func (t *Transport) Subscribe() error {
	t.mu.Lock()
	client := t.client
	t.mu.Unlock()
	if client == nil {
		return fmt.Errorf("mqtt: not connected")
	}

	topic := IncomingTopic(t.agentID)
	tok := client.Subscribe(topic, defaultQoS, func(_ paho.Client, msg paho.Message) {
		if t.onMessage != nil {
			t.onMessage(Message{Topic: msg.Topic(), Payload: string(msg.Payload())})
		}
	})
	if !tok.WaitTimeout(connectTimeout) {
		return fmt.Errorf("mqtt subscribe: timeout")
	}
	return tok.Error()
}

// Publish sends a JSON-encoded payload to the target agent's P2P topic.
func (t *Transport) Publish(targetAgentID string, payload interface{}) error {
	t.mu.Lock()
	client := t.client
	t.mu.Unlock()
	if client == nil {
		return fmt.Errorf("mqtt: not connected")
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mqtt publish: marshal: %w", err)
	}

	topic := OutgoingTopic(targetAgentID)
	tok := client.Publish(topic, defaultQoS, false, data)
	if !tok.WaitTimeout(connectTimeout) {
		return fmt.Errorf("mqtt publish: timeout")
	}
	return tok.Error()
}

// IsConnected reports whether the client is currently connected.
func (t *Transport) IsConnected() bool {
	t.mu.Lock()
	client := t.client
	t.mu.Unlock()
	return client != nil && client.IsConnected()
}

// Close disconnects from the broker.
func (t *Transport) Close() {
	t.mu.Lock()
	client := t.client
	t.client = nil
	t.mu.Unlock()
	if client != nil {
		client.Disconnect(500)
	}
}

func (t *Transport) reconnectLoop(connClientID string, lostErr error) {
	delays := []time.Duration{1, 2, 4, 8, 16, 30}
	attempt := 0
	for {
		d := delays[min(attempt, len(delays)-1)] * time.Second
		time.Sleep(d)
		attempt++

		// Refresh token before reconnect
		cred, err := t.tokenClient.GetToken(connClientID, true)
		if err != nil {
			continue
		}

		opts := paho.NewClientOptions()
		opts.AddBroker(t.brokerURL)
		opts.SetClientID(connClientID)
		opts.SetUsername(cred.Username)
		opts.SetPassword(cred.Password)
		opts.SetTLSConfig(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec
		opts.SetCleanSession(false)
		opts.SetKeepAlive(keepAlive)
		opts.SetConnectTimeout(connectTimeout)
		opts.SetAutoReconnect(false)
		opts.SetDefaultPublishHandler(func(_ paho.Client, msg paho.Message) {
			if t.onMessage != nil {
				t.onMessage(Message{Topic: msg.Topic(), Payload: string(msg.Payload())})
			}
		})
		opts.SetConnectionLostHandler(func(_ paho.Client, err error) {
			go t.reconnectLoop(connClientID, err)
		})

		client := paho.NewClient(opts)
		tok := client.Connect()
		if !tok.WaitTimeout(connectTimeout) {
			continue
		}
		if err := tok.Error(); err != nil {
			continue
		}

		t.mu.Lock()
		t.client = client
		t.mu.Unlock()

		// Resubscribe
		_ = t.Subscribe()

		if t.onReconnect != nil {
			t.onReconnect()
		}
		return
	}
}

func normalizeBrokerURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "ssl://") || strings.HasPrefix(raw, "tcp://") {
		return raw
	}
	if strings.HasPrefix(raw, "mqtts://") {
		return "ssl://" + raw[len("mqtts://"):]
	}
	if strings.HasPrefix(raw, "mqtt://") {
		return "tcp://" + raw[len("mqtt://"):]
	}
	// bare host:port
	return "ssl://" + raw
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
