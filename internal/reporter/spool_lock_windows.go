//go:build windows
// +build windows

package reporter

import (
	"sync"
)

var windowsSpoolMutex sync.Mutex

// withSpoolLock 在 Windows 下使用同步互斥锁保护同一进程内的写并发
func withSpoolLock(fn func() error) error {
	windowsSpoolMutex.Lock()
	defer windowsSpoolMutex.Unlock()
	return fn()
}
