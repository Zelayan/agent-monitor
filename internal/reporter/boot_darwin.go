//go:build darwin

package reporter

import (
	"crypto/sha256"
	"encoding/hex"
	"syscall"
)

func darwinBootID() string {
	if out, err := syscall.Sysctl("kern.boottime"); err == nil && out != "" {
		hash := sha256.Sum256([]byte(out))
		return hex.EncodeToString(hash[:16])
	}
	return ""
}
