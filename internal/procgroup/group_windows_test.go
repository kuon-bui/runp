//go:build windows

package procgroup_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"runp/internal/procgroup"
)

func TestForceKillsDescendants(t *testing.T) {
	switch os.Getenv("RUNP_PROCESS_TREE_HELPER") {
	case "parent":
		marker := os.Getenv("RUNP_PROCESS_TREE_MARKER")
		for {
			if _, err := os.Stat(marker); err == nil {
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				os.Exit(2)
			}
			time.Sleep(10 * time.Millisecond)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestForceKillsDescendants$")
		cmd.Env = append(os.Environ(), "RUNP_PROCESS_TREE_HELPER=child")
		if err := cmd.Start(); err != nil {
			os.Exit(2)
		}
		_, _ = fmt.Fprintln(os.Stdout, cmd.Process.Pid)
		if err := cmd.Wait(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	case "child":
		for {
			time.Sleep(time.Hour)
		}
	}

	marker := t.TempDir() + `\assigned`
	cmd := exec.Command(os.Args[0], "-test.run=^TestForceKillsDescendants$")
	cmd.Env = append(os.Environ(),
		"RUNP_PROCESS_TREE_HELPER=parent",
		"RUNP_PROCESS_TREE_MARKER="+marker,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	group, err := procgroup.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = group.Force()
		_ = group.Wait()
		_ = group.Close()
	})
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	var descendant int
	if _, err := fmt.Fscanln(stdout, &descendant); err != nil {
		t.Fatalf("descendant PID: %v", err)
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(descendant))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)

	if err := group.Force(); err != nil {
		t.Fatal(err)
	}
	if err := group.Wait(); err == nil {
		t.Fatal("forced process unexpectedly exited successfully")
	}
	status, err := windows.WaitForSingleObject(handle, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if status != windows.WAIT_OBJECT_0 {
		t.Fatalf("descendant %d survived: wait status %d", descendant, status)
	}
}

func TestWaitReturnsSameResultToAllCallers(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/C", "exit "+strconv.Itoa(7))
	group, err := procgroup.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = group.Close() })

	first := group.Wait()
	second := group.Wait()
	if fmt.Sprint(first) != fmt.Sprint(second) {
		t.Fatalf("wait errors = %v, %v", first, second)
	}
}
