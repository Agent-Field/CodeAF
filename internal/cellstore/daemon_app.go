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
// limit. Until ctx ends, health requests keep this process's own helpers warm
// while it has saving work (appDaemonsWanted). An idle app sends none, so its
// helpers take their own idle exit and stop waking; the next save starts one
// again (Daemon.connect). Neither cancellation nor process exit sends a
// shutdown request.
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
				touchBusyAppDaemons(ctx)
			}
		}
	})
}

// touchBusyAppDaemons touches this process's helpers only while it has saving
// work. An idle app sends nothing.
func touchBusyAppDaemons(ctx context.Context) {
	if appDaemonsWanted() {
		touchAppDaemons(ctx)
	}
}

// appDaemonsWanted reports saving work a warm helper serves: a chat turn is
// running, a capture is held or running, or a seal still owes its durability
// fence. Anything else is an idle app. Its helper exits after the idle limit,
// which costs the next save a cold start and never the save: Daemon.Do starts
// a missing helper, and the WAL keeps every call until its seal holds.
func appDaemonsWanted() bool {
	return liveTurns.Load() > 0 || capturesOwed() || engineDebtOwed()
}

// capturesOwed reports held calls or a capture still running. A finished
// capture that failed stays registered for its error; it is not work.
func capturesOwed() bool {
	captures.Lock()
	defer captures.Unlock()
	if len(captures.held) > 0 {
		return true
	}
	for _, state := range captures.cells {
		select {
		case <-state.done:
		default:
			return true
		}
	}
	return false
}

// engineDebtOwed reports a seal whose engine sync has not yet confirmed it.
func engineDebtOwed() bool {
	engineCommits.Lock()
	defer engineCommits.Unlock()
	for _, debt := range engineCommits.cells {
		if debt.generation != 0 {
			return true
		}
	}
	return false
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
