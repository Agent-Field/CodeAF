//go:build !linux && !darwin

package service

// PlatformRuntime is nil where the platform names no boot or reads no start
// identity a later codeaf could prove (internal/processgroup): a kept-running
// service there could never be told apart from a stranger holding its pid, so
// the capability is absent rather than half-working.
func PlatformRuntime() Runtime { return nil }
