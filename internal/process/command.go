package process

import (
	"os/exec"
	"runtime"

	"runp/internal/config"
)

const (
	windowsShell        = "cmd.exe"
	windowsShellCommand = "/C"
	unixShell           = "/bin/sh"
	unixShellCommand    = "-c"
)

func buildCommand(cfg config.ResolvedProcess) *exec.Cmd {
	var cmd *exec.Cmd
	if !cfg.Shell {
		cmd = exec.Command(cfg.Command, cfg.Args...)
	} else if runtime.GOOS == "windows" {
		cmd = exec.Command(windowsShell, windowsShellCommand, cfg.Command)
	} else {
		cmd = exec.Command(unixShell, unixShellCommand, cfg.Command)
	}
	cmd.Dir = cfg.Directory
	cmd.Env = append([]string(nil), cfg.Env...)
	return cmd
}
