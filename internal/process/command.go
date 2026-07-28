package process

import (
	"os/exec"
	"runtime"

	"runp/internal/config"
)

func buildCommand(cfg config.ResolvedProcess) *exec.Cmd {
	var cmd *exec.Cmd
	if !cfg.Shell {
		cmd = exec.Command(cfg.Command, cfg.Args...)
	} else if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd.exe", "/C", cfg.Command)
	} else {
		cmd = exec.Command("/bin/sh", "-c", cfg.Command)
	}
	cmd.Dir = cfg.Directory
	cmd.Env = append([]string(nil), cfg.Env...)
	return cmd
}
