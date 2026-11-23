package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

const pairedStorageWithin = 1500 * time.Millisecond

// startPairedStorage overlaps the optional reading with the remaining pairing work.
// Joining before success lets another local reader see a timely answer immediately.
func startPairedStorage(ctx context.Context, dir string, route pair.Mailbox) func() {
	ctx, cancel := context.WithTimeout(ctx, pairedStorageWithin)
	done := make(chan struct{})
	// A fault in this optional reading must never take pairing or the surface
	// down, and done still closes so the pairing that waits on it finishes.
	guard.Go("pair/storage", func() {
		defer close(done)
		defer cancel()
		samplePairedStorage(ctx, dir, route)
	})
	return func() { <-done }
}

// samplePairedStorage makes a completed pairing visible to other local windows.
// Temporary startup refusals get two bounded retries. A successful legacy
// answer is saved too; failed readings preserve the previous saved answer.
func samplePairedStorage(ctx context.Context, dir string, route pair.Mailbox) {
	id, err := identity.Load(dir)
	if err != nil {
		return
	}
	dev, err := identity.Device(dir)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, pairedStorageWithin)
	defer cancel()
	store := blobstore.NewHTTP(route.URL, reqsign.SignFor(homeSigner{id, dev}, time.Now), &http.Client{})
	for attempt := 0; attempt < 3; attempt++ {
		counts, err := store.Stats(ctx)
		if err == nil {
			if ctx.Err() == nil {
				_ = syncsetup.SaveStorage(dir, route.URL, id.ID(), counts.Storage)
			}
			return
		}
		if ctx.Err() != nil || errors.Is(err, wireauth.ErrRevoked) || errors.Is(err, wireauth.ErrGone) || attempt == 2 {
			return
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
