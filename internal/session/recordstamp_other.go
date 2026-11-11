//go:build !linux && !darwin

package session

import "os"

// recordStampOf has no stamp to give where the change time is not known to be
// kept to the nanosecond and out of a writer's reach, so a [RecordCache] there
// keeps nothing and every read is the plain one.
func recordStampOf(os.FileInfo) (recordStamp, bool) { return recordStamp{}, false }

// openRecordFile opens a journal; the handle's own stat is checked for a
// regular file before anything is read from it.
func openRecordFile(path string) (*os.File, error) { return os.Open(path) }
