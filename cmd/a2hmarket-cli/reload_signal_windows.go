//go:build windows

package main

import (
	"fmt"
	"os"
)

func registerReloadSignal(ch chan<- os.Signal) bool {
	return false
}

func sendReloadSignal(proc *os.Process) error {
	return fmt.Errorf("listener reload is not supported on Windows")
}
