package triage

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestPromptCarriesTheItemAndTheVocabulary(t *testing.T) {
	it := factory.Item{Repo: "acme/api", Kind: factory.KindIssue, Num: 12, Title: "the meter resets at midnight",
		Body: strings.Repeat("x", BodyMost+500), Labels: []string{"bug", "billing"}}
	p := Prompt(it)
	for _, want := range []string{
		"acme/api", "kind: issue", "#12", "the meter resets at midnight", "labels: bug, billing",
		`"read"`, `"type"`, `"size"`, `"est_usd"`, `"risk"`, `"dup"`, `"priority"`, `"reason"`,
		"bug, feat, chore, question", "S, M, L", "touches money", "touches auth", "has ui", "migration",
		"1 (take it first) to 5 (later)", "at most five words",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	if strings.Contains(p, strings.Repeat("x", BodyMost+1)) {
		t.Errorf("the body was not cut to %d characters", BodyMost)
	}
}

var clean = `{"read": "the meter resets on the wrong clock", "type": "bug", "size": "s", "est_usd": 1.5,
"risk": ["Touches money", "none", "touches money"], "dup": "#9", "priority": 2, "reason": "users see wrong totals every night."}`

func TestParseReadsCleanJSON(t *testing.T) {
	got, err := Parse(clean)
	if err != nil {
		t.Fatal(err)
	}
	want := factory.Triage{Read: "the meter resets on the wrong clock", Type: "bug", Size: "S", Est: 1.5,
		Risk: []string{"touches money"}, Dup: "#9", Priority: 2, Reason: "users see wrong totals every"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse = %+v\nwant   %+v", got, want)
	}
}

func TestParseIsTolerantOfFencesAndProse(t *testing.T) {
	for name, reply := range map[string]string{
		"fenced":   "```json\n" + clean + "\n```",
		"trailing": clean + "\n\nThis is a small fix; {braces} in prose do not matter.",
		"leading":  "Here is the triage:\n" + clean,
	} {
		got, err := Parse(reply)
		if err != nil || got.Type != "bug" || got.Priority != 2 {
			t.Errorf("%s: Parse = %+v, %v", name, got, err)
		}
	}
}

func TestParseRefusesGarbageAndDropsWordsOutsideTheVocabulary(t *testing.T) {
	for _, reply := range []string{"", "I cannot help with that.", "{not json", `{"foo": 1}`} {
		if _, err := Parse(reply); !errors.Is(err, ErrNoRead) {
			t.Errorf("Parse(%q) err = %v, want ErrNoRead", reply, err)
		}
	}
	got, err := Parse(`{"read": "x", "type": "epic", "size": "XL", "est_usd": "$3", "priority": 9, "dup": "maybe 12", "risk": "has ui, migration"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "" || got.Size != "" || got.Priority != 0 || got.Dup != "" || got.Est != 3 ||
		!reflect.DeepEqual(got.Risk, []string{"has ui", "migration"}) {
		t.Fatalf("Parse kept a word outside the vocabulary: %+v", got)
	}
}

func TestApplyFillsOnlyWhatIsEmpty(t *testing.T) {
	it := factory.Item{Triage: factory.Triage{Type: "feat", Size: "L", Risk: []string{"has ui"}, Priority: 4}}
	Apply(&it, factory.Triage{Read: "r", Type: "bug", Size: "S", Est: 2, Risk: []string{"migration"}, Dup: "#3", Priority: 1, Reason: "why"})
	want := factory.Triage{Read: "r", Type: "feat", Size: "L", Est: 2, Risk: []string{"has ui"}, Dup: "#3", Priority: 4, Reason: "why"}
	if !reflect.DeepEqual(it.Triage, want) {
		t.Fatalf("Apply = %+v\nwant  %+v", it.Triage, want)
	}
}

func TestAnOldRiskLevelStillReadsAsADocument(t *testing.T) {
	// A document from before Risk was a list carries it as a level word.
	var tr factory.Triage
	if err := tr.UnmarshalJSON([]byte(`{"Type":"bug","Risk":"high","Read":"r"}`)); err != nil {
		t.Fatal(err)
	}
	if tr.Type != "bug" || tr.Read != "r" || tr.Risk != nil {
		t.Fatalf("old document read as %+v", tr)
	}
	if err := tr.UnmarshalJSON([]byte(`{"Risk":["has ui"]}`)); err != nil || !reflect.DeepEqual(tr.Risk, []string{"has ui"}) {
		t.Fatalf("new document read as %+v, %v", tr, err)
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// recorder is a Call that answers from a script and counts its prompts.
type recorder struct {
	mu      sync.Mutex
	prompts []string
	answer  func(n int) (string, error)
	asked   chan struct{}
}

func (r *recorder) call(_ context.Context, prompt string) (string, error) {
	r.mu.Lock()
	r.prompts = append(r.prompts, prompt)
	n := len(r.prompts)
	r.mu.Unlock()
	defer func() { r.asked <- struct{}{} }()
	return r.answer(n)
}

func waitAsked(t *testing.T, r *recorder) {
	t.Helper()
	select {
	case <-r.asked:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker never asked")
	}
}

func TestWorkerTriagesOneItemATickAndStopsOnCtx(t *testing.T) {
	st := openStore(t)
	var ids []int
	for _, title := range []string{"one", "two", "three"} {
		it, err := st.Create(factory.Item{Repo: "acme/api", Title: title, State: factory.StateNew, Kind: factory.KindIssue})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, it.ID)
	}
	read, err := st.Create(factory.Item{Repo: "acme/api", Title: "read already", State: factory.StateNew, Triage: factory.Triage{Read: "kept"}})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := st.Get(ids[0])
	r := &recorder{asked: make(chan struct{}, 8), answer: func(int) (string, error) { return clean, nil }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Worker(ctx, st, r.call, 150*time.Millisecond, nil); close(done) }()

	waitAsked(t, r)
	triaged := 0
	for wait := time.Now().Add(time.Second); triaged == 0 && time.Now().Before(wait); time.Sleep(2 * time.Millisecond) {
		items, _ := st.List()
		for _, it := range items {
			if !it.Triage.TriagedAt.IsZero() {
				triaged++
			}
		}
	}
	if triaged != 1 {
		t.Fatalf("after one tick %d items were triaged, want exactly one", triaged)
	}
	for range 2 {
		waitAsked(t, r)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker did not stop with its context")
	}
	for _, id := range ids {
		it, _ := st.Get(id)
		if it.Triage.TriagedAt.IsZero() || it.Triage.Read != "the meter resets on the wrong clock" || it.Triage.Priority != 2 {
			t.Errorf("#%d not triaged: %+v", id, it.Triage)
		}
	}
	if got, _ := st.Get(read.ID); got.Triage.Read != "kept" || !got.Triage.TriagedAt.IsZero() {
		t.Errorf("an item with a read was read again: %+v", got.Triage)
	}
	if after, _ := st.Get(ids[0]); !after.Changed.Equal(before.Changed) {
		t.Errorf("a read moved the row's age: Changed %v -> %v", before.Changed, after.Changed)
	}
	if len(r.prompts) != 3 {
		t.Errorf("%d calls for three items", len(r.prompts))
	}
}

func TestWorkerStampsAnUnreadableAnswerAndNeverAsksAgain(t *testing.T) {
	st := openStore(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Title: "x", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	r := &recorder{asked: make(chan struct{}, 8), answer: func(int) (string, error) { return "sorry, no", nil }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Worker(ctx, st, r.call, 5*time.Millisecond, nil)
	waitAsked(t, r)
	time.Sleep(200 * time.Millisecond)
	got, _ := st.Get(it.ID)
	if got.Triage.TriagedAt.IsZero() || got.Triage.Read != "" {
		t.Fatalf("an unreadable answer left %+v", got.Triage)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.prompts) != 1 {
		t.Fatalf("one item was asked about %d times", len(r.prompts))
	}
}

func TestWorkerGivesAFailingCallItsTriesThenStamps(t *testing.T) {
	st := openStore(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Title: "x", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	r := &recorder{asked: make(chan struct{}, 8), answer: func(int) (string, error) { return "", errors.New("refused") }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Worker(ctx, st, r.call, time.Millisecond, nil)
	for n := 1; n <= CallTries; n++ {
		waitAsked(t, r)
		if got, _ := st.Get(it.ID); n < CallTries && !got.Triage.TriagedAt.IsZero() {
			t.Fatalf("stamped after %d failed calls, want %d", n, CallTries)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if got, _ := st.Get(it.ID); got.Triage.TriagedAt.IsZero() {
		t.Fatal("an item whose call always fails was never stamped")
	}
}
