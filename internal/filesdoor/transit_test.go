package filesdoor

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

// frameOfLen is one frame that is exactly total bytes long.
func (w *world) frameOfLen(first, total int) []byte {
	w.t.Helper()
	n := total - 300
	for i := 0; i < 6; i++ {
		frame, _ := w.frameOf(first, n)
		if len(frame) == total {
			return frame
		}
		n += total - len(frame)
	}
	w.t.Fatalf("could not build a frame of %d bytes", total)
	return nil
}

// A frame bigger than one response moves as bounded chunks, each asked for
// after the last was read, and arrives byte-identical. The edges that matter
// are exactly one response, one byte over it, and the biggest frame there is.
func TestFilesDoorMovesAFrameInBoundedChunks(t *testing.T) {
	cases := []struct {
		name     string
		size     int
		requests int
	}{
		{"twelve mebibytes", 12 << 20, 2},
		{"exactly one response", MaxResponseBytes, 1},
		{"one byte over one response", MaxResponseBytes + 1, 2},
		{"the biggest frame", blobstore.MaxFrame, 2},
		{"a small frame", 10 << 10, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			disk := newDisk(t)
			frame := w.frameOfLen(1, c.size)
			id := putFrame(t, disk, frame)
			w.serve(w.config(wrap(t, disk)))
			source := w.source()
			got, err := source.GetFrame(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, frame) {
				t.Fatal("the frame that arrived is not the frame that was sent")
			}
			stats := source.Stats()
			if stats.Requests != c.requests {
				t.Fatalf("%d requests, want %d", stats.Requests, c.requests)
			}
			if stats.MaxResponseBytes > MaxResponseBytes+ResponseSlack {
				t.Fatalf("a response of %d bytes", stats.MaxResponseBytes)
			}
			if stats.MaxInFlight != 1 {
				t.Fatalf("%d requests in flight", stats.MaxInFlight)
			}
		})
	}
}

// An object bigger than one response is named, not sent, in a get-many answer;
// the client then reads it as ranges of its frame and cuts it out, so no
// response is ever bigger than the bound and the object still arrives whole.
func TestFilesDoorReadsAnObjectBiggerThanAResponseByRanges(t *testing.T) {
	w := newWorld(t)
	disk := newDisk(t)
	big := MaxResponseBytes + 1<<20
	frame, objects := w.frameOf(1, 100, big)
	putFrame(t, disk, frame)
	store := wrap(t, disk)
	w.serve(w.config(store))
	source := w.source()
	ctx := context.Background()

	// Asked together, the small one that leads fits and the big one is left for
	// the next ask: a shorter prefix, which a taker already handles.
	got, err := source.GetMany(ctx, []string{objects[0].RID, objects[1].RID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].RID != objects[0].RID || !bytes.Equal(got[0].Bytes, objects[0].Bytes) {
		t.Fatalf("GetMany answered %d objects, want only the small one", len(got))
	}
	// Asked alone, it comes whole, by ranges.
	before := source.Stats().Requests
	got, err = source.GetMany(ctx, []string{objects[1].RID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].RID != objects[1].RID || !bytes.Equal(got[0].Bytes, objects[1].Bytes) {
		t.Fatal("the big object did not arrive intact")
	}
	// The get-many that named it, one locate, and two ranges (8 MiB and 1 MiB).
	if used := source.Stats().Requests - before; used != 4 {
		t.Fatalf("%d requests for the big object, want 4", used)
	}
	one, err := source.Get(ctx, objects[1].RID)
	if err != nil || !bytes.Equal(one, objects[1].Bytes) {
		t.Fatalf("Get of the big object = %v", err)
	}
	if stats := source.Stats(); stats.MaxResponseBytes > MaxResponseBytes+ResponseSlack || stats.MaxInFlight != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

// errClass reduces an error to the sentinel a taker would test for.
func errClass(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, blobstore.ErrNotFound):
		return "not found"
	case errors.Is(err, blobstore.ErrBadRID):
		return "bad rid"
	case errors.Is(err, blobstore.ErrTooMany):
		return "too many"
	}
	return err.Error()
}

// For the same inputs a source answers what the store behind the door answers:
// the same objects, the same prefix, the same absent and malformed cases. It is
// the property that lets a taker use it in place of a local store.
func TestFilesDoorAnswersLikeTheStoreItFronts(t *testing.T) {
	w := newWorld(t)
	disk := newDisk(t)
	// Ten objects of 300 KB: a get-many stops after the fourth, past a mebibyte.
	frameA, _ := w.frameOf(1, 300<<10, 300<<10, 300<<10, 300<<10, 300<<10, 300<<10, 300<<10, 300<<10, 300<<10, 300<<10)
	frameB, _ := w.frameOf(11, 100, 200, 300)
	idA := putFrame(t, disk, frameA)
	putFrame(t, disk, frameB)
	w.serve(w.config(disk))
	source := w.source()
	ctx := context.Background()

	absent := func(n int) string { return rid(1000 + n) }
	manyCases := map[string][]string{
		"a prefix cut at a mebibyte": listIDs(1, 10),
		"small objects together":     listIDs(11, 3),
		"absent in the middle":       {rid(11), absent(1), rid(13)},
		"absent first":               {absent(2), rid(11)},
		"nothing asked":              {},
		"an id twice":                {rid(11), rid(11)},
		"too many":                   listIDs(2000, MaxRidsPerRequest+1),
		"a malformed id":             {rid(11), "not-an-id"},
		"exactly the most":           append(listIDs(1, 13), listIDs(3000, MaxRidsPerRequest-13)...),
	}
	for name, rids := range manyCases {
		t.Run("GetMany/"+name, func(t *testing.T) {
			want, wantErr := disk.GetMany(ctx, rids)
			got, gotErr := source.GetMany(ctx, rids)
			if errClass(gotErr) != errClass(wantErr) {
				t.Fatalf("error = %v, the store says %v", gotErr, wantErr)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("answered %d objects, the store answers %d", len(got), len(want))
			}
		})
	}

	// Has is asked in as many requests as the ids need, up to the store's bound.
	var many []string
	for i := 0; i < 600; i++ {
		many = append(many, rid(i%40))
	}
	hasCases := map[string][]string{
		"none":          {},
		"a few":         {rid(1), absent(3), rid(12)},
		"over one ask":  many,
		"the most":      listIDs(0, blobstore.MaxHas),
		"over the most": listIDs(0, blobstore.MaxHas+1),
		"a bad id":      {rid(1), "xyz"},
	}
	for name, rids := range hasCases {
		t.Run("Has/"+name, func(t *testing.T) {
			want, wantErr := disk.Has(ctx, rids)
			got, gotErr := source.Has(ctx, rids)
			if errClass(gotErr) != errClass(wantErr) || !reflect.DeepEqual(got, want) {
				t.Fatalf("Has = %v, %v; the store says %v, %v", got, gotErr, want, wantErr)
			}
		})
		t.Run("Locate/"+name, func(t *testing.T) {
			want, wantErr := disk.Locate(ctx, rids)
			got, gotErr := source.Locate(ctx, rids)
			if errClass(gotErr) != errClass(wantErr) || !reflect.DeepEqual(got, want) {
				t.Fatalf("Locate = %v, %v; the store says %v, %v", got, gotErr, want, wantErr)
			}
		})
	}

	t.Run("GetFrame", func(t *testing.T) {
		for name, id := range map[string]string{"held": idA, "absent": rid(5000), "malformed": "xyz"} {
			want, wantErr := disk.GetFrame(ctx, id)
			got, gotErr := source.GetFrame(ctx, id)
			if errClass(gotErr) != errClass(wantErr) || !bytes.Equal(got, want) {
				t.Fatalf("%s: GetFrame = %d bytes, %v; the store says %d bytes, %v", name, len(got), gotErr, len(want), wantErr)
			}
		}
	})
	t.Run("Get", func(t *testing.T) {
		for name, id := range map[string]string{"held": rid(12), "absent": absent(9), "malformed": "xyz"} {
			want, wantErr := disk.Get(ctx, id)
			got, gotErr := source.Get(ctx, id)
			if errClass(gotErr) != errClass(wantErr) || !bytes.Equal(got, want) {
				t.Fatalf("%s: Get = %d bytes, %v; the store says %d bytes, %v", name, len(got), gotErr, len(want), wantErr)
			}
		}
	})
}

// C9: bounded transit. A store of at least a hundred megabytes is read through
// the real relay pipe and no more than one bounded response is ever in flight
// on the stream, over one dial.
//
// It takes about a second and a half on a laptop, so it runs every time rather
// than only outside short mode.
func TestFilesDoorMovesAHundredMegabytesInBoundedResponses(t *testing.T) {
	boundedTransit(t, 13, 100_000_000)
}

func boundedTransit(t *testing.T, frames, atLeast int) {
	t.Helper()
	w := newWorld(t)
	disk := newDisk(t)
	store := wrap(t, disk)
	ids := make([]string, frames)
	total := 0
	for i := 0; i < frames; i++ {
		// Four objects of about two mebibytes: a frame the size a bulk save
		// writes, which is one byte over a response and so takes two chunks.
		frame, _ := w.frameOf(i*10, 2<<20, 2<<20, 2<<20, 2<<20-1000)
		ids[i] = putFrame(t, disk, frame)
		total += len(frame)
	}
	if total < atLeast {
		t.Fatalf("the store holds %d bytes, want at least %d", total, atLeast)
	}
	w.serve(w.config(store))
	source := w.source()
	ctx := context.Background()

	started := time.Now()
	for i, id := range ids {
		got, err := source.GetFrame(ctx, id)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		// The id is the hash of the bytes and the client checks it, so a frame
		// that arrived is a frame that is identical.
		if blobstore.IDOf(got) != id {
			t.Fatalf("frame %d is not the frame that was sent", i)
		}
	}
	// And the same chat read object by object, as a take that wants objects does.
	for i := 0; i < min(frames, 2); i++ {
		rids := listIDs(i*10, 4)
		for len(rids) > 0 {
			objects, err := source.GetMany(ctx, rids)
			if err != nil {
				t.Fatal(err)
			}
			rids = rids[len(objects):]
		}
	}
	t.Logf("%d bytes in %v", total, time.Since(started))

	stats := source.Stats()
	if stats.BytesDown < int64(atLeast) {
		t.Fatalf("%d bytes came down, want at least %d", stats.BytesDown, atLeast)
	}
	if stats.MaxInFlight != 1 {
		t.Fatalf("%d requests were in flight at once", stats.MaxInFlight)
	}
	if stats.MaxResponseBytes > MaxResponseBytes+ResponseSlack {
		t.Fatalf("a response of %d bytes, the bound is %d plus a small header", stats.MaxResponseBytes, MaxResponseBytes)
	}
	if stats.MaxRequestBytes > 1<<10 {
		t.Fatalf("a request of %d bytes", stats.MaxRequestBytes)
	}
	if stats.Dials != 1 || w.relay.dial.Load() != 1 {
		t.Fatalf("%d dials by the client, %d reached the relay; want one", stats.Dials, w.relay.dial.Load())
	}
	if store.frameBytes.Load() < int64(atLeast) {
		t.Fatalf("the door read %d bytes of frames", store.frameBytes.Load())
	}
}

// C19: a device removed while it is fetching is cut off within one response. The
// roster is asked before every response, so the request after the removal gets a
// refusal and not one more object byte, and the store is not read for it.
func TestFilesDoorCutsOffADeviceRemovedMidStream(t *testing.T) {
	w := newWorld(t)
	disk := newDisk(t)
	frame := w.frameOfLen(1, 12<<20)
	id := putFrame(t, disk, frame)
	store := wrap(t, disk)
	cfg := w.config(store)
	// A roster answer that lasts a nanosecond is asked again every time, which
	// is the strictest reading of "before every response".
	cfg.RosterFor = time.Nanosecond
	w.serve(cfg)
	// The device is removed the moment the first chunk has been read.
	store.onFrame(func() { w.allow(w.b.ID(), false) })

	source := w.source()
	_, err := source.GetFrame(context.Background(), id)
	var no *RefusedError
	if !errors.As(err, &no) || no.Reason != RemovedLine {
		t.Fatalf("GetFrame = %v, want the refusal %q", err, RemovedLine)
	}
	if n := store.frameReads.Load(); n != 1 {
		t.Fatalf("the store read the frame %d times, want 1: no object byte after the removal", n)
	}
	reads := store.reads.Load()
	if _, err := source.Has(context.Background(), []string{rid(1)}); !errors.As(err, &no) {
		t.Fatalf("a new request from the removed device = %v", err)
	}
	if _, err := source.GetFrame(context.Background(), id); !errors.As(err, &no) {
		t.Fatalf("a new frame request from the removed device = %v", err)
	}
	if store.reads.Load() != reads {
		t.Fatal("the store was read for a device that had been removed")
	}
}

// A roster answer is trusted for its lifetime and not a moment more, and a
// refresh that fails does not make an old answer live longer.
func TestFilesDoorRosterAnswersExpireAndFailedRefreshesDoNotExtendThem(t *testing.T) {
	now := time.Unix(1000, 0)
	calls, failing := 0, false
	roster := &rosterCache{
		now: func() time.Time { return now },
		ttl: 10 * time.Second,
		fetch: func(context.Context) (map[string]bool, error) {
			calls++
			if failing {
				return nil, errors.New("unreachable")
			}
			return map[string]bool{"dev_a": true}, nil
		},
	}
	ctx := context.Background()
	ask := func() (bool, error) { return roster.allows(ctx, "dev_a") }
	if ok, err := ask(); !ok || err != nil || calls != 1 {
		t.Fatalf("first ask = %v, %v after %d fetches", ok, err, calls)
	}
	now = now.Add(9 * time.Second)
	if ok, _ := ask(); !ok || calls != 1 {
		t.Fatalf("an answer inside its lifetime was fetched again (%d)", calls)
	}
	now = now.Add(2 * time.Second)
	failing = true
	if ok, err := ask(); ok || err == nil {
		t.Fatalf("an expired answer was used after a failed refresh: %v, %v", ok, err)
	}
	if ok, err := ask(); ok || err == nil || calls != 3 {
		t.Fatalf("a failed refresh extended the old answer: %v, %v after %d fetches", ok, err, calls)
	}
	failing = false
	if ok, err := ask(); !ok || err != nil {
		t.Fatalf("a refresh that works = %v, %v", ok, err)
	}
	if ok, _ := roster.allows(ctx, "dev_unknown"); ok {
		t.Fatal("a device not in the roster is allowed")
	}
}

// A roster that cannot be read for a moment in the middle of a take costs the
// taker one redial and not the take: the door answers nothing rather than
// guess, and the client's second dial finds the roster readable again.
func TestFilesDoorAMomentaryRosterFailureMidTakeIsRiddenOut(t *testing.T) {
	w := newWorld(t)
	disk := newDisk(t)
	frame, objects := w.frameOf(1, 100, 200)
	putFrame(t, disk, frame)
	store := wrap(t, disk)
	cfg := w.config(store)
	cfg.RosterFor = time.Nanosecond // asked again for every request
	var asked atomic.Int64
	cfg.Roster = func(ctx context.Context) (map[string]bool, error) {
		if asked.Add(1) == 3 { // the hello, one request, then this one
			return nil, errors.New("directory unreachable")
		}
		return w.roster(ctx)
	}
	w.serve(cfg)
	source := NewSource(SourceConfig{
		Service: w.service, Identity: w.id, Device: w.b, TargetID: w.a.ID(), TargetName: "selfhost-pc",
		RetryWithin: time.Second, Sleep: func(context.Context, time.Duration) error { return nil },
	})
	defer source.Close()
	ctx := context.Background()
	for i, o := range objects {
		got, err := source.Get(ctx, o.RID)
		if err != nil || !bytes.Equal(got, o.Bytes) {
			t.Fatalf("object %d: %v", i, err)
		}
	}
	if got := source.Stats().Dials; got != 2 {
		t.Fatalf("%d dials, want 2: the first and the redial after the door went quiet", got)
	}
}
