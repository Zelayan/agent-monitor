//go:build !darwin

package reporter

func darwinBootID() string {
	return ""
}
