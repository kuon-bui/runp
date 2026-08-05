package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"runp/internal/config"
)

func TestLoadMissingReturnsDefaultWithoutCreatingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || len(got.Projects) != 0 {
		t.Fatalf("config = %#v", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing config created: %v", err)
	}
}

func TestCurrentPathUsesWorkingDirectoryWhenConfigMissing(t *testing.T) {
	directory := t.TempDir()
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	got, err := config.CurrentPath()
	if err != nil {
		t.Fatal(err)
	}
	directory, err = os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(directory, ".runp.json")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if err := config.Save(got, config.Default()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("saved config: %v", err)
	}
}

func TestLoadMalformedDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	before := []byte("{broken")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("expected error")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("file changed: %q", after)
	}
}

func TestLoadRejectsEmptyUnknownAndTrailingJSON(t *testing.T) {
	for name, content := range map[string]string{
		"empty":    "",
		"unknown":  `{"version":1,"defaults":{},"projects":[],"extra":true}`,
		"trailing": `{"version":1,"defaults":{},"projects":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := config.Load(path); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSaveFailurePreservesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	before := []byte("original\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	invalid := config.Default()
	invalid.Version = 2
	if err := config.Save(path, invalid); err == nil {
		t.Fatal("expected validation error")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("file changed: %q", after)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	cfg.Projects = []config.Project{{
		Name:      "app",
		Directory: t.TempDir(),
		Processes: []config.Process{{Name: "api", Command: "api", Env: map[string]string{"API_TOKEN": "secret"}}},
	}}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) || !strings.Contains(string(data), `"version": 1`) {
		t.Fatalf("data = %q", data)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if got.Version != cfg.Version {
		t.Fatalf("loaded = %#v", got)
	}
	if got.Projects[0].Processes[0].Env["API_TOKEN"] != "secret" {
		t.Fatalf("environment = %#v", got.Projects[0].Processes[0].Env)
	}

	if err := config.Save(path, got); err != nil {
		t.Fatalf("save after load: %v", err)
	}
}
