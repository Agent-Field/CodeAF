package cellstore

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

var appDaemonLifetime atomic.Bool

const appDaemonTouchEvery = 20 * time.Second

// SetAppDaemonLifetime selects a one-minute idle limit for helpers this process
// starts. Call it once, before starting helpers. Existing helpers keep their
// limit. Until ctx ends, health requests keep this process's own helpers warm;
// neither cancellation nor process exit sends a shutdown request.
func SetAppDaemonLifetime(ctx context.Context) {
	if appDaemonLifetime.Swap(true) {
		return
	}
	guard.Go("cellstore/app-daemons", func() {
		ticker := time.NewTicker(appDaemonTouchEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				touchAppDaemons(ctx)
			}
		}
	})
}

func touchAppDaemons(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, healthWait)
	defer cancel()
	daemonOwners.Range(func(socket, owned any) bool {
		select {
		case <-owned.(*spawnedDaemon).exited:
			return true
		default:
		}
		// Use the existing dial only: connect would start a missing helper.
		conn, err := dial(ctx, socket.(string))
		if err != nil {
			return ctx.Err() == nil
		}
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		deadline, _ := ctx.Deadline()
		_ = conn.SetDeadline(deadline)
		_, _ = exchange(conn, wireRequest{V: wireVersion, ID: nextID.Add(1), Verb: "health"})
		stop()
		_ = conn.Close()
		return ctx.Err() == nil
	})
}
