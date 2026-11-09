package main

import (
	"context"
	"net/http"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// samplePairedStorage makes a completed pairing visible to other local windows.
// An absent field or failed request must preserve the previous saved answer.
func samplePairedStorage(ctx context.Context, dir string, route pair.Mailbox) {
	id, err := identity.Load(dir)
	if err != nil {
		return
	}
	dev, err := identity.Device(dir)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, holdsLeaseWithin)
	defer cancel()
	store := blobstore.NewHTTP(route.URL, reqsign.SignFor(homeSigner{id, dev}, time.Now), &http.Client{})
	counts, err := store.Stats(ctx)
	if err == nil && counts.Storage != "" {
		_ = syncsetup.SaveStorage(dir, route.URL, id.ID(), counts.Storage)
	}
}
