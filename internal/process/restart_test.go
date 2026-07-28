package process_test

import (
	"testing"
	"time"

	"runp/internal/config"
	"runp/internal/process"
)

func restartConfig() config.RestartConfig {
	return config.RestartConfig{
		MaxAttempts:    5,
		Window:         config.Duration(time.Minute),
		InitialBackoff: config.Duration(time.Second),
		MaxBackoff:     config.Duration(30 * time.Second),
	}
}

func TestRestartPolicy(t *testing.T) {
	tests := []struct {
		name     string
		policy   string
		expected bool
		exitCode int
		want     bool
	}{
		{name: "expected exit", policy: "always", expected: true, exitCode: 1, want: false},
		{name: "never", policy: "never", exitCode: 1, want: false},
		{name: "on failure zero", policy: "on-failure", exitCode: 0, want: false},
		{name: "on failure nonzero", policy: "on-failure", exitCode: 1, want: true},
		{name: "on failure signal", policy: "on-failure", exitCode: -1, want: true},
		{name: "always zero", policy: "always", exitCode: 0, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var tracker process.RestartTracker
			_, got := tracker.Next(restartConfig(), test.policy, test.expected, test.exitCode, time.Unix(100, 0))
			if got != test.want {
				t.Fatalf("restart = %v", got)
			}
		})
	}
}

func TestRestartBackoffAndCap(t *testing.T) {
	cfg := restartConfig()
	cfg.MaxAttempts = 8
	var tracker process.RestartTracker
	now := time.Unix(100, 0)
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	for index, expected := range want {
		delay, ok := tracker.Next(cfg, "always", false, 0, now.Add(time.Duration(index)*time.Second))
		if !ok || delay != expected {
			t.Fatalf("attempt %d = %s %v", index, delay, ok)
		}
	}
}

func TestRestartBudget(t *testing.T) {
	cfg := restartConfig()
	cfg.MaxAttempts = 2
	var tracker process.RestartTracker
	now := time.Unix(100, 0)
	if delay, ok := tracker.Next(cfg, "on-failure", false, 1, now); !ok || delay != time.Second {
		t.Fatalf("first = %s %v", delay, ok)
	}
	if delay, ok := tracker.Next(cfg, "on-failure", false, 1, now.Add(time.Second)); !ok || delay != 2*time.Second {
		t.Fatalf("second = %s %v", delay, ok)
	}
	if _, ok := tracker.Next(cfg, "on-failure", false, 1, now.Add(2*time.Second)); ok {
		t.Fatal("budget should be exhausted")
	}
}

func TestRestartBudgetUsesRollingWindow(t *testing.T) {
	cfg := restartConfig()
	cfg.MaxAttempts = 2
	var tracker process.RestartTracker
	now := time.Unix(100, 0)
	_, _ = tracker.Next(cfg, "always", false, 0, now)
	_, _ = tracker.Next(cfg, "always", false, 0, now.Add(time.Second))
	delay, ok := tracker.Next(cfg, "always", false, 0, now.Add(time.Minute+time.Nanosecond))
	if !ok || delay != 2*time.Second {
		t.Fatalf("after expiration = %s %v", delay, ok)
	}
}

func TestRestartMarkHealthyResetsOnlyAfterWindow(t *testing.T) {
	cfg := restartConfig()
	cfg.MaxAttempts = 1
	now := time.Unix(100, 0)

	var early process.RestartTracker
	_, _ = early.Next(cfg, "always", false, 0, now)
	early.MarkHealthy(now, now.Add(time.Minute-time.Nanosecond), time.Minute)
	if _, ok := early.Next(cfg, "always", false, 0, now.Add(time.Minute-time.Nanosecond)); ok {
		t.Fatal("early healthy mark reset budget")
	}

	var mature process.RestartTracker
	_, _ = mature.Next(cfg, "always", false, 0, now)
	mature.MarkHealthy(now, now.Add(time.Minute), time.Minute)
	if delay, ok := mature.Next(cfg, "always", false, 0, now.Add(time.Minute)); !ok || delay != time.Second {
		t.Fatalf("reset = %s %v", delay, ok)
	}
}
