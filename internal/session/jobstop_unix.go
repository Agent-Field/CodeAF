//go:build !windows

package session

func requireJobStopProof() bool { return false }
