//go:build windows

package procgroup

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const forcedExitCode = 1

type Group struct {
	cmd       *exec.Cmd
	pid       int
	job       windows.Handle
	waitOnce  sync.Once
	waitErr   error
	closeOnce sync.Once
	closeErr  error
}

func Start(cmd *exec.Cmd) (*Group, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}

	flags := uint32(windows.CREATE_NEW_PROCESS_GROUP)
	if cmd.SysProcAttr != nil {
		flags |= cmd.SysProcAttr.CreationFlags
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
	if err = cmd.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	pid := cmd.Process.Pid
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		_ = windows.CloseHandle(process)
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = windows.CloseHandle(job)
		return nil, err
	}

	// ponytail: Go os/exec does not expose suspended primary thread; replace with direct CreateProcess(CREATE_SUSPENDED) if escaped descendants are observed.
	return &Group{cmd: cmd, pid: pid, job: job}, nil
}

func (g *Group) PID() int {
	return g.pid
}

func (g *Group) Graceful() error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(g.pid))
}

func (g *Group) Force() error {
	err := windows.TerminateJobObject(g.job, forcedExitCode)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
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
	g.closeOnce.Do(func() {
		_ = g.Force()
		_ = g.Wait()
		g.closeErr = windows.CloseHandle(g.job)
	})
	return g.closeErr
}
