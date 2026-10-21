//go:build relayurl

package relayconf

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/blobstore/blobstoretest"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// skewed is how far the signing clock is moved: past reqsign.Skew (5 minutes).
const skewed = 6 * time.Minute

func wall() time.Time { return time.Now() }

// Every case gets a fresh identity, so it meets an empty namespace, and runs in
// parallel because the lease cases wait out LeaseTTL in real time.
func TestStoreConformance(t *testing.T) {
	base := baseURL(t)
	blobstoretest.Run(t, func(t *testing.T) blobstore.Store {
		t.Parallel()
		return blobstore.NewHTTP(base, newAccount(t).sign("a", wall), httpClient)
	})
}

func TestDirectoryConformance(t *testing.T) {
	base := baseURL(t)
	directorytest.RunRigs(t, func(t *testing.T) directorytest.Rig {
		t.Parallel()
		acct := newAccount(t)
		return directorytest.Rig{
			Clock: relayClock{t, base},
			Devices: func(name string) directory.Client {
				return directory.NewHTTP(base, acct.sign(name, wall), httpClient)
			},
			ID: acct.deviceID,
		}
	})
}

// One identity's objects and cells are invisible to another, and answered as if
// they never existed.
func TestNamespaceIsolation(t *testing.T) {
	base, ctx := baseURL(t), context.Background()
	mine, theirs := newAccount(t), newAccount(t)

	blobs := func(a *account) *blobstore.HTTP { return blobstore.NewHTTP(base, a.sign("a", wall), httpClient) }
	dirs := func(a *account) directory.Client { return directory.NewHTTP(base, a.sign("a", wall), httpClient) }

	frame, rid := oneFrame(t)
	if _, err := blobs(mine).PutFrame(ctx, frame); err != nil {
		t.Fatal(err)
	}
	if _, err := blobs(theirs).Get(ctx, rid); !errors.Is(err, blobstore.ErrNotFound) {
		t.Fatalf("other identity's Get = %v, want ErrNotFound", err)
	}
	const cell = "01J0000000000000000000000B"
	if _, err := dirs(mine).Create(ctx, cell, directory.CellInit{Head: head, Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	if _, err := dirs(theirs).Cell(ctx, cell); !errors.Is(err, directory.ErrNotFound) {
		t.Fatalf("other identity's Cell = %v, want ErrNotFound", err)
	}
	if l, err := dirs(theirs).List(ctx); err != nil || len(l.Cells) != 0 {
		t.Fatalf("other identity's List = %+v, %v; want no cells", l, err)
	}
}

// A request signed six minutes off the relay's clock is refused as skew on both
// wires, and the client names it as the seam's one error.
func TestSkewIsRefused(t *testing.T) {
	base, ctx := baseURL(t), context.Background()
	acct := newAccount(t)
	for _, shift := range []time.Duration{skewed, -skewed} {
		now := func() time.Time { return time.Now().Add(shift) }
		if _, err := blobstore.NewHTTP(base, acct.sign("a", now), httpClient).Has(ctx, nil); !errors.Is(err, wireauth.ErrSkew) {
			t.Errorf("store, shift %v: %v, want ErrSkew", shift, err)
		}
		if _, err := directory.NewHTTP(base, acct.sign("a", now), httpClient).List(ctx); !errors.Is(err, wireauth.ErrSkew) {
			t.Errorf("directory, shift %v: %v, want ErrSkew", shift, err)
		}
	}
}

const head = "1111111111111111111111111111111111111111111111111111111111111111"

// oneFrame is a frame holding one sealed object, and that object's id.
func oneFrame(t *testing.T) (frame []byte, rid string) {
	t.Helper()
	o := blobstore.Object{RID: strings.Repeat("0", 63) + "1", Bytes: []byte("AGEO\x01isolated")}
	frame, err := blobstore.Encode(strings.Repeat("a", 32), []blobstore.Object{o})
	if err != nil {
		t.Fatal(err)
	}
	return frame, o.RID
}
