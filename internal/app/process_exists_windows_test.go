//go:build windows

package app_test

import "golang.org/x/sys/windows"

func processExists(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}

	defer windows.CloseHandle(handle)
	var exitCode uint32
	return windows.GetExitCodeProcess(handle, &exitCode) == nil && exitCode == 259
}
