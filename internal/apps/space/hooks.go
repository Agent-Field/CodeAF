package space

import (
	"context"
	"sync"
)

var (
	memberGoneMu    sync.Mutex
	memberGoneHooks []func(context.Context, string)
)

// OnMemberGone lets other tracks clean up data when a workspace member leaves.
func OnMemberGone(h func(ctx context.Context, member string)) {
	if h == nil {
		return
	}
	memberGoneMu.Lock()
	memberGoneHooks = append(memberGoneHooks, h)
	memberGoneMu.Unlock()
}

func memberGone(ctx context.Context, member string) {
	memberGoneMu.Lock()
	hooks := append([]func(context.Context, string){}, memberGoneHooks...)
	memberGoneMu.Unlock()

	// Callbacks run outside the lock so they can register more cleanup hooks.
	for _, h := range hooks {
		h(ctx, member)
	}
}
