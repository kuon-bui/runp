package process

import (
	"reflect"
	"runtime"
	"testing"

	"runp/internal/config"
)

func TestBuildCommandDirect(t *testing.T) {
	cfg := config.ResolvedProcess{Command: "tool", Args: []string{"a b", "c"}, Directory: t.TempDir(), Env: []string{"A=1"}}
	cmd := buildCommand(cfg)
	if cmd.Path != "tool" || !reflect.DeepEqual(cmd.Args, []string{"tool", "a b", "c"}) {
		t.Fatalf("command = %q %q", cmd.Path, cmd.Args)
	}
	if cmd.Dir != cfg.Directory || !reflect.DeepEqual(cmd.Env, cfg.Env) {
		t.Fatalf("dir/env = %q %q", cmd.Dir, cmd.Env)
	}
}

func TestBuildCommandShell(t *testing.T) {
	cfg := config.ResolvedProcess{Command: "echo hello", Args: []string{"ignored"}, Shell: true}
	cmd := buildCommand(cfg)
	want := []string{"/bin/sh", "-c", cfg.Command}
	if runtime.GOOS == "windows" {
		want = []string{"cmd.exe", "/C", cfg.Command}
	}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("command = %q", cmd.Args)
	}
}
