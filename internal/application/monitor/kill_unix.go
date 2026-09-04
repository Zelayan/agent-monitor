//go:build !windows

package monitor

import "syscall"

func terminateProcessGroup(pid, _ int) error {
	if pid <= 1 {
		return nil
	}
	// 向本地内核直接查询目标 PID 的真实进程组 ID (PGID)，拒绝盲信外部输入
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid > 1 {
		if err := syscall.Kill(-pgid, syscall.SIGTERM); err == nil {
			return nil
		}
	}
	// 降级单进程终止
	return syscall.Kill(pid, syscall.SIGTERM)
}
