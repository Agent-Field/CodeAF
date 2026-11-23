package filesdoor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relay"
)

// C16, client half: a take makes many requests and dials once. Fifty and more
// requests from several goroutines share one tunnel, one at a time, and the
// relay saw one registration and one dial for all of it.
func TestFilesDoorOneTunnelServesManyRequests(t *testing.T) {
	w := newWorld(t)
	disk := newDisk(t)
	const frames = 20
	type held struct {
		id      string
		rids    []string
		objects []blobstore.Object
	}
	var all []held
	for i := 0; i < frames; i++ {
		frame, objects := w.frameOf(i*10, 1000, 1500, 2000, 500, 700)
		h := held{id: putFrame(t, disk, frame), objects: objects}
		for _, o := range objects {
			h.rids = append(h.rids, o.RID)
		}
		all = append(all, h)
	}
	w.serve(w.config(wrap(t, disk)))
	source := w.source()
	ctx := context.Background()

	const workers, rounds = 8, 7
	var wg sync.WaitGroup
	failures := make(chan string, workers*rounds*4)
	for g := 0; g < workers; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				h := all[(g*rounds+r)%frames]
				objects, err := source.GetMany(ctx, h.rids)
				if err != nil || len(objects) != len(h.objects) {
					failures <- fmt.Sprintf("GetMany: %d objects, %v", len(objects), err)
					continue
				}
				for i, o := range objects {
					if o.RID != h.objects[i].RID || !bytes.Equal(o.Bytes, h.objects[i].Bytes) {
						failures <- "GetMany: an object came back changed"
					}
				}
				if _, err := source.GetFrame(ctx, h.id); err != nil {
					failures <- fmt.Sprintf("GetFrame: %v", err)
				}
				if have, err := source.Has(ctx, h.rids); err != nil || len(have) != len(h.rids) {
					failures <- fmt.Sprintf("Has: %v", err)
				}
				if at, err := source.Locate(ctx, h.rids); err != nil || len(at) != len(h.rids) {
					failures <- fmt.Sprintf("Locate: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	close(failures)
	for f := range failures {
		t.Error(f)
	}
	stats := source.Stats()
	if want := workers * rounds * 4; stats.Requests != want {
		t.Fatalf("%d requests, want %d", stats.Requests, want)
	}
	if stats.Dials != 1 || w.relay.dial.Load() != 1 {
		t.Fatalf("%d dials by the client, %d at the relay; want one", stats.Dials, w.relay.dial.Load())
	}
	if w.relay.engine.Load() != 1 {
		t.Fatalf("%d registrations at the relay; want one", w.relay.engine.Load())
	}
	if stats.MaxInFlight != 1 {
		t.Fatalf("%d requests were in flight at once", stats.MaxInFlight)
	}
}

// A dial is lazy: nothing reaches the relay until the first request.
func TestFilesDoorSourceDialsNothingUntilItIsAsked(t *testing.T) {
	w := newWorld(t)
	source := w.source()
	if stats := source.Stats(); stats.Dials != 0 || w.relay.dial.Load() != 0 {
		t.Fatalf("a new source made %d dials", stats.Dials)
	}
}

// A source only reads.
func TestFilesDoorSourceIsReadOnly(t *testing.T) {
	w := newWorld(t)
	source := w.source()
	frame, _ := w.frameOf(1, 100)
	if _, err := source.PutFrame(context.Background(), frame); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("PutFrame = %v, want ErrReadOnly", err)
	}
	if source.Stats().Dials != 0 {
		t.Fatal("a refused write dialled")
	}
}

// When nobody is registered under the door's name the source retries with a
// growing pause until its window ends, and then says the computer is off,
// naming it, with the last reason underneath.
func TestFilesDoorSourceIsOffWhenNobodyIsRegistered(t *testing.T) {
	w := newWorld(t)
	var slept []time.Duration
	source := NewSource(SourceConfig{
		Service: w.service, Identity: w.id, Device: w.b, TargetID: w.a.ID(), TargetName: "selfhost-pc",
		Sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
	})
	defer source.Close()
	_, err := source.Has(context.Background(), []string{rid(1)})
	if !errors.Is(err, ErrSourceOff) {
		t.Fatalf("Has = %v, want a source that is off", err)
	}
	var off *OffError
	if !errors.As(err, &off) || off.DeviceID != w.a.ID() || off.Name != "selfhost-pc" {
		t.Fatalf("the error is %#v", err)
	}
	if !errors.Is(err, relay.ErrNoMachine) {
		t.Fatalf("the last dial error is not underneath: %v", errors.Unwrap(err))
	}
	if !strings.Contains(err.Error(), "selfhost-pc") {
		t.Fatalf("the sentence does not name the computer: %q", err)
	}
	// A name nobody is registered under is what a computer that is off answers
	// with, so the window is the short one (F3): the pauses grow as always and
	// the last is cut to what is left of it.
	want := []time.Duration{500 * time.Millisecond, time.Second, 500 * time.Millisecond}
	if len(slept) != len(want) {
		t.Fatalf("slept %v, want %v", slept, want)
	}
	for i, d := range want {
		if slept[i] != d {
			t.Fatalf("pause %d was %v, want %v (all: %v)", i, slept[i], d, slept)
		}
	}
	var total time.Duration
	for _, d := range slept {
		total += d
	}
	if total > DefaultNoMachineWithin {
		t.Fatalf("paused %v in all, past the window of %v", total, DefaultNoMachineWithin)
	}
	stats := source.Stats()
	if stats.Dials != len(slept)+1 || stats.DialFailures != stats.Dials || stats.Requests != 0 {
		t.Fatalf("stats = %+v after %d pauses", stats, len(slept))
	}
}

// F3, the other half: the short window is for the relay saying nobody is
// registered, and for nothing else. A relay that is failing, or a first answer
// of that kind followed by a failure, is still given the long window.
func TestFilesDoorSourceKeepsTheLongWindowForAnythingButNoMachine(t *testing.T) {
	w := newWorld(t)
	noMachine := (&relay.Server{})
	for _, tc := range []struct {
		name   string
		handle func(call int) http.Handler
	}{
		{"the relay is failing", func(int) http.Handler {
			return http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
				http.Error(rw, "upstream trouble", http.StatusBadGateway)
			})
		}},
		{"nobody is registered at first and the relay then fails", func(call int) http.Handler {
			if call <= 2 {
				return noMachine
			}
			return http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
				http.Error(rw, "upstream trouble", http.StatusBadGateway)
			})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				tc.handle(int(calls.Add(1))).ServeHTTP(rw, r)
			}))
			defer server.Close()
			var slept []time.Duration
			source := NewSource(SourceConfig{
				Service: server.URL, Identity: w.id, Device: w.b, TargetID: w.a.ID(), TargetName: "selfhost-pc",
				Sleep: func(_ context.Context, d time.Duration) error {
					slept = append(slept, d)
					return nil
				},
			})
			defer source.Close()
			_, err := source.Has(context.Background(), []string{rid(1)})
			if !errors.Is(err, ErrSourceOff) {
				t.Fatalf("Has = %v, want a source that is off", err)
			}
			var total time.Duration
			for _, d := range slept {
				total += d
			}
			if total <= DefaultNoMachineWithin || total > DefaultRetryWithin {
				t.Fatalf("paused %v in all (%v); want the long window, between %v and %v", total, slept, DefaultNoMachineWithin, DefaultRetryWithin)
			}
		})
	}
}

// Cancelling the context ends a request that is waiting on a door, with the
// context's own error, and closing the source ends everything.
func TestFilesDoorSourceStopsWhenItsContextOrItselfEnds(t *testing.T) {
	t.Run("the caller cancels while a door is missing", func(t *testing.T) {
		w := newWorld(t)
		source := w.source()
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(200 * time.Millisecond); cancel() }()
		start := time.Now()
		_, err := source.Has(ctx, []string{rid(1)})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Has = %v, want context.Canceled", err)
		}
		if time.Since(start) > 3*time.Second {
			t.Fatal("the cancellation was not noticed")
		}
	})
	t.Run("the caller's context is already over", func(t *testing.T) {
		w := newWorld(t)
		w.serve(w.config(wrap(t, newDisk(t))))
		source := w.source()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := source.Has(ctx, []string{rid(1)}); !errors.Is(err, context.Canceled) {
			t.Fatalf("Has = %v, want context.Canceled", err)
		}
	})
	t.Run("the source is closed", func(t *testing.T) {
		w := newWorld(t)
		w.serve(w.config(wrap(t, newDisk(t))))
		source := w.source()
		if _, err := source.Has(context.Background(), []string{rid(1)}); err != nil {
			t.Fatal(err)
		}
		_ = source.Close()
		if _, err := source.Has(context.Background(), []string{rid(1)}); !errors.Is(err, ErrClosed) {
			t.Fatalf("Has after Close = %v, want ErrClosed", err)
		}
	})
}

// A door that answers with something that is not the protocol is closed on, the
// error says the store could not be reached, and it is not asked again at once.
// A door that merely ends the connection is dialled again once.
func TestFilesDoorSourceClosesOnAnAnswerThatBreaksTheRules(t *testing.T) {
	cases := []struct {
		name  string
		reply func(tunnel *pair.Tunnel)
		dials int
	}{
		{"an answer of another kind", func(tn *pair.Tunnel) { sendRaw(t, tn, typeGetFrame|answerBit, []byte{0}) }, 1},
		{"an answer of the wrong shape", func(tn *pair.Tunnel) { sendRaw(t, tn, typeHas|answerBit, []byte{0, 5, 1}) }, 1},
		{"an answer that is not a boolean", func(tn *pair.Tunnel) { sendRaw(t, tn, typeHas|answerBit, []byte{0, 1, 7}) }, 1},
		{"an error that is not one byte", func(tn *pair.Tunnel) { sendRaw(t, tn, typeError, []byte{1, 2, 3}) }, 1},
		{"an error code nobody knows", func(tn *pair.Tunnel) { sendRaw(t, tn, typeError, []byte{99}) }, 1},
		{"a length over the limit", func(tn *pair.Tunnel) { _, _ = tn.Write([]byte{0xff, 0xff, 0xff, 0xff, 1}) }, 1},
		{"a connection that just ends", func(tn *pair.Tunnel) { _ = tn.Close() }, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			w.fakeDoor(func(tn *pair.Tunnel) {
				if _, _, _, err := readMessage(tn, MaxRequestBytes); err != nil {
					return
				}
				c.reply(tn)
			})
			source := NewSource(SourceConfig{
				Service: w.service, Identity: w.id, Device: w.b, TargetID: w.a.ID(), TargetName: "selfhost-pc",
				RetryWithin: time.Second, Sleep: func(context.Context, time.Duration) error { return nil },
			})
			defer source.Close()
			_, err := source.Has(context.Background(), []string{rid(1)})
			if !errors.Is(err, blobstore.ErrUnreachable) {
				t.Fatalf("Has = %v, want an unreachable store", err)
			}
			if got := source.Stats().Dials; got != c.dials {
				t.Fatalf("%d dials, want %d", got, c.dials)
			}
			// The broken tunnel is not kept: the next request dials afresh.
			_, _ = source.Has(context.Background(), []string{rid(1)})
			if source.Stats().Dials <= c.dials {
				t.Fatal("the next request reused a tunnel that had broken the rules")
			}
		})
	}
}

// A door that answers an error code gives the taker the store's own sentinel,
// and the tunnel stays usable.
func TestFilesDoorErrorCodesBecomeTheStoresOwnErrors(t *testing.T) {
	cases := map[byte]error{
		codeBadRID:   blobstore.ErrBadRID,
		codeTooMany:  blobstore.ErrTooMany,
		codeDamaged:  blobstore.ErrDamaged,
		codeInternal: blobstore.ErrAnswered,
	}
	for code, want := range cases {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			w := newWorld(t)
			w.fakeDoor(func(tn *pair.Tunnel) {
				for {
					if _, _, _, err := readMessage(tn, MaxRequestBytes); err != nil {
						return
					}
					sendRaw(t, tn, typeError, []byte{code})
				}
			})
			source := w.source()
			for i := 0; i < 2; i++ {
				if _, err := source.Has(context.Background(), []string{rid(1)}); !errors.Is(err, want) {
					t.Fatalf("Has = %v, want %v", err, want)
				}
			}
			if got := source.Stats().Dials; got != 1 {
				t.Fatalf("%d dials; an error answer should leave the tunnel in use", got)
			}
		})
	}
}

// A door that is not the computer asked for is not trusted: whatever answers
// at a name has to prove, with its certificate, that it is that computer, and a
// door of another wire version says so and is not retried.
func TestFilesDoorSourceTrustsOnlyTheComputerItAskedFor(t *testing.T) {
	t.Run("a hello from another computer", func(t *testing.T) {
		w := newWorld(t)
		w.fakeDoorWith(func(tn *pair.Tunnel) hello { return helloFor(w.b, tn) }, func(*pair.Tunnel) {})
		source := w.source()
		_, err := source.Has(context.Background(), []string{rid(1)})
		var no *RefusedError
		if !errors.As(err, &no) || no.Reason != "could not confirm this is selfhost-pc" {
			t.Fatalf("Has = %v", err)
		}
		if got := source.Stats().Dials; got != 1 {
			t.Fatalf("%d dials; it is not worth asking again", got)
		}
	})
	t.Run("a signature that is not for this tunnel", func(t *testing.T) {
		w := newWorld(t)
		w.fakeDoorWith(func(tn *pair.Tunnel) hello {
			h := helloFor(w.a, tn)
			h.Sig = w.a.Sign([]byte("another tunnel"))
			return h
		}, func(*pair.Tunnel) {})
		_, err := w.source().Has(context.Background(), []string{rid(1)})
		var no *RefusedError
		if !errors.As(err, &no) {
			t.Fatalf("Has = %v, want a refusal", err)
		}
	})
	t.Run("another wire version", func(t *testing.T) {
		w := newWorld(t)
		w.fakeDoorWith(func(tn *pair.Tunnel) hello {
			h := helloFor(w.a, tn)
			h.V = Version + 1
			return h
		}, func(*pair.Tunnel) {})
		source := w.source()
		_, err := source.Has(context.Background(), []string{rid(1)})
		var no *RefusedError
		if !errors.As(err, &no) || no.Reason != differentVersion {
			t.Fatalf("Has = %v", err)
		}
		if got := source.Stats().Dials; got != 1 {
			t.Fatalf("%d dials", got)
		}
	})
}

// A caller that is waiting its turn behind a request that never finishes can
// still be cancelled, and closing the source ends the one in front of it.
func TestFilesDoorACallerWaitingItsTurnCanStillBeCancelled(t *testing.T) {
	w := newWorld(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	w.fakeDoor(func(tn *pair.Tunnel) {
		_, _, _, _ = readMessage(tn, MaxRequestBytes)
		<-release // never answers
	})
	source := w.source()
	front := make(chan error, 1)
	go func() {
		_, err := source.Has(context.Background(), []string{rid(1)})
		front <- err
	}()
	time.Sleep(300 * time.Millisecond) // long enough for it to be in flight

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := source.Has(ctx, []string{rid(2)}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the waiting caller got %v, want its own deadline", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("the waiting caller was held far past its deadline")
	}
	_ = source.Close()
	select {
	case err := <-front:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("the request in front got %v, want ErrClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("closing the source did not end the request in front")
	}
}

// A door that takes a request and never answers is given up on after its
// silence bound, dialled once more, and then reported as out of reach, not waited
// on for ever.
func TestFilesDoorSourceGivesUpOnADoorThatGoesSilent(t *testing.T) {
	old := silentAfter
	silentAfter = 300 * time.Millisecond
	defer func() { silentAfter = old }()
	w := newWorld(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	w.fakeDoor(func(tn *pair.Tunnel) {
		_, _, _, _ = readMessage(tn, MaxRequestBytes)
		<-release
	})
	source := NewSource(SourceConfig{
		Service: w.service, Identity: w.id, Device: w.b, TargetID: w.a.ID(), TargetName: "selfhost-pc",
		RetryWithin: time.Second, Sleep: func(context.Context, time.Duration) error { return nil },
	})
	defer source.Close()
	start := time.Now()
	_, err := source.Has(context.Background(), []string{rid(1)})
	if !errors.Is(err, blobstore.ErrUnreachable) {
		t.Fatalf("Has = %v, want an unreachable store", err)
	}
	if got := source.Stats().Dials; got != 2 {
		t.Fatalf("%d dials, want the first and one more", got)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("a silent door held the request far past its bound")
	}
}
