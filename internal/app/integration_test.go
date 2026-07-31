package app_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"runp/internal/config"
	"runp/internal/controller"
	"runp/internal/logstore"
	"runp/internal/process"
)

func TestRunpWorkflowHelper(t *testing.T) {
	mode := os.Getenv("RUNP_WORKFLOW_HELPER")
	if mode == "" {
		return
	}
	marker := os.Getenv("RUNP_WORKFLOW_MARKER")
	if marker != "" {
		file, err := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			os.Exit(2)
		}
		_, _ = fmt.Fprintln(file, os.Getenv("RUNP_WORKFLOW_NAME"))
		_ = file.Close()
	}
	_, _ = fmt.Fprintln(os.Stdout, os.Getenv("RUNP_WORKFLOW_NAME")+" stdout")
	_, _ = fmt.Fprintln(os.Stderr, os.Getenv("RUNP_WORKFLOW_NAME")+" stderr")
	if mode == "crash-once" {
		crash := os.Getenv("RUNP_WORKFLOW_CRASH")
		if _, err := os.Stat(crash); os.IsNotExist(err) {
			_ = os.WriteFile(crash, nil, 0o600)
			os.Exit(1)
		}
	}
}

func TestRunpWorkflow(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	cfg := config.Default()
	cfg.Projects = []config.Project{{
		Name:      "shop",
		Directory: dir,
		Autostart: true,
		Processes: []config.Process{
			workflowProcess("api", marker, "wait"),
			workflowProcess("worker", marker, "crash-once", "api"),
		},
	}}
	logs := logstore.New(filepath.Join(dir, "logs"), time.Millisecond)
	manager := process.NewManager(logs)
	control, err := controller.New(cfg, manager)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = control.ForceShutdown(ctx)
		_ = logs.Close()
	})

	if err := control.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForWorkflow(t, control.Events(), func(snapshot controller.Snapshot) bool {
		return workflowState(snapshot, "api") == process.Running && workflowState(snapshot, "worker") == process.Running && workflowRestarts(snapshot, "worker") == 1
	})
	lines := waitForStartLines(t, marker, 3)
	if len(lines) < 3 || lines[0] != "api" || lines[1] != "worker" || lines[2] != "worker" {
		t.Fatalf("start order = %q", lines)
	}
	for _, name := range []string{"api", "worker"} {
		waitForRecords(t, logs, name)
	}

	if err := control.StopProject(context.Background(), "shop"); err != nil {
		t.Fatal(err)
	}
	waitForWorkflow(t, control.Events(), func(snapshot controller.Snapshot) bool {
		return workflowState(snapshot, "api") == process.Stopped && workflowState(snapshot, "worker") == process.Stopped
	})
	if err := control.StartProject(context.Background(), "shop"); err != nil {
		t.Fatal(err)
	}
	api, ok := manager.Snapshot(process.Key{Project: "shop", Process: "api"})
	if !ok || api.State != process.Running || api.PID <= 0 {
		t.Fatalf("restarted api = %#v %v", api, ok)
	}
	pid := api.PID
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := control.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if processExists(pid) {
		t.Fatalf("helper PID %d remains", pid)
	}
}

func workflowProcess(name, marker, mode string, dependencies ...string) config.Process {
	return config.Process{
		Name:      name,
		Command:   os.Args[0],
		Args:      []string{"-test.run=^TestRunpWorkflowHelper$", "--"},
		DependsOn: dependencies,
		Env: map[string]string{
			"RUNP_WORKFLOW_HELPER": mode,
			"RUNP_WORKFLOW_NAME":   name,
			"RUNP_WORKFLOW_MARKER": marker,
			"RUNP_WORKFLOW_CRASH":  filepath.Join(filepath.Dir(marker), name+".crashed"),
		},
		StopTimeout: config.Duration(50 * time.Millisecond),
		Health:      config.HealthConfig{Type: "process", Interval: config.Duration(time.Millisecond), Timeout: config.Duration(time.Second)},
		Restart: config.RestartConfig{
			Policy:         "on-failure",
			MaxAttempts:    1,
			Window:         config.Duration(time.Minute),
			InitialBackoff: config.Duration(time.Millisecond),
			MaxBackoff:     config.Duration(time.Millisecond),
		},
		Log: config.LogConfig{MaxSizeMB: 1, MaxFiles: 2, BufferLines: 100},
	}
}

func waitForWorkflow(t *testing.T, events <-chan controller.Event, ready func(controller.Snapshot) bool) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			if ready(event.Snapshot) {
				return
			}
		case <-timer.C:
			t.Fatal("workflow did not reach running state after restart")
		}
	}
}

func waitForRecords(t *testing.T, logs *logstore.Store, name string) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		records := logs.Snapshot("shop", name)
		var stdout, stderr bool
		for _, record := range records {
			stdout = stdout || record.Stream == logstore.Stdout && strings.Contains(record.Text, name+" stdout")
			stderr = stderr || record.Stream == logstore.Stderr && strings.Contains(record.Text, name+" stderr")
		}
		if stdout && stderr {
			return
		}
		select {
		case <-logs.Events():
		case <-timer.C:
			t.Fatalf("logs for %s = %#v", name, records)
		}
	}
}

func waitForStartLines(t *testing.T, path string, count int) []string {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		lines := strings.Fields(string(data))
		if len(lines) >= count {
			return lines
		}
		select {
		case <-timer.C:
			return lines
		default:
			runtime.Gosched()
		}
	}
}

func workflowState(snapshot controller.Snapshot, name string) process.State {
	for _, project := range snapshot.Projects {
		for _, item := range project.Processes {
			if item.Name == name {
				return item.Runtime.State
			}
		}
	}
	return ""
}

func workflowRestarts(snapshot controller.Snapshot, name string) int {
	for _, project := range snapshot.Projects {
		for _, item := range project.Processes {
			if item.Name == name {
				return item.Runtime.RestartCount
			}
		}
	}
	return 0
}
