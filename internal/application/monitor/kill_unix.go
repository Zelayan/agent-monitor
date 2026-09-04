//go:build !windows

package monitor

import "syscall"

func terminateProcessGroup(pid, pgid int) error {
	targetGroup := pgid
	if targetGroup <= 1 {
		targetGroup = pid
	}
	if targetGroup > 1 {
		if err := syscall.Kill(-targetGroup, syscall.SIGTERM); err != nil {
			if pid > 1 {
				return syscall.Kill(pid, syscall.SIGTERM)
			}
			return err
		}
	}
	return nil
}
