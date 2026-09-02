package lane

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// ── A ROUTER IS KNOWN BY WHAT IT ANSWERS ────────────────────────────────────
//
// This build used to decide whether an endpoints page was ever fetched by
// testing the base URL for `openrouter.ai`, so a proxy, a mirror, a self-hosted
// router or the router reached by its IP silently got no sheet (issue #373).
// The law is that the base is ASKED, ONCE, and its answer remembered; the
// hostname survives only as a hint that skips the asking. Every test here is
// one clause of that law, driven through a fetcher that answers whatever the
// test says and counts how often it was made to.

// answering is a [Fetcher] whose reply the test scripts, and which counts its
// calls so that "was the base asked again" is a number and not a guess.
type answering struct {
	mu    sync.Mutex
	reply func() (string, error)
	calls int
}

// waitingAnswer holds one fetch in flight until the test has moved the sheet
// to another base.
type waitingAnswer struct {
	started chan struct{}
	release chan struct{}
}

func (w *waitingAnswer) Fetch(context.Context, string, string) (io.ReadCloser, error) {
	close(w.started)
	<-w.release
	return io.NopCloser(strings.NewReader(onePage)), nil
}

func (a *answering) Fetch(context.Context, string, string) (io.ReadCloser, error) {
	a.mu.Lock()
	a.calls++
	reply := a.reply
	a.mu.Unlock()
	body, err := reply()
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

func (a *answering) asked() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

func (a *answering) answer(reply func() (string, error)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reply = reply
}

// onePage is the smallest endpoints page that decodes to a lane.
const onePage = `{"data":{"endpoints":[{"provider_name":"Cloudflare","latency_last_30m":{"p50":700},"throughput_last_30m":{"p50":60}}]}}`

func servesAPage() (string, error) { return onePage, nil }
func noPageHere() (string, error) {
	return "", fmt.Errorf("the router answered 404 Not Found: %w", ErrNoSheetHere)
}
func aBadAfternoon() (string, error) { return "", errors.New("the router answered 500 Internal Server Error") }

// probed is a sheet wired at a base nobody has vouched for, with a clock the
// test turns by hand and a cache directory of its own.
func probed(t *testing.T, reply func() (string, error)) (*sheet, *answering, *time.Time) {
	t.Helper()
	fetch := &answering{reply: reply}
	now := time.Date(2026, time.August, 25, 9, 0, 0, 0, time.UTC)
	s := newSheet()
	s.now = func() time.Time { return now }
	s.wire("http://127.0.0.1:1/api/v1", "", fetch, false)
	s.cacheIn(t.TempDir())
	return s, fetch, &now
}

func (s *sheet) answer() sheetAnswer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.answered
}

// TestABaseThatServesASheetIsRememberedAsServing is the happy half of the law:
// the probe is the first fetch, a page coming back is the whole answer, and a
// base that answered is never held back afterwards — not even by a 404 about
// some model it happens not to serve.
func TestABaseThatServesASheetIsRememberedAsServing(t *testing.T) {
	s, fetch, _ := probed(t, servesAPage)
	if s.answer() != answerUnasked {
		t.Fatal("a base nobody vouched for was wired with an answer already filed")
	}
	if err := s.Refresh(context.Background(), scriptedModel); err != nil {
		t.Fatalf("the first refresh: %v", err)
	}
	if s.answer() != answerServes || fetch.asked() != 1 {
		t.Fatalf("after one page came back the base is %v and was asked %d times", s.answer(), fetch.asked())
	}
	if len(s.Rows(scriptedModel)) != 1 {
		t.Fatalf("the page that answered the probe was not kept as rows: %d", len(s.Rows(scriptedModel)))
	}

	// A ROUTER ASKED ABOUT A MODEL IT DOES NOT SERVE ANSWERS 404 ABOUT THAT
	// MODEL, and that must not cost every other model its lanes.
	fetch.answer(noPageHere)
	if err := s.Refresh(context.Background(), "nobody/serves-this"); !errors.Is(err, ErrNoSheetHere) {
		t.Fatalf("a 404 about one model came back as %v", err)
	}
	if s.answer() != answerServes {
		t.Fatal("one model's 404 turned a router that had served a page into a sheetless base")
	}
	fetch.answer(servesAPage)
	if err := s.Refresh(context.Background(), scriptedModel); err != nil || fetch.asked() != 3 {
		t.Fatalf("a serving base was held back: err %v, asked %d times", err, fetch.asked())
	}
}

// TestASheetlessBaseIsNotAskedAgainUntilItsAnswerIsStale is acceptance 2 of
// #373: a base that said there is no page is believed, for exactly [sheetTTL],
// and then asked exactly once more.
func TestASheetlessBaseIsNotAskedAgainUntilItsAnswerIsStale(t *testing.T) {
	s, fetch, now := probed(t, noPageHere)
	if err := s.Refresh(context.Background(), scriptedModel); !errors.Is(err, ErrNoSheetHere) {
		t.Fatalf("the base's own 404 came back as %v", err)
	}
	if s.answer() != answerSheetless || fetch.asked() != 1 {
		t.Fatalf("after a 404 the base is %v and was asked %d times", s.answer(), fetch.asked())
	}

	// HELD BACK: the next refresh, and one for another model, go nowhere.
	for _, model := range []string{scriptedModel, "qwen/qwen3.5-9b"} {
		if err := s.Refresh(context.Background(), model); !errors.Is(err, ErrNoSheetHere) {
			t.Fatalf("a held-back refresh of %s came back as %v", model, err)
		}
	}
	if fetch.asked() != 1 {
		t.Fatalf("a base that said there is no page was asked again %d times inside the TTL", fetch.asked()-1)
	}

	// A MOMENT SHORT OF STALE IS STILL FRESH.
	*now = now.Add(sheetTTL - time.Second)
	if err := s.Refresh(context.Background(), scriptedModel); !errors.Is(err, ErrNoSheetHere) || fetch.asked() != 1 {
		t.Fatalf("a second short of the TTL the base was asked again: err %v, asked %d", err, fetch.asked())
	}

	// STALE: asked exactly once more, and this time it has grown a page.
	*now = now.Add(time.Second)
	fetch.answer(servesAPage)
	if err := s.Refresh(context.Background(), scriptedModel); err != nil {
		t.Fatalf("the refresh after the TTL: %v", err)
	}
	if fetch.asked() != 2 || s.answer() != answerServes {
		t.Fatalf("after the TTL the base was asked %d times and is %v", fetch.asked(), s.answer())
	}
}

// TestABadAfternoonIsNotAVerdictAboutTheBase keeps a 500, a 429 and a severed
// connection from being read as "this is not a router": only [ErrNoSheetHere]
// makes a base sheetless, and an ordinary error leaves it asked again at once.
func TestABadAfternoonIsNotAVerdictAboutTheBase(t *testing.T) {
	s, fetch, _ := probed(t, aBadAfternoon)
	err := s.Refresh(context.Background(), scriptedModel)
	if err == nil || errors.Is(err, ErrNoSheetHere) {
		t.Fatalf("a 500 came back as %v", err)
	}
	if s.answer() != answerUnasked {
		t.Fatalf("one 500 filed an answer about the base: %v", s.answer())
	}
	fetch.answer(servesAPage)
	if err := s.Refresh(context.Background(), scriptedModel); err != nil || fetch.asked() != 2 {
		t.Fatalf("a base that had a bad afternoon was held back: err %v, asked %d", err, fetch.asked())
	}
	if s.answer() != answerServes {
		t.Fatal("the page that came back after the bad afternoon was not remembered")
	}
}

// TestAKnownBaseIsNeverProbed is acceptance 3: the hint is honoured, the wiring
// itself makes no request, and the shipped router's first refresh is exactly
// the fetch it always was — with a 404 about one model unable to unseat it.
func TestAKnownBaseIsNeverProbed(t *testing.T) {
	fetch := &answering{reply: noPageHere}
	s := newSheet()
	s.wire("https://openrouter.ai/api/v1", "sk-or-test", fetch, true)
	s.cacheIn(t.TempDir())
	if fetch.asked() != 0 {
		t.Fatalf("wiring a known base made %d requests", fetch.asked())
	}
	if s.answer() != answerServes {
		t.Fatalf("a base vouched for was wired as %v", s.answer())
	}
	if err := s.Refresh(context.Background(), "nobody/serves-this"); !errors.Is(err, ErrNoSheetHere) {
		t.Fatalf("a 404 about one model came back as %v", err)
	}
	if s.answer() != answerServes || !s.askable(time.Now()) {
		t.Fatal("a 404 about one model turned the known router sheetless")
	}
	fetch.answer(servesAPage)
	if err := s.Refresh(context.Background(), scriptedModel); err != nil || fetch.asked() != 2 {
		t.Fatalf("the known base was held back: err %v, asked %d", err, fetch.asked())
	}
}

// TestAMovedBaseForgetsWhatTheOldOneAnswered: the answer is cached PER BASE,
// and what one router said is not evidence about the next address.
func TestAMovedBaseForgetsWhatTheOldOneAnswered(t *testing.T) {
	s, fetch, _ := probed(t, noPageHere)
	if err := s.Refresh(context.Background(), scriptedModel); !errors.Is(err, ErrNoSheetHere) {
		t.Fatalf("the first base's 404 came back as %v", err)
	}
	if s.answer() != answerSheetless {
		t.Fatalf("the first base is %v after saying it has no page", s.answer())
	}

	// The same base again, with a fresh bearer, keeps its answer: it is the
	// same fact told twice, and the client wiring after the process beat must
	// not reopen a question the base has already closed.
	s.wire("http://127.0.0.1:1/api/v1/", "sk-later", fetch, false)
	if s.answer() != answerSheetless {
		t.Fatalf("re-wiring the same base forgot its answer: %v", s.answer())
	}

	// A different base starts unasked, and is asked.
	fetch.answer(servesAPage)
	s.wire("http://127.0.0.1:2/api/v1", "", fetch, false)
	if s.answer() != answerUnasked {
		t.Fatalf("a new base inherited the old one's answer: %v", s.answer())
	}
	if err := s.Refresh(context.Background(), scriptedModel); err != nil || fetch.asked() != 2 {
		t.Fatalf("the new base was not asked: err %v, asked %d", err, fetch.asked())
	}
	if s.answer() != answerServes {
		t.Fatalf("the new base's page was not remembered: %v", s.answer())
	}
}

// TestAMovedBaseDoesNotFileAnAnswerThatWasAlreadyInFlight closes the window
// between fetching a page from one base and filing it under the next one.
func TestAMovedBaseDoesNotFileAnAnswerThatWasAlreadyInFlight(t *testing.T) {
	const (
		baseA = "http://127.0.0.1:1/api/v1"
		baseB = "http://127.0.0.1:2/api/v1"
	)
	now := time.Date(2026, time.August, 25, 9, 0, 0, 0, time.UTC)
	waiting := &waitingAnswer{started: make(chan struct{}), release: make(chan struct{})}
	s := newSheet()
	s.now = func() time.Time { return now }
	s.wire(baseA, "", waiting, false)
	s.cacheIn(t.TempDir())

	done := make(chan error, 1)
	go func() { done <- s.Refresh(context.Background(), scriptedModel) }()
	<-waiting.started
	serving := &answering{reply: servesAPage}
	s.wire(baseB, "", serving, false)
	close(waiting.release)
	if err := <-done; !errors.Is(err, errSheetMoved) {
		t.Fatalf("the old base's in-flight page came back as %v", err)
	}
	if s.answer() != answerUnasked {
		t.Fatalf("the new base inherited the old one's answer: %v", s.answer())
	}
	if s.Rows(scriptedModel) != nil {
		t.Fatal("the old base's rows were filed under the new base")
	}

	if err := s.Refresh(context.Background(), scriptedModel); err != nil {
		t.Fatalf("the new base's refresh: %v", err)
	}
	if s.answer() != answerServes || len(s.Rows(scriptedModel)) != 1 {
		t.Fatalf("the new base was filed as %v with %d rows", s.answer(), len(s.Rows(scriptedModel)))
	}
}

// TestTheBeatIsQuietOnASheetlessBase drives the law through the beat and a
// real stub router that publishes no endpoints page: the first model's fetch
// is the probe, and the second model in the same round costs the base nothing
// — no request, no error surfaced, no rows.
func TestTheBeatIsQuietOnASheetlessBase(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	Default().Reset()
	const other = "qwen/qwen3.5-9b"
	s, stub, fetch := wired(t)
	stub.Model(other, scripted()...)
	stub.Sheetless()

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Beat(ctx, s, []string{scriptedModel, other}, time.Hour)
		close(done)
	}()
	waitFor(t, func() bool { return stub.Sheets(scriptedModel) == 1 })
	// The first pass goes on to the second model at once; give it the moment
	// it needs to be held back before the beat is stopped.
	waitFor(t, func() bool { _, _, calls := fetch.asked(); return calls >= 1 && s.answer() == answerSheetless })
	time.Sleep(20 * time.Millisecond)
	stop()
	<-done

	if got := stub.Sheets(other); got != 0 {
		t.Fatalf("a base that had said there is no page was asked again about another model: %d readings", got)
	}
	if s.Rows(scriptedModel) != nil || s.Rows(other) != nil {
		t.Fatal("a sheetless base produced rows")
	}
	if believed := Default().Ledger().Beliefs(scriptedModel); len(believed) != 0 {
		t.Fatalf("the beat primed %d lanes from a base with no sheet", len(believed))
	}
}
