package update

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

type windowsFileReplacer func(target, replacement, backup string) error

// Windows reports error 1177 after moving the original to the backup but
// before moving the checked replacement onto the target.
const windowsUnableToMoveReplacement2 syscall.Errno = 1177

// replaceWindowsExecutable gives ReplaceFileW the target, checked replacement,
// and backup path. On its documented partial failure, recovery first restores
// the backup and then tries to finish the replacement if that new move fails.
func replaceWindowsExecutable(temporary, target string, replace windowsFileReplacer) error {
	backup := target + ".old"
	if err := os.Remove(backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	err := replace(target, temporary, backup)
	if !errors.Is(err, windowsUnableToMoveReplacement2) {
		return err
	}
	if restoreErr := os.Rename(backup, target); restoreErr == nil {
		return err
	} else if installErr := os.Rename(temporary, target); installErr == nil {
		return nil
	} else {
		return fmt.Errorf("replace target: %w; restore original: %v; move replacement: %v", err, restoreErr, installErr)
	}
}
