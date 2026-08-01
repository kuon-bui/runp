package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"runp/internal/config"
	"runp/internal/controller"
	"runp/internal/logstore"
	"runp/internal/process"
)

func stubProgram(t *testing.T) {
	t.Helper()
	previous := runProgram
	runProgram = func(*tea.Program) error { return nil }
	t.Cleanup(func() { runProgram = previous })
}

func TestRunCreatesMissingConfiguredFile(t *testing.T) {
	stubProgram(t)
	path := filepath.Join(t.TempDir(), "nested", "runp.json")
	if err := Run(context.Background(), []string{"--config", path}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version": 1`) {
		t.Fatalf("config = %s", data)
	}
}

func TestRunRejectsMalformedConfig(t *testing.T) {
	stubProgram(t)
	path := filepath.Join(t.TempDir(), "runp.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), []string{"--config", path}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "load config") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunRejectsUnknownFlagAndPositionals(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"extra"}} {
		err := Run(context.Background(), args, strings.NewReader(""), io.Discard, io.Discard)
		if err == nil {
			t.Fatalf("args %q accepted", args)
		}
	}
}

func TestRunUsesDefaultConfigPath(t *testing.T) {
	stubProgram(t)
	configRoot, _ := setUserDirs(t)
	if err := Run(context.Background(), nil, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configRoot, "runp", "config.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("default config: %v", err)
	}
}

func TestRunUsesConfigFromWorkingDirectory(t *testing.T) {
	stubProgram(t)
	configRoot, _ := setUserDirs(t)
	directory := t.TempDir()
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	path := filepath.Join(directory, ".runp.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = Run(context.Background(), nil, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "load config") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(configRoot, "runp", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("user config created despite local config: %v", err)
	}
}

func TestRunStoresLogsBelowCacheLogsDirectory(t *testing.T) {
	root := t.TempDir()
	_, cacheRoot := setUserDirsAt(t, root)
	projectDir := filepath.Join(root, "project")
	if err := os.Mkdir(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Projects = []config.Project{{
		Name:      "shop",
		Directory: projectDir,
		Autostart: true,
		Processes: []config.Process{{
			Name:    "api",
			Command: os.Args[0],
			Args:    []string{"-test.run=^TestRunLogHelper$", "--"},
			Env:     map[string]string{"RUNP_LOG_HELPER": "1"},
			Health:  config.HealthConfig{Type: "process", Interval: config.Duration(time.Millisecond), Timeout: config.Duration(time.Second)},
			Restart: config.RestartConfig{Policy: "never", MaxAttempts: 1, Window: config.Duration(time.Minute), InitialBackoff: config.Duration(time.Millisecond), MaxBackoff: config.Duration(time.Millisecond)},
			Log:     config.LogConfig{MaxSizeMB: 1, MaxFiles: 2, BufferLines: 10},
		}},
	}}
	path := filepath.Join(root, "runp.json")
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	previous := runProgram
	runProgram = func(*tea.Program) error {
		deadline := time.Now().Add(time.Second)
		want := filepath.Join(cacheRoot, "runp", "logs", "shop", "api.log")
		for time.Now().Before(deadline) {
			if _, err := os.Stat(want); err == nil {
				return nil
			}
			runtime.Gosched()
		}
		return fmt.Errorf("log file not created at %s", want)
	}
	t.Cleanup(func() { runProgram = previous })
	if err := Run(context.Background(), []string{"--config", path}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestRunLogHelper(t *testing.T) {
	if os.Getenv("RUNP_LOG_HELPER") == "" {
		return
	}
	_, _ = fmt.Fprintln(os.Stdout, "log")
	select {}
}

func setUserDirs(t *testing.T) (string, string) {
	t.Helper()
	return setUserDirsAt(t, t.TempDir())
}

func setUserDirsAt(t *testing.T, root string) (string, string) {
	t.Helper()
	switch runtime.GOOS {
	case "darwin":
		t.Setenv("HOME", root)
		return filepath.Join(root, "Library", "Application Support"), filepath.Join(root, "Library", "Caches")
	case "windows":
		configRoot := filepath.Join(root, "config")
		cacheRoot := filepath.Join(root, "cache")
		t.Setenv("AppData", configRoot)
		t.Setenv("LocalAppData", cacheRoot)
		return configRoot, cacheRoot
	default:
		configRoot := filepath.Join(root, "config")
		cacheRoot := filepath.Join(root, "cache")
		t.Setenv("XDG_CONFIG_HOME", configRoot)
		t.Setenv("XDG_CACHE_HOME", cacheRoot)
		return configRoot, cacheRoot
	}
}

func TestRunConvertsPanicToError(t *testing.T) {
	previous := runProgram
	runProgram = func(*tea.Program) error { panic("boom") }
	t.Cleanup(func() { runProgram = previous })
	path := filepath.Join(t.TempDir(), "runp.json")
	err := Run(context.Background(), []string{"--config", path}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "panic: boom") || !strings.Contains(err.Error(), "goroutine") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunReturnsProgramError(t *testing.T) {
	want := errors.New("program failed")
	previous := runProgram
	runProgram = func(*tea.Program) error { return want }
	t.Cleanup(func() { runProgram = previous })
	path := filepath.Join(t.TempDir(), "runp.json")
	if err := Run(context.Background(), []string{"--config", path}, strings.NewReader(""), io.Discard, io.Discard); !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}

func TestRunValidatesReplacementBeforeSaving(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	if err := os.Mkdir(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: projectDir, Processes: []config.Process{{
		Name:    "api",
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestRunLogHelper$", "--"},
		Env:     map[string]string{"RUNP_LOG_HELPER": "1"},
		Health:  config.HealthConfig{Type: "process", Interval: config.Duration(time.Millisecond), Timeout: config.Duration(time.Second)},
		Restart: config.RestartConfig{Policy: "never", MaxAttempts: 1, Window: config.Duration(time.Minute), InitialBackoff: config.Duration(time.Millisecond), MaxBackoff: config.Duration(time.Millisecond)},
		Log:     config.LogConfig{MaxSizeMB: 1, MaxFiles: 2, BufferLines: 10},
	}}}}
	path := filepath.Join(root, "runp.json")
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	logs := logstore.New(filepath.Join(root, "logs"), time.Millisecond)
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
	if err := control.StartProcess(context.Background(), "shop", "api"); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var next config.Config
	if err := json.Unmarshal(data, &next); err != nil {
		t.Fatal(err)
	}
	next.Projects[0].Processes[0].Command = "changed"
	if err := saveReplacement(path, control, &cfg, next); err == nil {
		t.Fatal("invalid replacement accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("config changed after rejected replacement:\n%s", after)
	}
}
