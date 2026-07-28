//go:build linux || darwin

package procgroup

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
)

type Group struct {
	cmd      *exec.Cmd
	pid      int
	waitOnce sync.Once
	waitErr  error
}

func Start(cmd *exec.Cmd) (*Group, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Group{cmd: cmd, pid: cmd.Process.Pid}, nil
}

func (g *Group) PID() int {
	return g.pid
}

func (g *Group) Graceful() error {
	return signalGroup(g.pid, syscall.SIGTERM)
}

func (g *Group) Force() error {
	return signalGroup(g.pid, syscall.SIGKILL)
}

func signalGroup(pid int, signal syscall.Signal) error {
	err := syscall.Kill(-pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (g *Group) Wait() error {
	g.waitOnce.Do(func() {
		g.waitErr = g.cmd.Wait()
	})
	return g.waitErr
}

func (g *Group) Close() error {
	if err := g.Force(); err != nil {
		return err
	}
	return g.Wait()
}
