//go:build !windows

package opener

func platformCommand() (string, []string) { return "", nil }
