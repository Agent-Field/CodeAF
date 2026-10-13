package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/standing"
)

// App passes reuse the terminal's runner, routing and spending rails, with the
// app's memory-only credentials resolved anew for every pass.
func appStandingTick(ctx context.Context, store *standing.Store) error {
	ticker, release, err := v3StandingTicker(store)
	if err != nil {
		return err
	}
	defer release()
	passCtx, cancel := context.WithTimeout(ctx, standing.TickWindow)
	defer cancel()
	_, err = ticker.Tick(passCtx)
	return err
}
