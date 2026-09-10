package main

import (
	"context"
	"errors"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// v3ModelShelf is the catalog the surface's model lists read, behind ONE
// pointer that a refresh can swap.
//
// It exists because a [catalog.Catalog] is immutable once resolved and shared by
// everything that asks it a question, so "today's list" cannot be written into
// the one the process warmed at launch — it has to be a second catalog, handed
// out in the first one's place. The shelf is that place: every reader that asks
// the never-waiting question ([v3Catalog.ModelsNow]) through it sees the new
// list the moment a refresh lands, and a reader that was handed the launch
// catalog directly keeps the launch catalog, which is still true, only older.
type v3ModelShelf struct {
	current atomic.Pointer[catalog.Catalog]
	// options is how the launch catalog was loaded, so a refresh asks the same
	// router, with the same key, into the same cache file.
	options catalog.Options
}

func newV3ModelShelf(models *catalog.Catalog, options catalog.Options) *v3ModelShelf {
	shelf := &v3ModelShelf{options: options}
	shelf.current.Store(models)
	return shelf
}

// ModelsNow is the list on the shelf, answered without waiting — nil while a
// lazily loaded launch catalog is still warming, and nil for a nil shelf, so
// every reader falls through to the disk cache exactly as it did before.
func (s *v3ModelShelf) ModelsNow() []catalog.Model {
	if s == nil {
		return nil
	}
	return s.current.Load().ModelsNow()
}

// refresh is the surface's [tui3.Options.RefreshModels]: today's list from the
// router, put on the shelf, written to ~/.aforge/v3/models.json so the next
// launch opens on it, and handed back with when it left the router.
//
// A FAILURE PUTS NOTHING ON THE SHELF. [catalog.Refresh] has already degraded to
// the cache it could not replace, and that cache is no newer than what the
// shelf holds, so the list the person was shown stays exactly as it was and
// only the reason travels back. The fetch's ceiling is the catalog's own
// fifteen seconds, applied by its client.
func (s *v3ModelShelf) refresh(ctx context.Context) ([]tui3.Model, time.Time, error) {
	fresh, err := catalog.Refresh(ctx, s.options)
	if err != nil {
		return nil, time.Time{}, v3FetchReason(err)
	}
	s.current.Store(fresh)
	rows := v3Models(fresh)
	_ = tui3.WriteModelCache(rows)
	return rows, fresh.FetchedAt(), nil
}

// v3FetchReason is the error as a person reads it on one line: the transport's
// own reason, without the `Get "https://…/models?output_modalities=all":` the
// HTTP client puts in front of it. The address is the same on every failure and
// says nothing about why this one happened.
func v3FetchReason(err error) error {
	var failed *url.Error
	if errors.As(err, &failed) && failed.Err != nil {
		return failed.Err
	}
	return err
}
