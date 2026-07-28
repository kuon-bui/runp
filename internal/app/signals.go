package app

import (
	"context"
	"os"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"runp/internal/controller"
	"runp/internal/tui"
)

type signalProgram interface {
	Send(tea.Msg)
	Kill()
}

func handleSignals(program *tea.Program, control *controller.Controller) func() {
	signals := make(chan os.Signal, 2)
	stopNotify := notifySignals(signals)
	stopLifecycle := handleSignalChannel(signals, program, control.Shutdown, control.ForceShutdown)
	return func() {
		stopNotify()
		stopLifecycle()
	}
}

func handleSignalChannel(signals <-chan os.Signal, program signalProgram, graceful, force func(context.Context) error) func() {
	stop := make(chan struct{})
	var stopOnce sync.Once
	go func() {
		select {
		case <-signals:
			go program.Send(tui.ShutdownRequestMsg{})
			go func() { _ = graceful(context.Background()) }()
		case <-stop:
			return
		}
		select {
		case <-signals:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = force(ctx)
			cancel()
			program.Kill()
		case <-stop:
		}
	}()
	return func() { stopOnce.Do(func() { close(stop) }) }
}
