package process

import (
	"sync"
	"time"

	"runp/internal/config"
)

type RestartTracker struct {
	mu       sync.Mutex
	attempts []time.Time
}

func (t *RestartTracker) Next(cfg config.RestartConfig, policy string, expected bool, exitCode int, now time.Time) (time.Duration, bool) {
	if expected || policy == config.RestartNever || policy == config.RestartOnFailure && exitCode == 0 {
		return 0, false
	}
	if policy != config.RestartAlways && policy != config.RestartOnFailure {
		return 0, false
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	window := time.Duration(cfg.Window)
	cutoff := now.Add(-window)
	first := 0
	for first < len(t.attempts) && t.attempts[first].Before(cutoff) {
		first++
	}
	t.attempts = append(t.attempts[:0], t.attempts[first:]...)
	if len(t.attempts) >= cfg.MaxAttempts {
		return 0, false
	}

	delay := time.Duration(cfg.InitialBackoff)
	maximum := time.Duration(cfg.MaxBackoff)
	for range len(t.attempts) {
		if delay >= maximum-delay {
			delay = maximum
			break
		}
		delay *= 2
	}
	if delay > maximum {
		delay = maximum
	}
	t.attempts = append(t.attempts, now)
	return delay, true
}

func (t *RestartTracker) MarkHealthy(since, now time.Time, window time.Duration) {
	if now.Sub(since) < window {
		return
	}
	t.mu.Lock()
	t.attempts = nil
	t.mu.Unlock()
}
