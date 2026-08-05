//go:build windows

package wrapper

import (
	"os"
	"os/exec"
	"os/signal"
	"sync"
)

func forwardSignals(process *os.Process) func() {
	signals := make(chan os.Signal, 2)
	done := make(chan struct{})
	signal.Notify(signals, os.Interrupt)
	go func() {
		for {
			select {
			case sig := <-signals:
				_ = process.Signal(sig)
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			signal.Stop(signals)
			close(done)
		})
	}
}

func processExitCode(exitErr *exec.ExitError) int {
	return exitErr.ExitCode()
}
