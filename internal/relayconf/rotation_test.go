//go:build relayurl

package relayconf

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// shortest is the longest a grace may be for the deletion case to be worth
// waiting for; a relay whose minimum is longer skips it, as the pairing suite
// skips its expiry case.
const shortest = 5 * time.Second

// TestRotationConformance holds a relay to the rotation contract beyond the
// directory cases every relay already passes: the store wire follows the same
// state machine, a replaced identity is read-only, and at the deadline it is gone.
func TestRotationConformance(t *testing.T) {
	cases := map[string]func(*testing.T, string, *account){
		"StoreRefusesFramesWhenFrozen": storeRefusesFramesWhenFrozen,
		"ReadsStayOpenWhenRetired":     readsStayOpenWhenRetired,
		"RetireDeletes":                retireDeletes,
	}
	base := baseURL(t)
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(t, base, newAccount(t))
		})
	}
}

func gate(base string, a *account) directory.Gate {
	return directory.Gate{Client: directory.NewHTTP(base, a.sign("a", wall), httpClient)}
}

func blobsOf(base string, a *account) *blobstore.HTTP {
	return blobstore.NewHTTP(base, a.sign("a", wall), httpClient)
}

func storeRefusesFramesWhenFrozen(t *testing.T, base string, a *account) {
	ctx := context.Background()
	if err := gate(base, a).Freeze(ctx); err != nil {
		t.Fatal(err)
	}
	frame, _ := oneFrame(t)
	if _, err := blobsOf(base, a).PutFrame(ctx, frame); !errors.Is(err, wireauth.ErrRotated) {
		t.Fatalf("a frame put on a frozen identity: %v, want ErrRotated", err)
	}
}

func readsStayOpenWhenRetired(t *testing.T, base string, a *account) {
	ctx := context.Background()
	frame, rid := oneFrame(t)
	if _, err := blobsOf(base, a).PutFrame(ctx, frame); err != nil {
		t.Fatal(err)
	}
	g := gate(base, a)
	if err := g.Freeze(ctx); err != nil {
		t.Fatal(err)
	}
	if err := g.Retire(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := blobsOf(base, a).Get(ctx, rid); err != nil {
		t.Fatalf("a read of a retired identity inside its grace: %v", err)
	}
	if _, err := directory.NewHTTP(base, a.sign("a", wall), httpClient).List(ctx); err != nil {
		t.Fatalf("a list of a retired identity inside its grace: %v", err)
	}
}

// retireDeletes waits out the shortest grace the relay accepts and then expects
// 410 gone on both wires. It needs a relay started with a short minimum grace
// and a short sweep interval.
func retireDeletes(t *testing.T, base string, a *account) {
	ctx := context.Background()
	d := directory.NewHTTP(base, a.sign("a", wall), httpClient)
	limits, err := d.Rotation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	min := time.Duration(limits.MinMS) * time.Millisecond
	if min > shortest {
		t.Skipf("the relay's shortest grace is %v; start it with a shorter one to run the deletion case", min)
	}
	g := gate(base, a)
	if err := g.Freeze(ctx); err != nil {
		t.Fatal(err)
	}
	if err := g.Retire(ctx, min); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(min + 30*time.Second)
	for {
		_, err := d.List(ctx)
		if errors.Is(err, wireauth.ErrGone) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the identity was still there %v after its deadline: %v", 30*time.Second, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	if _, err := blobsOf(base, a).Has(ctx, nil); !errors.Is(err, wireauth.ErrGone) {
		t.Fatalf("the store wire of a deleted identity: %v, want ErrGone", err)
	}
}
