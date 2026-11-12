package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE CLOCK MOVES ONLY WHEN THE TEST SAYS SO. Callbacks run outside its lock,
// just as a real timer does, so the agent still owns all scheduling decisions.
type jobWakeClock struct {
	mu     sync.Mutex
	now    time.Duration
	timers []*jobWakeTimer
}

type jobWakeTimer struct {
	at      time.Duration
	fire    func()
	stopped bool
}

func (c *jobWakeClock) after(delay time.Duration, fire func()) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := &jobWakeTimer{at: c.now + delay, fire: fire}
	c.timers = append(c.timers, timer)
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		timer.stopped = true
	}
}

func (c *jobWakeClock) advance(delay time.Duration) {
	c.mu.Lock()
	c.now += delay
	var fires []func()
	for _, timer := range c.timers {
		if !timer.stopped && timer.at <= c.now {
			timer.stopped = true
			fires = append(fires, timer.fire)
		}
	}
	c.mu.Unlock()
	for _, fire := range fires {
		fire()
	}
}

func jobWakeFixture(t *testing.T, configure ...func(*Config)) (*Agent, *scriptedCompleter, *jobWakeClock, <-chan (<-chan Event), string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "session.jsonl")
	client := &scriptedCompleter{}
	a, _ := newTestAgent(t, client, func(cfg *Config) {
		cfg.SessionFile = file
		for _, apply := range configure {
			apply(cfg)
		}
	})
	clock := &jobWakeClock{}
	a.jobWakeAfter = clock.after
	wakes, leave := a.WatchWakes()
	t.Cleanup(leave)
	return a, client, clock, wakes, file
}

func TestJobWakeWindowVisionFallbackKeepsPendingAnswer(t *testing.T) {
	a, client, clock, wakes, file := jobWakeFixture(t, func(cfg *Config) {
		cfg.SupportsImages = func(model string) bool { return model == "vendor/eyes" }
		withSlot(map[string]string{"vision": "vendor/eyes"})(cfg)
	})
	entered, release := make(chan struct{}), make(chan struct{})
	client.steps = []step{func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
		close(entered)
		<-release
		return textResponse("a picture"), nil
	}}
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	one := settlementJob(t, a.jobs)
	a.jobs.settleExit(one, 7)
	stream, err := a.SubmitImage(context.Background(), "what is this?", []Image{
		{Path: writeImage(t, t.TempDir(), "shot.png", "PHOTOBYTES")},
	})
	if err != nil {
		t.Fatal(err)
	}
	settlementWait(t, entered, "vision request")
	if sent := client.request(0); len(sent) != 1 || len(imagePartURLs(sent[0])) != 1 {
		t.Fatal("vision did not receive only the person's image message")
	}
	clock.advance(jobWakeDelay)
	noJobWake(t, wakes)
	unblock()
	collect(t, stream)
	select {
	case answer := <-wakes:
		collect(t, answer)
	default:
		t.Fatal("vision finished without waking an answer to the pending ending")
	}
	clock.advance(jobWakeDelay)
	noJobWake(t, wakes)
	if got := client.requests(); got != 2 {
		t.Fatalf("requests = %d, want vision and one ordinary answer", got)
	}
	if got := client.model(1); got != a.Model() {
		t.Fatalf("pending ending answered by %q, want chat model %q", got, a.Model())
	}
	assertJobNotes(t, userTextIn(client.request(1)), one)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	assertJobNotes(t, string(journal), one)
}

func TestJobWakeWindowCloseKeepsEndingAfterStoppedTimer(t *testing.T) {
	a, client, clock, wakes, file := jobWakeFixture(t)
	one := settlementJob(t, a.jobs)
	a.jobs.settleExit(one, 7)
	if err := a.StopWork(); err != nil {
		t.Fatal(err)
	}
	clock.advance(jobWakeDelay)
	noJobWake(t, wakes)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	assertJobNotes(t, string(journal), one)
	if got := conversationRequests(client); got != 0 {
		t.Fatalf("requests after stop and close = %d, want 0", got)
	}
}

func noJobWake(t *testing.T, wakes <-chan (<-chan Event)) {
	t.Helper()
	select {
	case stream, ok := <-wakes:
		if ok {
			collect(t, stream)
			t.Fatal("unexpected turn")
		}
	default:
	}
}

func finishJobWake(t *testing.T, wakes <-chan (<-chan Event)) {
	t.Helper()
	select {
	case stream, ok := <-wakes:
		if !ok {
			t.Fatal("wake lane closed")
		}
		collect(t, stream)
	case <-time.After(10 * time.Second):
		t.Fatal("ending never woke a turn")
	}
}

func assertJobNotes(t *testing.T, text string, jobs ...*job) {
	t.Helper()
	last := -1
	for _, one := range jobs {
		needle := fmt.Sprintf("job %d exited", one.id)
		at := strings.Index(text, needle)
		if at <= last || strings.Count(text, needle) != 1 {
			t.Fatalf("ending %q missing, duplicated or out of order:\n%s", needle, text)
		}
		last = at
	}
}

func TestJobWakeWindowCollectsFailures(t *testing.T) {
	a, client, clock, wakes, file := jobWakeFixture(t)
	var jobs []*job
	for range 3 {
		one := settlementJob(t, a.jobs)
		jobs = append(jobs, one)
		a.jobs.settleExit(one, 7)
		noJobWake(t, wakes)
		clock.advance(500 * time.Millisecond)
	}
	noJobWake(t, wakes)
	clock.advance(500 * time.Millisecond)
	finishJobWake(t, wakes)
	clock.advance(3 * time.Second)
	noJobWake(t, wakes)
	if got := conversationRequests(client); got != 1 {
		t.Fatalf("turns = %d, want 1", got)
	}
	client.mu.Lock()
	text := userTextIn(client.seen[0])
	client.mu.Unlock()
	assertJobNotes(t, text, jobs...)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	assertJobNotes(t, string(journal), jobs...)
}

func TestJobWakeWindowSeparatesDistantFailures(t *testing.T) {
	a, client, clock, wakes, _ := jobWakeFixture(t)
	for range 2 {
		a.jobs.settleExit(settlementJob(t, a.jobs), 7)
		noJobWake(t, wakes)
		clock.advance(3 * time.Second)
		finishJobWake(t, wakes)
	}
	if got := conversationRequests(client); got != 2 {
		t.Fatalf("turns = %d, want 2", got)
	}
}

func TestJobWakeWindowOwedEndingAbsorbsPending(t *testing.T) {
	a, client, clock, wakes, _ := jobWakeFixture(t)
	first := settlementJob(t, a.jobs)
	a.jobs.settleExit(first, 7)
	noJobWake(t, wakes)
	clock.advance(time.Second)
	owed := settlementJob(t, a.jobs)
	owed.owed = true
	park := a.taskNewsWait()
	a.jobs.settleExit(owed, 0)
	select {
	case <-park:
	default:
		t.Fatal("owed ending did not release parked worker immediately")
	}
	finishJobWake(t, wakes)
	clock.advance(3 * time.Second)
	noJobWake(t, wakes)
	if got := conversationRequests(client); got != 1 {
		t.Fatalf("turns = %d, want 1", got)
	}
	client.mu.Lock()
	text := userTextIn(client.seen[0])
	client.mu.Unlock()
	assertJobNotes(t, text, first, owed)
}

func TestJobWakeWindowCleanAndKilledStayQuiet(t *testing.T) {
	for _, killed := range []bool{false, true} {
		t.Run(fmt.Sprintf("killed=%v", killed), func(t *testing.T) {
			a, client, clock, wakes, _ := jobWakeFixture(t)
			one := settlementJob(t, a.jobs)
			if killed {
				one.requestKill()
			}
			a.jobs.settleExit(one, 0)
			clock.advance(3 * time.Second)
			noJobWake(t, wakes)
			if got := conversationRequests(client); got != 0 {
				t.Fatalf("turns = %d", got)
			}
			a.mu.Lock()
			defer a.mu.Unlock()
			if len(a.steering) != 0 || a.jobWake != nil {
				t.Fatal("quiet ending scheduled work")
			}
			want := 1
			if killed {
				want = 0
			}
			if len(a.ambient) != want {
				t.Fatalf("quiet notes = %d, want %d", len(a.ambient), want)
			}
		})
	}
}

func TestJobWakeWindowCloseWithdrawsTimer(t *testing.T) {
	a, client, clock, wakes, file := jobWakeFixture(t)
	one := settlementJob(t, a.jobs)
	a.jobs.settleExit(one, 7)
	noJobWake(t, wakes)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	assertJobNotes(t, string(journal), one)
	if len(clock.timers) != 1 || !clock.timers[0].stopped {
		t.Fatal("close left a timer running")
	}
	// A CALLBACK ALREADY DISPATCHED MUST BE INERT TOO, not only a timer whose
	// Stop won the race against the runtime clock.
	clock.timers[0].fire()
	clock.advance(3 * time.Second)
	noJobWake(t, wakes)
	if got := conversationRequests(client); got != 0 {
		t.Fatalf("turns after close = %d", got)
	}
}

func TestJobWakeWindowNotifyAndMidTurn(t *testing.T) {
	t.Run("explicit notification", func(t *testing.T) {
		a, _, clock, wakes, _ := jobWakeFixture(t)
		one := settlementJob(t, a.jobs)
		one.notify = true
		a.jobs.settleExit(one, 0)
		noJobWake(t, wakes)
		clock.advance(2 * time.Second)
		finishJobWake(t, wakes)
	})
	t.Run("mid turn", func(t *testing.T) {
		a, client, clock, wakes, _ := jobWakeFixture(t)
		entered, release := make(chan struct{}), make(chan struct{})
		client.steps = []step{func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			close(entered)
			<-release
			return textResponse("done"), nil
		}}
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		t.Cleanup(unblock)
		stream, err := a.Submit(context.Background(), "work")
		if err != nil {
			t.Fatal(err)
		}
		settlementWait(t, entered, "model request")
		a.jobs.settleExit(settlementJob(t, a.jobs), 7)
		if len(clock.timers) != 0 {
			t.Fatal("mid-turn ending armed idle timer")
		}
		unblock()
		collect(t, stream)
		// THE EXISTING LAST-REQUEST RULE STILL OWES ONE ANSWER to a note that
		// arrived too late for the running request to read it.
		finishJobWake(t, wakes)
		clock.advance(3 * time.Second)
		noJobWake(t, wakes)
	})
}

// A PERSON'S TURN READS THE SAME QUEUE, so it consumes the pending wake just
// as an owed ending does. An old callback must not wake a newer window early.
func TestJobWakeWindowSubmittedTurnWithdrawsOldCallback(t *testing.T) {
	a, client, clock, wakes, _ := jobWakeFixture(t)
	a.jobs.settleExit(settlementJob(t, a.jobs), 7)
	noJobWake(t, wakes)
	clock.mu.Lock()
	old := clock.timers[0]
	clock.mu.Unlock()
	stream, err := a.Submit(context.Background(), "read the news")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, stream)
	clock.mu.Lock()
	stopped := old.stopped
	clock.mu.Unlock()
	if !stopped {
		t.Fatal("submitted turn left a timer running")
	}
	a.jobs.settleExit(settlementJob(t, a.jobs), 7)
	noJobWake(t, wakes)
	old.fire()
	noJobWake(t, wakes)
	clock.advance(2 * time.Second)
	finishJobWake(t, wakes)
	if got := conversationRequests(client); got != 2 {
		t.Fatalf("turns = %d, want 2", got)
	}
}
