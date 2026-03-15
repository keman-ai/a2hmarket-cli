package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ensureListenerRunning starts `listener run` in background when it is not alive.
// Best-effort only: failures are reported to stderr but never block the caller.
func ensureListenerRunning(configDir string) {
	if isListenerAlive(pidPath(configDir)) {
		return
	}

	lockDir := filepath.Join(configDir, "store", "listener_autostart.lock")
	unlock := tryAcquireAutostartLock(lockDir)
	if unlock == nil {
		return
	}
	defer unlock()

	// Re-check after lock acquisition to avoid duplicate start in concurrent pulls.
	if isListenerAlive(pidPath(configDir)) {
		return
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: cannot locate current executable for listener auto-start: %v\n", err)
		return
	}

	logPath := filepath.Join(configDir, "store", "listener_autostart.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "warn: cannot prepare listener log directory: %v\n", err)
		return
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: cannot open listener auto-start log file: %v\n", err)
		return
	}
	defer logFile.Close()

	cmd := exec.Command(exe, "listener", "run", "--config-dir", configDir)
	cmd.Stdin = nil
	cmd.Stdout = io.MultiWriter(logFile)
	cmd.Stderr = io.MultiWriter(logFile)

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "warn: listener auto-start failed: %v\n", err)
		return
	}
	_ = cmd.Process.Release()

	// Wait briefly for PID file write-up so the current invocation can see a healthy state.
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		if isListenerAlive(pidPath(configDir)) {
			return
		}
	}
}

// tryAcquireAutostartLock uses an atomic mkdir lock to avoid concurrent duplicate starts.
// Returns unlock function on success, or nil when lock is held by another process.
func tryAcquireAutostartLock(lockDir string) func() {
	_ = os.MkdirAll(filepath.Dir(lockDir), 0755)

	if err := os.Mkdir(lockDir, 0755); err == nil {
		return func() { _ = os.Remove(lockDir) }
	}

	// Best-effort stale lock cleanup.
	if fi, err := os.Stat(lockDir); err == nil {
		if time.Since(fi.ModTime()) > 30*time.Second {
			_ = os.Remove(lockDir)
			if err := os.Mkdir(lockDir, 0755); err == nil {
				return func() { _ = os.Remove(lockDir) }
			}
		}
	}
	return nil
}
