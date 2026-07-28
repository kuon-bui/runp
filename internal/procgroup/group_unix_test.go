//go:build linux || darwin

package procgroup_test

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"

	"runp/internal/procgroup"
)

func TestForceKillsDescendants(t *testing.T) {
	if os.Getenv("RUNP_PROCESS_TREE_HELPER") == "1" {
		cmd := exec.Command("sh", "-c", "sleep 60 & echo $!; wait")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestForceKillsDescendants$")
	cmd.Env = append(os.Environ(), "RUNP_PROCESS_TREE_HELPER=1")
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

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatalf("descendant PID: %v", scanner.Err())
	}
	descendant, err := strconv.Atoi(scanner.Text())
	if err != nil {
		t.Fatal(err)
	}
	if err := group.Force(); err != nil {
		t.Fatal(err)
	}
	if err := group.Wait(); err == nil {
		t.Fatal("forced process unexpectedly exited successfully")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(descendant, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("descendant %d survived: %v", descendant, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWaitReturnsSameResultToAllCallers(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 7")
	group, err := procgroup.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	first := group.Wait()
	second := group.Wait()
	if fmt.Sprint(first) != fmt.Sprint(second) {
		t.Fatalf("wait errors = %v, %v", first, second)
	}
}
