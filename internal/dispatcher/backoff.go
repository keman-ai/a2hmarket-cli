// Package dispatcher implements outbox-based reliable delivery for A2A, push, and media messages.
// It mirrors JS runtime/js/src/listener/*-dispatcher.js with Go-idiomatic patterns.
package dispatcher

import "time"

const (
	defaultBaseDelayMs int64 = 2_000
	defaultMaxDelayMs  int64 = 120_000
	defaultMultiplier        = 2.0
)

// CalculateBackoffMs returns the next retry delay in milliseconds using exponential backoff.
// attempt is 1-based. maxDelayMs <= 0 defaults to 120 000 ms.
func CalculateBackoffMs(attempt int, maxDelayMs int64) int64 {
	if maxDelayMs <= 0 {
		maxDelayMs = defaultMaxDelayMs
	}
	delay := defaultBaseDelayMs
	for i := 1; i < attempt; i++ {
		delay = int64(float64(delay) * defaultMultiplier)
		if delay >= maxDelayMs {
			delay = maxDelayMs
			break
		}
	}
	return delay
}

// NextRetryAt returns the absolute Unix-ms timestamp for the next retry.
func NextRetryAt(attempt int, maxDelayMs int64) int64 {
	return time.Now().UnixMilli() + CalculateBackoffMs(attempt, maxDelayMs)
}
