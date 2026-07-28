//go:build windows

package app

import (
	"os"
	"os/signal"
)

func notifySignals(signals chan<- os.Signal) func() {
	signal.Notify(signals, os.Interrupt)
	return func() { signal.Stop(signals) }
}
