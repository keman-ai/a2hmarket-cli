//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func registerReloadSignal(ch chan<- os.Signal) bool {
	signal.Notify(ch, syscall.SIGUSR1)
	return true
}

func sendReloadSignal(proc *os.Process) error {
	return proc.Signal(syscall.SIGUSR1)
}
