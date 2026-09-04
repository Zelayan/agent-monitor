//go:build !darwin

package monitor

func darwinBootID() string {
	return ""
}
