package main

import "errors"

// mkfifo reports that Windows has no named pipes in the file system, so the
// FIFO cases skip there.
func mkfifo(string, uint32) error { return errors.ErrUnsupported }
