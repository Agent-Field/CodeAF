package fsreplace

import (
	"errors"
	"golang.org/x/sys/windows"
	"time"
)

// Retry waits at most 630 ms for transient locks. A missing source, invalid
// name or permanent error is returned immediately; retrying never deletes data.
func Retry(operation func() error) error {
	var err error
	for attempt := 0; ; attempt++ {
		err = operation()
		if err == nil || attempt == 6 || !transient(err) {
			return err
		}
		time.Sleep(time.Duration(10<<attempt) * time.Millisecond)
	}
}

func transient(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
