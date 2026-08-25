// Package daemon implements the OS-agnostic managed-daemon core: the single-endpoint-owner
// lock, the ServiceManager seam that launchd and systemd adapters plug into, and credential
// rotation. Throughline does not implement its own process supervisor; this package only
// enforces that at most one throughline mcp process owns the endpoint at a time and gives
// upgrades/rotation a safe, restartable sequence to run.
package daemon

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// ErrAlreadyRunning is returned by Acquire when another live process already holds the
// lock. It reports that process's PID so the caller can give a deterministic, actionable
// message instead of an OS-level "address already in use".
type ErrAlreadyRunning struct{ PID int }

func (e ErrAlreadyRunning) Error() string {
	return fmt.Sprintf("throughline mcp is already running (pid %d)", e.PID)
}

// Lock is a held single-endpoint-owner lock. Release must be called exactly once, normally
// via defer, to free the OS-level flock and remove the PID file if this process still owns
// it.
type Lock struct {
	path string
	file *os.File
}

// Acquire takes the exclusive single-endpoint-owner lock at path, creating it if needed.
// It uses flock(2) so a crashed owner's lock is released by the kernel automatically —
// there is no stale-lock cleanup step, and no window where two owners both believe they
// hold it. On success it writes this process's PID into the file for diagnostics (doctor,
// daemon status) and for ErrAlreadyRunning's message.
func Acquire(path string) (*Lock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open daemon lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		holder := readPID(file)
		_ = file.Close()
		return nil, ErrAlreadyRunning{PID: holder}
	}
	if err := file.Truncate(0); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("truncate daemon lock: %w", err)
	}
	if _, err := file.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write daemon lock pid: %w", err)
	}
	return &Lock{path: path, file: file}, nil
}

// Release frees the lock. The file is left in place (flock's exclusivity, not the file's
// existence, is what enforces single ownership) so the next Acquire can reuse it.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	_ = unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	return l.file.Close()
}

// HeldBy returns the PID recorded in the lock file at path without acquiring it, for
// read-only diagnostics (doctor, daemon status). It returns 0 if the file is absent, empty,
// or unreadable.
func HeldBy(path string) int {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()
	return readPID(file)
}

func readPID(file *os.File) int {
	buffer := make([]byte, 32)
	n, err := file.ReadAt(buffer, 0)
	if err != nil && n == 0 {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(buffer[:n])))
	if err != nil {
		return 0
	}
	return pid
}
