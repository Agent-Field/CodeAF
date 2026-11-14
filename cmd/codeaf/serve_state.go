package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// startServeKeeping starts serve's home-state and presence workers only when
// syncsetup.ServeStateVar asks for them (a dev box sets it), and returns their
// join.
func startServeKeeping(ctx context.Context, service string) func() {
	return serveKeeping(ctx, service, syncsetup.ServeKeepsState())
}

// serveKeeping is startServeKeeping with the decision made. With keep false
// only permission discovery runs; home-state and presence remain opt-in.
func serveKeeping(ctx context.Context, service string, keep bool) func() {
	stopStorage := syncsetup.StartStorageRefresh(ctx, home.Dir())
	if !keep {
		return stopStorage
	}
	joinState := startServeHomeState(ctx, home.Dir(), func(err error) {
		fmt.Fprintln(os.Stderr, "could not sync this machine's saved keys and settings:", err)
	})
	presenceCtx, stopPresence := context.WithCancel(ctx)
	presenceDone := make(chan struct{})
	guard.Go("serve/presence", func() { defer close(presenceDone); keepServePresence(presenceCtx, service) })
	return func() {
		stopPresence()
		<-presenceDone
		joinState()
		stopStorage()
	}
}

// startServeHomeState gives the serving process one home-state worker and a
// join. Its first round runs asynchronously so a slow relay cannot delay serving.
func startServeHomeState(ctx context.Context, home string, report func(error)) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	guard.Go("serve/home-state", func() {
		defer close(done)
		syncsetup.RunHomeState(ctx, home, report)
	})
	return func() { cancel(); <-done }
}
