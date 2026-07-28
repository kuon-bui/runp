package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"runp/internal/config"
)

func TestDurationJSON(t *testing.T) {
	var got struct {
		Timeout config.Duration `json:"timeout"`
	}
	if err := json.Unmarshal([]byte(`{"timeout":"5s"}`), &got); err != nil {
		t.Fatal(err)
	}
	if time.Duration(got.Timeout) != 5*time.Second {
		t.Fatalf("timeout = %s", time.Duration(got.Timeout))
	}
}

func TestDurationRejectsInvalidValues(t *testing.T) {
	for _, input := range []string{`{"timeout":5}`, `{"timeout":"bad"}`, `{"timeout":"-1s"}`} {
		var got struct {
			Timeout config.Duration `json:"timeout"`
		}
		if err := json.Unmarshal([]byte(input), &got); err == nil {
			t.Fatalf("expected %s to fail", input)
		}
	}
}

func TestDefault(t *testing.T) {
	got := config.Default()
	if got.Version != 1 || time.Duration(got.Defaults.StopTimeout) != 5*time.Second {
		t.Fatalf("unexpected defaults: %#v", got)
	}
	if got.Defaults.Log != (config.LogConfig{MaxSizeMB: 10, MaxFiles: 5, BufferLines: 10000}) {
		t.Fatalf("log defaults = %#v", got.Defaults.Log)
	}
}

func TestValidateRejectsDependencyCycle(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{
		Name: "shop", Directory: root,
		Processes: []config.Process{
			{Name: "api", Command: "api", DependsOn: []string{"web"}},
			{Name: "web", Command: "web", DependsOn: []string{"api"}},
		},
	}}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "shop.api.dependsOn") || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateRejectsInvalidProcesses(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name    string
		process config.Process
		want    string
	}{
		{name: "missing dependency", process: config.Process{Name: "api", Command: "api", DependsOn: []string{"db"}}, want: "missing process"},
		{name: "shell args", process: config.Process{Name: "api", Command: "npm run dev", Shell: true, Args: []string{"extra"}}, want: "args"},
		{name: "invalid HTTP", process: config.Process{Name: "api", Command: "api", Health: config.HealthConfig{Type: "http", URL: "ftp://host"}}, want: "http"},
		{name: "invalid TCP", process: config.Process{Name: "api", Command: "api", Health: config.HealthConfig{Type: "tcp", Address: "missing-port"}}, want: "host:port"},
		{name: "invalid restart", process: config.Process{Name: "api", Command: "api", Restart: config.RestartConfig{Policy: "sometimes"}}, want: "restart.policy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Projects = []config.Project{{Name: "shop", Directory: root, Processes: []config.Process{tt.process}}}
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateRejectsSafeNameCollisions(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{
		{Name: "a/b", Directory: root, Processes: []config.Process{{Name: "api", Command: "api"}}},
		{Name: "a?b", Directory: root, Processes: []config.Process{{Name: "api", Command: "api"}}},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "safe name") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveMergesDefaultsPathsAndEnvironment(t *testing.T) {
	root := t.TempDir()
	backend := filepath.Join(root, "backend")
	if err := os.Mkdir(backend, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("FROM_FILE=file\nOVERRIDE=file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FROM_PARENT", "parent")
	t.Setenv("OVERRIDE", "parent")
	cfg := config.Default()
	cfg.Projects = []config.Project{{
		Name: "shop", Directory: root,
		Processes: []config.Process{{
			Name: "api", Command: "api", Directory: "backend", EnvFile: ".env",
			Env:     map[string]string{"OVERRIDE": "explicit"},
			Health:  config.HealthConfig{Type: "tcp", Address: "127.0.0.1:3000"},
			Restart: config.RestartConfig{Policy: "on-failure"},
			Log:     config.LogConfig{MaxFiles: 2},
		}},
	}}
	got, err := cfg.Resolve("shop", "api")
	if err != nil {
		t.Fatal(err)
	}
	if got.Directory != backend || got.StopTimeout != 5*time.Second {
		t.Fatalf("resolved = %#v", got)
	}
	if got.Health.Type != "tcp" || time.Duration(got.Health.Timeout) != 30*time.Second || time.Duration(got.Health.Interval) != 500*time.Millisecond {
		t.Fatalf("health = %#v", got.Health)
	}
	if got.Restart.MaxAttempts != 5 || got.Restart.Policy != "on-failure" || got.Log.MaxFiles != 2 || got.Log.MaxSizeMB != 10 {
		t.Fatalf("merged defaults = restart %#v log %#v", got.Restart, got.Log)
	}
	env := envMap(got.Env)
	want := map[string]string{"FROM_PARENT": "parent", "FROM_FILE": "file", "OVERRIDE": "explicit"}
	for key, value := range want {
		if env[key] != value {
			t.Fatalf("env[%s] = %q", key, env[key])
		}
	}
	if !sortStrings(got.Env) {
		t.Fatalf("environment is not sorted: %v", got.Env)
	}
}

func TestResolveDefaultsHealthAndRestart(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: root, Processes: []config.Process{{Name: "api", Command: "api"}}}}
	got, err := cfg.Resolve("shop", "api")
	if err != nil {
		t.Fatal(err)
	}
	if got.Health.Type != "process" || got.Restart.Policy != "never" {
		t.Fatalf("resolved = %#v", got)
	}
}

func TestSafeName(t *testing.T) {
	tests := map[string]string{"api": "api", "a/b": "a_b", "..": "_..", "á": "_"}
	for input, want := range tests {
		if got := config.SafeName(input); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", input, got, want)
		}
	}
}

func envMap(items []string) map[string]string {
	result := make(map[string]string, len(items))
	for _, item := range items {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			result[key] = value
		}
	}
	return result
}

func sortStrings(items []string) bool {
	copyOfItems := append([]string(nil), items...)
	slicesSort(copyOfItems)
	return reflect.DeepEqual(items, copyOfItems)
}

func slicesSort(items []string) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j] < items[j-1]; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
