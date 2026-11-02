package worker_test

import "errors"

// mkfifo reports that Windows has no named pipes in the file system, so the
// capacity rig skips there.
func mkfifo(string, uint32) error { return errors.ErrUnsupported }
