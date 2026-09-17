//go:build windows

package update

// CleanupOldRunning removes the backup left by the previous Windows update.
// Resolution is platform-specific so other launches never pay for this read.
func CleanupOldRunning(executable func() (string, error)) {
	target, err := ExecutableTarget(executable)
	if err == nil {
		CleanupOld(target)
	}
}
