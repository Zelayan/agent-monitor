//go:build !windows
// +build !windows

package reporter

import (
	"os"
	"path/filepath"
	"syscall"
)

// withSpoolLock 在 Unix/macOS/Linux 下使用 syscall.Flock 实现跨进程文件排他锁
func withSpoolLock(fn func() error) error {
	lockPath := spoolLockFile()
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	return fn()
}
