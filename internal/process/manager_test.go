package process_test

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"testing"
	"time"

	"runp/internal/config"
	"runp/internal/logstore"
	"runp/internal/process"
)

func TestProcessHelper(t *testing.T) {
	mode := os.Getenv("RUNP_PROCESS_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "wait":
		time.Sleep(24 * time.Hour)
	case "stdout-stderr":
		_, _ = os.Stdout.WriteString("stdout line\n")
		_, _ = os.Stderr.WriteString("stderr line\n")
		time.Sleep(24 * time.Hour)
	case "exit-1":
		os.Exit(1)
	case "exit-0":
		os.Exit(0)
	case "exit-after-window":
		time.Sleep(50 * time.Millisecond)
		os.Exit(1)
	case "ignore-term":
		signal.Ignore()
		time.Sleep(24 * time.Hour)
	}
}

func newTestManager(t *testing.T) (*process.Manager, *logstore.Store) {
	t.Helper()
	logs := logstore.New(t.TempDir(), time.Millisecond)
	manager := process.NewManager(logs)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = manager.ForceShutdown(ctx)
		_ = logs.Close()
	})
	return manager, logs
}

func helperConfig(mode string) config.ResolvedProcess {
	return config.ResolvedProcess{
		ProjectName: "shop",
		Name:        "api",
		Command:     os.Args[0],
		Args:        []string{"-test.run=^TestProcessHelper$", "--"},
		Env:         append(os.Environ(), "RUNP_PROCESS_HELPER="+mode),
		StopTimeout: 50 * time.Millisecond,
		Health: config.HealthConfig{
			Type:     "process",
			Interval: config.Duration(5 * time.Millisecond),
			Timeout:  config.Duration(time.Second),
		},
		Restart: config.RestartConfig{
			Policy:         "never",
			MaxAttempts:    2,
			Window:         config.Duration(time.Minute),
			InitialBackoff: config.Duration(5 * time.Millisecond),
			MaxBackoff:     config.Duration(20 * time.Millisecond),
		},
		Log: config.LogConfig{MaxSizeMB: 1, MaxFiles: 2, BufferLines: 100},
	}
}

func TestManagerStartStop(t *testing.T) {
	manager, _ := newTestManager(t)
	cfg := helperConfig("wait")
	if err := manager.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background(), cfg); err != nil {
		t.Fatalf("duplicate start: %v", err)
	}
	key := process.Key{Project: "shop", Process: "api"}
	running, ok := manager.Snapshot(key)
	if !ok || running.State != process.Running || running.PID <= 0 {
		t.Fatalf("running snapshot = %#v %v", running, ok)
	}
	if err := manager.Stop(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	stopped, _ := manager.Snapshot(key)
	if stopped.State != process.Stopped || stopped.PID != 0 {
		t.Fatalf("stopped snapshot = %#v", stopped)
	}

	want := []process.State{process.Starting, process.Running, process.Stopping, process.Stopped}
	for index, expected := range want {
		select {
		case event := <-manager.Events():
			if event.Snapshot.State != expected {
				t.Fatalf("event %d = %s, want %s", index, event.Snapshot.State, expected)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing event %s", expected)
		}
	}
}

func TestManagerCapturesLogs(t *testing.T) {
	manager, logs := newTestManager(t)
	cfg := helperConfig("stdout-stderr")
	if err := manager.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	var records []logstore.Record
	for {
		records = logs.Snapshot("shop", "api")
		var stdoutFound, stderrFound bool
		for _, record := range records {
			stdoutFound = stdoutFound || record.Stream == logstore.Stdout && strings.Contains(record.Text, "stdout line")
			stderrFound = stderrFound || record.Stream == logstore.Stderr && strings.Contains(record.Text, "stderr line")
		}
		if stdoutFound && stderrFound {
			break
		}
		select {
		case <-logs.Events():
		case <-deadline.C:
			t.Fatalf("records = %#v", records)
		}
	}
	if err := manager.Stop(context.Background(), process.Key{Project: "shop", Process: "api"}); err != nil {
		t.Fatal(err)
	}
}

func TestManagerStartFailure(t *testing.T) {
	manager, _ := newTestManager(t)
	cfg := helperConfig("wait")
	cfg.Command = "/runp-command-does-not-exist"
	err := manager.Start(context.Background(), cfg)
	if err == nil {
		t.Fatal("start unexpectedly succeeded")
	}
	snapshot, ok := manager.Snapshot(process.Key{Project: "shop", Process: "api"})
	if !ok || snapshot.State != process.Failed || snapshot.Error == "" {
		t.Fatalf("snapshot = %#v %v", snapshot, ok)
	}
}

func TestManagerRestartPolicy(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		policy      string
		wantRestart bool
	}{
		{name: "never on failure", mode: "exit-1", policy: "never"},
		{name: "on failure", mode: "exit-1", policy: "on-failure", wantRestart: true},
		{name: "on failure ignores zero", mode: "exit-0", policy: "on-failure"},
		{name: "always restarts zero", mode: "exit-0", policy: "always", wantRestart: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, _ := newTestManager(t)
			cfg := helperConfig(test.mode)
			cfg.Restart.Policy = test.policy
			cfg.Restart.MaxAttempts = 1
			_ = manager.Start(context.Background(), cfg)

			restarted := false
			deadline := time.After(3 * time.Second)
			for {
				select {
				case event := <-manager.Events():
					if event.Snapshot.State == process.Restarting {
						restarted = true
					}
					if event.Snapshot.State == process.Failed {
						if restarted != test.wantRestart {
							t.Fatalf("restarted = %v", restarted)
						}
						if event.Snapshot.RestartCount != boolInt(test.wantRestart) {
							t.Fatalf("restart count = %d", event.Snapshot.RestartCount)
						}
						return
					}
				case <-deadline:
					t.Fatal("restart policy did not settle")
				}
			}
		})
	}
}

func TestManagerRestartBudgetExhaustion(t *testing.T) {
	manager, _ := newTestManager(t)
	cfg := helperConfig("exit-1")
	cfg.Restart.Policy = "on-failure"
	cfg.Restart.MaxAttempts = 2
	_ = manager.Start(context.Background(), cfg)

	deadline := time.After(time.Second)
	for {
		select {
		case event := <-manager.Events():
			if event.Snapshot.State == process.Failed && event.Snapshot.RestartCount == 2 {
				return
			}
		case <-deadline:
			t.Fatal("restart budget did not exhaust")
		}
	}
}

func TestManagerHealthyWindowResetsRestartBudget(t *testing.T) {
	manager, _ := newTestManager(t)
	cfg := helperConfig("exit-after-window")
	cfg.Restart.Policy = "on-failure"
	cfg.Restart.MaxAttempts = 1
	cfg.Restart.Window = config.Duration(20 * time.Millisecond)
	cfg.Restart.InitialBackoff = config.Duration(time.Millisecond)
	cfg.Restart.MaxBackoff = config.Duration(time.Millisecond)
	if err := manager.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case event := <-manager.Events():
			if event.Snapshot.State == process.Restarting && event.Snapshot.RestartCount >= 2 {
				return
			}
		case <-timeout.C:
			snapshot, _ := manager.Snapshot(process.Key{Project: "shop", Process: "api"})
			t.Fatalf("healthy window did not reset restart tracker: %#v", snapshot)
		}
	}
}

func TestManagerStopCancelsRestartBackoff(t *testing.T) {
	manager, _ := newTestManager(t)
	cfg := helperConfig("exit-1")
	cfg.Restart.Policy = "always"
	cfg.Restart.InitialBackoff = config.Duration(time.Second)
	cfg.Restart.MaxBackoff = config.Duration(time.Second)
	_ = manager.Start(context.Background(), cfg)

	waitForState(t, manager.Events(), process.Restarting)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := manager.Stop(ctx, process.Key{Project: "shop", Process: "api"}); err != nil {
		t.Fatal(err)
	}
	waitForState(t, manager.Events(), process.Stopped)
	if snapshot, _ := manager.Snapshot(process.Key{Project: "shop", Process: "api"}); snapshot.State != process.Stopped {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestManagerShutdownSuppressesRestart(t *testing.T) {
	manager, logs := newTestManager(t)
	cfg := helperConfig("exit-1")
	cfg.Restart.Policy = "always"
	cfg.Restart.InitialBackoff = config.Duration(time.Second)
	cfg.Restart.MaxBackoff = config.Duration(time.Second)
	_ = manager.Start(context.Background(), cfg)

	waitForState(t, manager.Events(), process.Restarting)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := manager.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	for event := range manager.Events() {
		if event.Snapshot.State == process.Starting {
			t.Fatalf("restart after shutdown: %#v", event)
		}
	}
	_ = logs.Close()
}

func TestManagerForceShutdownEscalatesGracefulShutdown(t *testing.T) {
	manager, _ := newTestManager(t)
	cfg := helperConfig("ignore-term")
	cfg.StopTimeout = 10 * time.Second
	if err := manager.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	gracefulDone := make(chan error, 1)
	go func() { gracefulDone <- manager.Shutdown(context.Background()) }()
	waitForState(t, manager.Events(), process.Stopping)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.ForceShutdown(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := manager.Snapshot(process.Key{Project: "shop", Process: "api"})
	if snapshot.State != process.Stopped {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	select {
	case err := <-gracefulDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("graceful shutdown remained blocked after force")
	}
}

func TestManagerRetriesGracefulShutdownAfterCanceledAttempt(t *testing.T) {
	manager, _ := newTestManager(t)
	cfg := helperConfig("ignore-term")
	cfg.StopTimeout = 25 * time.Millisecond
	if err := manager.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("first shutdown error = %v", err)
	}
	retryCtx, retryCancel := context.WithTimeout(context.Background(), time.Second)
	defer retryCancel()
	if err := manager.Shutdown(retryCtx); err != nil {
		t.Fatal(err)
	}
	if snapshot, _ := manager.Snapshot(process.Key{Project: "shop", Process: "api"}); snapshot.State != process.Stopped {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestManagerUserStopSuppressesAlwaysRestart(t *testing.T) {
	manager, _ := newTestManager(t)
	cfg := helperConfig("wait")
	cfg.Restart.Policy = "always"
	if err := manager.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop(context.Background(), process.Key{Project: "shop", Process: "api"}); err != nil {
		t.Fatal(err)
	}
	waitForState(t, manager.Events(), process.Stopped)
	if snapshot, _ := manager.Snapshot(process.Key{Project: "shop", Process: "api"}); snapshot.RestartCount != 0 {
		t.Fatalf("restart count = %d", snapshot.RestartCount)
	}
}

func waitForState(t *testing.T, events <-chan process.Event, state process.State) process.Snapshot {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case event := <-events:
			if event.Snapshot.State == state {
				return event.Snapshot
			}
		case <-deadline:
			t.Fatalf("missing state %s", state)
		}
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
