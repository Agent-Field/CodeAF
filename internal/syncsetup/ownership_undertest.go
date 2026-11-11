package syncsetup

import (
	"context"
	"testing"
)

// BeforeOwnershipCheckInTests pauses this drive's watcher before its reading.
// The returned callback runs after the reading and its viewer notification.
// Install only while the drive is idle, and close the drive before cleanup.
func (d *Drive) BeforeOwnershipCheckInTests(before func(context.Context) func(), afterRelease func()) {
	if !testing.Testing() {
		panic("syncsetup: ownership hook requires a test binary")
	}
	d.mu.Lock()
	d.beforeOwnershipCheck = before
	d.afterReleaseInTests = afterRelease
	d.mu.Unlock()
}
