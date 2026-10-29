//go:build !linux && !darwin

package executor

// Where process inspection is unavailable, the record keeps the request's argv and no ports.
func cmdline(int) []string { return nil }

func pidPorts(int) []int { return nil }

func listenPorts([]int) []int { return nil }
