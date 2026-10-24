//go:build !linux

package executor

// Off Linux the record keeps the request's argv and no ports.
func cmdline(int) []string { return nil }

func listenPorts([]int) []int { return nil }
