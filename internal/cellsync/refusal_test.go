package cellsync

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// refused is a batcher whose next flush is refused with err, and the lines the
// person has been told so far.
func refused(t *testing.T, r *rig, err error) (*Batcher, *[]string) {
	t.Helper()
	b := r.batcher()
	b.Now = r.clock.Now
	var told []string
	b.OnError = func(e error) {
		row, _ := RefusalOf(e)
		told = append(told, row.Say(e))
	}
	r.publishFirst(map[string]string{"a": "0"})
	note(b, r.seal(map[string]string{"a": "1"}))
	r.store.set(err, nil)
	return b, &told
}

func waitOf(t *testing.T, err error) time.Duration {
	t.Helper()
	r := newRig(t)
	b, _ := refused(t, r, err)
	return b.flush(context.Background())
}

func TestAFullRelayIsToldAtOnceAndNotRetried(t *testing.T) {
	r := newRig(t)
	b, told := refused(t, r, blobstore.ErrFull)
	if d := b.flush(context.Background()); d != MaxHalt {
		t.Fatalf("wait after a full relay = %v, want a halt of %v", d, MaxHalt)
	}
	b.flush(context.Background())
	if len(*told) != 1 || (*told)[0] != chatlist.RelayFull(0) {
		t.Fatalf("told %q, want the full sentence once", *told)
	}
}

func TestFreedEndsAHaltAndSyncResumes(t *testing.T) {
	r := newRig(t)
	b, _ := refused(t, r, blobstore.ErrFull)
	sl := newFakeSleeper(r.clock)
	b.Sleep = sl.Sleep
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()
	// A turn is already noted, so the loop's first flush is refused at once and it
	// halts. Waiting for that halt, not for a count of sleepers, is what is
	// deterministic: the heartbeat's own sleep may or may not be registered yet.
	sl.settleOn(MaxHalt)

	h := r.seal(map[string]string{"a": "2"})
	note(b, h)
	r.store.set(nil, nil)
	b.Freed()
	eventually(t, "the newest head to be sent", func() bool { return headIfAny(r) == h })
	if got := r.head(cellID).Head; got != h || b.Pending() != 0 {
		t.Fatalf("after Freed: head %s, %d pending; want the newest head sent", got, b.Pending())
	}
	cancel()
	<-done
}

func TestARateLimitStaysQuietUntilItLastsAFewMinutes(t *testing.T) {
	r := newRig(t)
	b, told := refused(t, r, wireauth.ErrRateLimited)
	b.flush(context.Background())
	r.clock.Advance(2 * time.Minute)
	b.flush(context.Background())
	if len(*told) != 0 {
		t.Fatalf("told %q within two minutes of a rate limit", *told)
	}
	r.clock.Advance(2 * time.Minute)
	b.flush(context.Background())
	b.flush(context.Background())
	if len(*told) != 1 || (*told)[0] != chatlist.SlowDown {
		t.Fatalf("told %q after four minutes, want the slow-down sentence once", *told)
	}
}

func TestARateLimitIsRetriedOnTheWaitTheRelayNamed(t *testing.T) {
	err := wireauth.Wait(wireauth.ErrRateLimited, http.Header{"Retry-After": {"120"}})
	if d := waitOf(t, err); d != 120*time.Second {
		t.Fatalf("wait = %v, want the relay's 120s", d)
	}
	if d := waitOf(t, wireauth.ErrRateLimited); d != 10*time.Second {
		t.Fatalf("wait without a Retry-After = %v, want the doubled interval", d)
	}
}

func TestTooManyIdentitiesIsToldAtOnceAndWaitsTheRelaysWord(t *testing.T) {
	r := newRig(t)
	err := wireauth.Wait(wireauth.ErrTooManyIdentities, http.Header{"Retry-After": {"3600"}})
	b, told := refused(t, r, err)
	if d := b.flush(context.Background()); d != time.Hour {
		t.Fatalf("wait = %v, want the relay's hour", d)
	}
	if len(*told) != 1 || (*told)[0] != chatlist.TooManyNew {
		t.Fatalf("told %q, want the new-identities sentence", *told)
	}
}

func TestARevokedComputerIsToldAtOnceAndNotRetried(t *testing.T) {
	r := newRig(t)
	b, told := refused(t, r, wireauth.ErrRevoked)
	if d := b.flush(context.Background()); d != MaxHalt {
		t.Fatalf("wait after a revoked refusal = %v, want a halt", d)
	}
	if len(*told) != 1 || (*told)[0] != chatlist.Removed {
		t.Fatalf("told %q, want the stopped-computer sentence", *told)
	}
}

func TestAnUnreachableRelayIsNotARefusal(t *testing.T) {
	if _, ok := RefusalOf(blobstore.ErrUnreachable); ok {
		t.Fatal("an unreachable relay was mapped to a person-facing sentence")
	}
}

func TestEveryRefusalHasOneSentenceAndOneSentinel(t *testing.T) {
	lines := map[string]bool{}
	for _, r := range refusals {
		line := r.Say(r.Is)
		if line == "" || lines[line] {
			t.Errorf("refusal %v has an empty or repeated sentence %q", r.Is, line)
		}
		lines[line] = true
	}
}

func TestARecoveredRelayMakesTheNextRefusalNews(t *testing.T) {
	r := newRig(t)
	b, told := refused(t, r, blobstore.ErrFull)
	b.flush(context.Background())
	r.store.set(nil, nil)
	b.flush(context.Background())
	r.store.set(blobstore.ErrFull, nil)
	note(b, r.seal(map[string]string{"a": "2"}))
	b.flush(context.Background())
	if len(*told) != 2 {
		t.Fatalf("told %q, want the full sentence again after a recovery", *told)
	}
}

func TestAFullRelayNamesItsCeilingWhenItToldUsOne(t *testing.T) {
	r := newRig(t)
	b, told := refused(t, r, blobstore.Capped(blobstore.ErrFull, 5<<30))
	b.flush(context.Background())
	if len(*told) != 1 || (*told)[0] != chatlist.RelayFull(5<<30) {
		t.Fatalf("told %q, want the full sentence naming 5 GiB", *told)
	}
}
