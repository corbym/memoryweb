//go:build windows

package stats

import "syscall"

const processQueryLimitedInformation = 0x1000

// defaultProcessAlive reports whether pid is a running process on Windows.
// It opens the process with PROCESS_QUERY_LIMITED_INFORMATION; if the open
// succeeds the process is alive.
func defaultProcessAlive(pid int) bool {
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	syscall.CloseHandle(handle) //nolint:errcheck
	return true
}
