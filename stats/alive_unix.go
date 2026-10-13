//go:build !windows

package stats

import "syscall"

// defaultProcessAlive reports whether pid is a running process.
// On Unix, sending signal 0 returns nil if the process exists, EPERM if it
// exists but is owned by a different user, and ESRCH if it does not exist.
func defaultProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
