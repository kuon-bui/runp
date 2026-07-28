//go:build !windows

package app

import (
	"os"
	"os/signal"
	"syscall"
)

func notifySignals(signals chan<- os.Signal) func() {
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	return func() { signal.Stop(signals) }
}
