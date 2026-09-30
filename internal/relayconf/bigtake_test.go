//go:build relayurl

package relayconf

import (
	"context"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

// TestBigTakeBack puts about 100 MiB of sealed objects, one per frame as a
// random file's chunks are packed, and reads every one back through the
// batched get with the client's own byte-sized deadlines and a plain HTTP client, as a device taking
// the change back would.
func TestBigTakeBack(t *testing.T) {
	base, ctx := baseURL(t), context.Background()
	store := blobstore.NewHTTP(base, newAccount(t).sign("a", wall), nil)
	const n = 105
	rids := make([]string, n)
	start := time.Now()
	for i := range rids {
		body := make([]byte, 1<<20-2048)
		_, _ = rand.Read(body)
		rids[i] = fmt.Sprintf("%064x", i+1)
		frame, err := blobstore.Encode("00000000000000000000000000000000", []blobstore.Object{{RID: rids[i], Bytes: append([]byte("AGEO\x01"), body...)}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.PutFrame(ctx, frame); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	t.Logf("put %d frames in %v", n, time.Since(start))
	start = time.Now()
	gets, worst := 0, time.Duration(0)
	for left := rids; len(left) > 0; gets++ {
		t0 := time.Now()
		got, err := store.GetMany(ctx, left[:min(len(left), blobstore.MaxGetMany)])
		if err != nil || len(got) == 0 {
			t.Fatalf("get many after %d requests: %v", gets, err)
		}
		worst = max(worst, time.Since(t0))
		left = left[len(got):]
	}
	t.Logf("read %d objects in %d requests, %v, slowest %v", n, gets, time.Since(start), worst)
	start = time.Now()
	for _, rid := range rids {
		if _, err := store.Get(ctx, rid); err != nil {
			t.Fatalf("get %s: %v", rid, err)
		}
	}
	t.Logf("read them one at a time in %v", time.Since(start))
}
