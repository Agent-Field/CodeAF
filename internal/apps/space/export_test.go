package space

import (
	"context"
	"testing"
)

// IsolateMemberGoneHooks drops registrations for this test and puts them back
// after, so one case does not observe another's cleanup hooks.
func IsolateMemberGoneHooks(t *testing.T) {
	t.Helper()
	memberGoneMu.Lock()
	prev := append([]func(context.Context, string){}, memberGoneHooks...)
	memberGoneHooks = nil
	memberGoneMu.Unlock()
	t.Cleanup(func() {
		memberGoneMu.Lock()
		memberGoneHooks = prev
		memberGoneMu.Unlock()
	})
}
