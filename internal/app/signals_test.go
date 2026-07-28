package app

import (
	"context"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"runp/internal/tui"
)

type fakeSignalProgram struct {
	sent   chan tea.Msg
	killed chan struct{}
}

func (p *fakeSignalProgram) Send(message tea.Msg) { p.sent <- message }
func (p *fakeSignalProgram) Kill()                { close(p.killed) }

func TestSignalLifecycleFirstSignalRequestsShutdown(t *testing.T) {
	signals := make(chan os.Signal, 2)
	program := &fakeSignalProgram{sent: make(chan tea.Msg, 1), killed: make(chan struct{})}
	var graceful, forced atomic.Int32
	stop := handleSignalChannel(signals, program, func(context.Context) error {
		graceful.Add(1)
		return nil
	}, func(context.Context) error {
		forced.Add(1)
		return nil
	})
	defer stop()

	signals <- os.Interrupt
	select {
	case message := <-program.sent:
		if _, ok := message.(tui.ShutdownRequestMsg); !ok {
			t.Fatalf("message = %T", message)
		}
	case <-time.After(time.Second):
		t.Fatal("first signal did not request shutdown")
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for graceful.Load() == 0 {
		select {
		case <-deadline.C:
			t.Fatal("first signal did not start graceful cleanup")
		default:
			runtime.Gosched()
		}
	}
	if forced.Load() != 0 {
		t.Fatal("first signal forced cleanup")
	}
}

func TestSignalLifecycleSecondSignalForcesAndKills(t *testing.T) {
	signals := make(chan os.Signal, 2)
	program := &fakeSignalProgram{sent: make(chan tea.Msg, 1), killed: make(chan struct{})}
	forceDone := make(chan struct{})
	stop := handleSignalChannel(signals, program, func(context.Context) error { return nil }, func(context.Context) error {
		close(forceDone)
		return nil
	})
	defer stop()

	signals <- os.Interrupt
	<-program.sent
	signals <- os.Interrupt
	select {
	case <-forceDone:
	case <-time.After(time.Second):
		t.Fatal("second signal did not force cleanup")
	}
	select {
	case <-program.killed:
	case <-time.After(time.Second):
		t.Fatal("second signal did not kill program")
	}
}
