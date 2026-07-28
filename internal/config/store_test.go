package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"runp/internal/config"
)

func TestLoadMissingCreatesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || len(got.Projects) != 0 {
		t.Fatalf("config = %#v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("mode = %o", info.Mode().Perm())
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
}
