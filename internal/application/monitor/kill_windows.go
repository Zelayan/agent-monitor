//go:build windows

package monitor

import "os"

func terminateProcessGroup(pid, pgid int) error {
	if pid > 0 {
		if proc, err := os.FindProcess(pid); err == nil && proc != nil {
			return proc.Kill()
		}
	}
	return nil
}
