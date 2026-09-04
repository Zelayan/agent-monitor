package reporter

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"sync"
)

var (
	hostInfoOnce sync.Once
	cachedHostID string
	cachedBootID string
)

// GetHostAndBootID 返回当前主机的 HostID 与系统 BootID（基于标准库探测，0 外部依赖）。
func GetHostAndBootID() (string, string) {
	hostInfoOnce.Do(func() {
		cachedHostID = detectHostID()
		cachedBootID = detectBootID()
	})
	return cachedHostID, cachedBootID
}

func detectHostID() string {
	// 1. Linux machine-id
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if data, err := os.ReadFile(p); err == nil {
			id := strings.TrimSpace(string(data))
			if id != "" {
				return id
			}
		}
	}

	// 2. hostname fallback
	if h, err := os.Hostname(); err == nil && h != "" {
		hash := sha256.Sum256([]byte(h))
		return hex.EncodeToString(hash[:16])
	}

	return "unknown-host"
}

func detectBootID() string {
	// 1. Linux procfs boot_id
	if data, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id
		}
	}

	// 2. macOS / Darwin: 获取 kern.boottime
	if id := darwinBootID(); id != "" {
		return id
	}

	return "unknown-boot"
}

// GetProcessStartTime 返回当前进程的启动时间（Unix 毫秒）。
func GetProcessStartTime(pid int) int64 {
	// 尝试通过文件状态获取启动时间基准
	if info, err := os.Stat("/proc/self"); err == nil {
		return info.ModTime().UnixMilli()
	}
	return 0
}
