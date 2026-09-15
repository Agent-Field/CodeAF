package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

// rateLimitedUntil answers 429 for the first `limit` requests and then answers
// a real completion. It is the fake provider these tests pace against.
func rateLimitedUntil(limit int, served *int, mu *sync.Mutex) http.HandlerFunc {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		*served++
		paced := *served <= limit
		mu.Unlock()
		if paced {
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"error":{"message":"slow down"}}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`))
	})
}

func pacedClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	client, err := NewClient(Config{APIKey: "k", BaseURL: "http://provider.test", Model: "sim/model",
		HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// A CONVERSATION GIVES UP, AND WHAT ENDS IT IS THE DEADLINE.
//
// It used to be a count — six attempts, `rateLimitAttempts` — and the count is
// deleted with the other five budgets this loop owned
// (docs/design/recovery/DESIGN.md §4). What bounds a watched call now is
// `lane.Role.GiveUp` for the role it was made in: ninety seconds for a
// conversation's turn, and the waits it asked for count against that whether or
// not the seam below really slept them.
func TestAWatchedCallGivesUpOnPacingWhenItsDeadlineIsSpent(t *testing.T) {
	var mu sync.Mutex
	served := 0
	client := pacedClient(t, rateLimitedUntil(1000, &served, &mu))
	client.wait = func(context.Context, time.Duration) error { return nil }

	began := time.Now()
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("a")); err == nil {
		t.Fatal("every attempt was rate limited; the call should have failed")
	}
	// IT ENDS AT ONCE IN REAL TIME. The waits were stubbed out, so the ninety
	// seconds it believes it spent cost the test nothing — which is the whole
	// reason the dispatcher charges itself for a wait it asked for.
	if took := time.Since(began); took > 20*time.Second {
		t.Fatalf("a stubbed-out wait still took %s of wall clock", took)
	}
	mu.Lock()
	attempts := served
	mu.Unlock()
	if attempts < 2 {
		t.Fatalf("a watched call made %d attempts; a 429 is answered by moving, not by giving up", attempts)
	}
	// AND IT IS THE DEADLINE THAT STOPPED IT, not a count: the backoffs it paid
	// (700ms doubling to the one-minute cap) reach `lane.RoleUnknown`'s give-up
	// in far fewer sends than the sixty a patient call used to be allowed.
	if attempts > 16 {
		t.Fatalf("a watched call made %d attempts inside one deadline; the doubling should have spent it sooner", attempts)
	}
}

// A TASK CHILD KEEPS GOING, and it keeps going for LONGER rather than for more
// attempts: `lane.RoleLeafUnattended`'s patience of three scales the same
// give-up the watched call above is bounded by, so a node waits four and a half
// minutes where a turn waits ninety seconds.
func TestATaskChildWaitsPacingOutPastTheBoundedPatience(t *testing.T) {
	var mu sync.Mutex
	served := 0
	const paced = 8
	client := pacedClient(t, rateLimitedUntil(paced, &served, &mu))
	var waits []time.Duration
	client.wait = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}

	ctx := WithPatientRateLimits(context.Background())
	if _, err := client.CompleteWithMessages(ctx, userMessages("a")); err != nil {
		t.Fatalf("a patient call should have landed once the pacing lifted: %v", err)
	}
	mu.Lock()
	attempts := served
	mu.Unlock()
	if attempts != paced+1 {
		t.Fatalf("a patient call made %d attempts, want it to keep going to %d", attempts, paced+1)
	}
	// NO WAIT IS EVER LONGER THAN maxProviderWait, and an unbounded loop is
	// where that stops being decoration: the exponent would otherwise overflow
	// into a negative duration somewhere past the sixtieth attempt.
	for index, delay := range waits {
		if delay <= 0 || delay > maxProviderWait {
			t.Fatalf("wait %d = %s, want a positive wait no longer than %s", index, delay, maxProviderWait)
		}
	}
}

// BUT IT STILL ENDS. "Waits it out however long that takes" was written as a
// loop with no exit but the context's, so an account saturated by something
// else — a sibling process on the same key, a neighbour's burst that never
// clears — held the node forever and said nothing an operator could act on.
func TestAPatientCallGivesUpOnceItsPatienceIsSpent(t *testing.T) {
	var mu sync.Mutex
	served := 0
	client := pacedClient(t, rateLimitedUntil(1<<20, &served, &mu))
	// Instant waits: this is the degenerate case the attempt ceiling exists for,
	// where the wall clock never moves and only a count is finite.
	client.wait = func(context.Context, time.Duration) error { return nil }

	ctx := WithPatientRateLimits(context.Background())
	_, err := client.CompleteWithMessages(ctx, userMessages("a"))
	if err == nil {
		t.Fatal("a provider that never lets up should still end the call")
	}
	mu.Lock()
	attempts := served
	mu.Unlock()
	// AND IT IS THE DEADLINE THAT ENDS IT, not `patientAttempts`. Sixty attempts
	// against a per-wait cap was an hour of pacing on paper and an arithmetic
	// backstop in practice; a task node's give-up is four and a half minutes and
	// it is a number a person could be told.
	if attempts < 2 {
		t.Fatalf("a patient call made %d attempts; a 429 is answered by moving first", attempts)
	}
	if attempts > 20 {
		t.Fatalf("a patient call made %d attempts inside one deadline", attempts)
	}
	// It comes out of the SAME DOOR every other provider failure comes out of:
	// the provider's own words, wrapped in the attempt count. internal/session
	// reads exactly this text to decide whether to retry the turn.
	if !strings.Contains(err.Error(), "API error (429)") || !strings.Contains(err.Error(), "slow down") {
		t.Fatalf("the spent call said %q, want the provider's own 429 through the usual path", err)
	}
}

// THERE IS ONE CURRENCY AND IT IS TIME.
//
// There used to be two, because neither bounded what the other could not:
// attempts bounded a provider that says "not yet" instantly and forever, and a
// wall clock bounded one that names its own waits. A deadline bounds both — a
// provider that refuses at connection speed spends it on backoffs, and one that
// names minute-long windows spends it on those — and the two currencies, with
// their four constants, are deleted.
//
// WHAT SCALES IT IS THE ROLE AND NOTHING ELSE. `lane.TurnGiveUp` × the role's
// own patience column, which is the same column `lane.VisiblePatience` is
// scaled by, so a role cannot be patient about when to act and impatient about
// when to stop.
func TestOneDeadlineIsTheWholeOfHowLongACallMayTake(t *testing.T) {
	for _, row := range []struct {
		role lanes.Role
		want time.Duration
	}{
		{lanes.RoleTalk, 90 * time.Second},
		{lanes.RoleLeafAttached, 90 * time.Second},
		{lanes.RoleLeafUnattended, 270 * time.Second},
		{lanes.RoleStanding, 9 * time.Minute},
		{lanes.RoleProbe, 45 * time.Second},
		{lanes.RoleUnknown, 270 * time.Second},
	} {
		if got := row.role.GiveUp(); got != row.want {
			t.Errorf("%s gives up after %s, want %s", row.role, got, row.want)
		}
	}
	// AND EVERY ROLE HAS ONE. A role with no deadline would be a call outside
	// the invariant, and there is no such role.
	for _, role := range lanes.Roles() {
		if role.GiveUp() <= 0 {
			t.Errorf("%s has no deadline", role)
		}
	}
}

// Patience is about PACING and never about faults. A provider that is broken is
// not a provider asking us to come back.
func TestPatienceDoesNotExtendToFaults(t *testing.T) {
	var mu sync.Mutex
	served := 0
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		served++
		mu.Unlock()
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"error":{"message":"broken"}}`))
	})
	client := pacedClient(t, handler)
	client.wait = func(context.Context, time.Duration) error { return nil }

	ctx := WithPatientRateLimits(context.Background())
	if _, err := client.CompleteWithMessages(ctx, userMessages("a")); err == nil {
		t.Fatal("a broken provider should still end the call")
	}
	mu.Lock()
	attempts := served
	mu.Unlock()
	// A FAULT IS RE-ASKED AND IT IS PAID FOR. `maxAttempts` was the private
	// three-fault budget; what bounds a fault storm now is the same deadline
	// everything else is bounded by, and the doubling backoff is what stops it
	// being spent at connection speed.
	if attempts < 2 {
		t.Fatalf("a broken provider was asked %d times; one fault is forgiven", attempts)
	}
	if attempts > 20 {
		t.Fatalf("a broken provider was asked %d times inside one deadline", attempts)
	}
}

// THE CONTEXT IS STILL THE ONLY THING THAT ENDS IT. An interrupt lands on a
// parked call at the next select, not at the end of a backoff and not at the
// end of the pacing.
func TestAnInterruptCutsThroughAParkedCall(t *testing.T) {
	ctx, cancel := context.WithCancel(WithPatientRateLimits(context.Background()))
	defer cancel()

	var mu sync.Mutex
	served := 0
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		served++
		stop := served >= 2
		mu.Unlock()
		if stop {
			// The person hit stop while the call was sitting on a 429.
			cancel()
		}
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"error":{"message":"slow down"}}`))
	})
	// The REAL backoff, because the thing under test is the wait answering the
	// context rather than the timer running out.
	client := pacedClient(t, handler)

	done := make(chan error, 1)
	go func() {
		_, err := client.CompleteWithMessages(ctx, userMessages("a"))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled call should not have succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled call was still parked five seconds later")
	}
}

// The surface hears the park begin and hears it end, once each.
func TestPacingIsAnnouncedWhenItStartsAndTakenBackWhenItEnds(t *testing.T) {
	var mu sync.Mutex
	served := 0
	client := pacedClient(t, rateLimitedUntil(2, &served, &mu))
	client.wait = func(context.Context, time.Duration) error { return nil }

	var said []bool
	var noticed sync.Mutex
	ctx := WithPacingNotice(WithPatientRateLimits(context.Background()), func(parked bool) {
		noticed.Lock()
		said = append(said, parked)
		noticed.Unlock()
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("a")); err != nil {
		t.Fatal(err)
	}
	noticed.Lock()
	defer noticed.Unlock()
	if len(said) != 2 || !said[0] || said[1] {
		t.Fatalf("the pacing notice said %v, want one park and one release", said)
	}
}

// A call that is never paced says nothing at all. The notice is news, not a
// heartbeat.
func TestAnUnpacedCallSaysNothing(t *testing.T) {
	var mu sync.Mutex
	served := 0
	client := pacedClient(t, rateLimitedUntil(0, &served, &mu))

	spoke := false
	ctx := WithPacingNotice(context.Background(), func(bool) { spoke = true })
	if _, err := client.CompleteWithMessages(ctx, userMessages("a")); err != nil {
		t.Fatal(err)
	}
	if spoke {
		t.Fatal("a call that was never paced told somebody it was waiting")
	}
}

// And a call that gives up still takes its own claim back: a surface left
// holding "still waiting" for a call that has ended is worse than one that was
// never told.
func TestPacingIsTakenBackWhenTheCallGivesUp(t *testing.T) {
	var mu sync.Mutex
	served := 0
	client := pacedClient(t, rateLimitedUntil(1000, &served, &mu))
	client.wait = func(context.Context, time.Duration) error { return nil }

	var said []bool
	ctx := WithPacingNotice(context.Background(), func(parked bool) { said = append(said, parked) })
	if _, err := client.CompleteWithMessages(ctx, userMessages("a")); err == nil {
		t.Fatal("every attempt was rate limited; the call should have failed")
	}
	if len(said) != 2 || !said[0] || said[1] {
		t.Fatalf("the pacing notice said %v, want one park and one release", said)
	}
}

// TestTheOneKnobBuysTimeAndNeverASecondSend is the brief's own acceptance for
// the fold, and it asserts BOTH halves of what `response.attempts` had to become.
//
// `attempts: 3` USED TO BE THREE SENDS. It is three times the patience now — a
// 270-second talk deadline where the measurement says 90 — and the thing that
// must NOT move with it is how many requests the call is allowed to make: a
// person asking for more patience is asking to be waited for, never for the same
// bytes to be posted to the same machine again. So the same `machines × shapes +
// 1` bound `control.Next` keeps at a factor of one is checked here at a factor of
// three, on the wire, against a pool that refuses everything.
//
// THE CLOCK IS THE SAME FICTION [TestOneDeadlineBoundsEverything] states: the
// wait seam is stubbed and the dispatcher charges itself for every wait it asked
// for, so a four-and-a-half-minute deadline is proved in microseconds.
func TestTheOneKnobBuysTimeAndNeverASecondSend(t *testing.T) {
	t.Cleanup(func() { lanes.UsePatience(1) })

	// FIRST, WHAT THE PERSON BOUGHT. Every role is scaled, because the factor is
	// about the person and the column is about the work.
	lanes.UsePatience(3)
	for _, row := range []struct {
		role lanes.Role
		want time.Duration
	}{
		{lanes.RoleTalk, 270 * time.Second},
		{lanes.RoleLeafAttached, 270 * time.Second},
		{lanes.RoleProbe, 135 * time.Second},
	} {
		if got := row.role.GiveUp(); got != row.want {
			t.Errorf("at three times the patience %s gives up after %s, want %s", row.role, got, row.want)
		}
	}
	// AND A FACTOR UNDER ONE IS NOT A SHORTER DEADLINE. It is somebody asking
	// this build to give up sooner than the measurement says a turn takes, and
	// it reads as the default.
	lanes.UsePatience(0.25)
	if got := lanes.RoleTalk.GiveUp(); got != lanes.TurnGiveUp {
		t.Errorf("a quarter of the patience gave %s, want the measured %s", got, lanes.TurnGiveUp)
	}

	// SECOND, WHAT IT DID NOT BUY. A pool of four machines that all refuse, run
	// at three times the patience: the call may take 270 seconds and it may not
	// make one send more than it could at 90.
	lanes.UsePatience(3)
	kept := watchedPlan(t)
	pool := &scriptedPool{script: map[string]int{}}
	for index := range 4 {
		name := fmt.Sprintf("Machine%d", index)
		pool.roster = append(pool.roster, name)
		pool.script[name] = http.StatusTooManyRequests
	}
	client := poolClient(t, "patience-three", pool)
	var asked []time.Duration
	client.wait = func(_ context.Context, delay time.Duration) error {
		asked = append(asked, delay)
		return nil
	}
	ctx := WithLaneChoice(context.Background(), lanes.Choice{Order: pool.roster})
	ctx = WithRole(ctx, lanes.RoleTalk)

	began := time.Now()
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err == nil {
		t.Fatal("a pool where every machine refuses should not have answered")
	}
	believed := time.Since(began)
	for _, delay := range asked {
		believed += delay
	}
	if bound := 270*time.Second + maxProviderWait; believed > bound {
		t.Fatalf("the call ran %s against a 270s deadline (one wait of slack is %s): waits %v",
			believed, maxProviderWait, asked)
	}
	// AND THE DEADLINE IT RAN UNDER REALLY WAS THE SCALED ONE. The bound above
	// is satisfied by a call that ends early, and this call DOES end early —
	// four machines and three rungs run out long before four and a half minutes
	// do, which is the whole point: patience is what a call MAY spend, never
	// what it must. So the plan the dispatcher was built with is read directly,
	// and it is the only honest way to say the multiplier arrived.
	//
	// The window is read to the second: the deadline and `Began` are stamped a
	// few microseconds apart by a real clock, and a test that demanded they be
	// equal to the nanosecond would be testing the clock.
	if window := kept.Deadline.Sub(kept.Began).Round(time.Second); window != 270*time.Second {
		t.Fatalf("the call ran under a %s deadline, want the 90s talk give-up times three", window)
	}
	sends := pool.sends()
	if limit := 4*(1+len(relaxRungs)) + 1; len(sends) > limit {
		t.Fatalf("three times the patience made %d sends over four machines, bound %d: %v",
			len(sends), limit, sends)
	}
}
