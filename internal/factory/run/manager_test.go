package run

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// fakeTalk is a manager conversation in memory: what the runner said into
// it, and what the person typed.
type fakeTalk struct {
	mu     sync.Mutex
	said   map[string][]string
	person map[string][]Heard
	last   map[string]string
	refuse int // how many Says to refuse before taking them
}

func newFakeTalk() *fakeTalk {
	return &fakeTalk{said: map[string][]string{}, person: map[string][]Heard{}, last: map[string]string{}}
}

func (f *fakeTalk) Say(transcript, line string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse > 0 {
		f.refuse--
		return errors.New("the conversation is in the middle of a turn")
	}
	f.said[transcript] = append(f.said[transcript], line)
	return nil
}

func (f *fakeTalk) Heard(transcript string, after time.Time) ([]Heard, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Heard
	for _, h := range f.person[transcript] {
		if h.At.After(after) {
			out = append(out, h)
		}
	}
	return out, nil
}

func (f *fakeTalk) LastSaid(transcript string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[transcript]
}

// typed is the person typing words into the conversation now.
func (f *fakeTalk) typed(transcript, words string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	at := time.Now()
	if hs := f.person[transcript]; len(hs) > 0 && !at.After(hs[len(hs)-1].At) {
		at = hs[len(hs)-1].At.Add(time.Microsecond)
	}
	f.person[transcript] = append(f.person[transcript], Heard{At: at, Words: words})
}

func (f *fakeTalk) lines(transcript string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.said[transcript]...)
}

// waitSaid waits until the conversation holds line.
func (f *fakeTalk) waitSaid(t *testing.T, transcript, line string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, l := range f.lines(transcript) {
			if l == line {
				return
			}
		}
		time.Sleep(3 * time.Millisecond)
	}
	t.Fatalf("the manager never said %q; it said:\n%s", line, strings.Join(f.lines(transcript), "\n"))
}

// managed is a rig whose items get a manager conversation named for them.
func managed(t *testing.T, exec map[factory.StageKind]Executor, talk *fakeTalk, poll time.Duration) (*rig, *int) {
	made := new(int)
	var mu sync.Mutex
	g := newRig(t, exec, func(o *Options) {
		o.Talk = talk
		o.TalkPoll = poll
		o.Manager = func(_ context.Context, it factory.Item) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			if it.Talk != "" {
				return it.Talk, nil
			}
			*made++
			return "manager-" + strconv.Itoa(it.ID), nil
		}
	})
	return g, made
}

func TestALaunchMakesTheManagerWhenTheItemHasNone(t *testing.T) {
	talk := newFakeTalk()
	g, made := managed(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) { return done("ok"), nil }),
	}, talk, time.Hour)
	id := g.add("fix it", chat("plan"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	if it.Talk != "manager-"+strconv.Itoa(id) || *made != 1 {
		t.Fatalf("the item's conversation is %q after %d makings; want the manager made once", it.Talk, *made)
	}
	talk.waitSaid(t, it.Talk, sayLanded)
}

func TestTheManagerHearsEveryStageInOrderInTheFloorsWords(t *testing.T) {
	talk := newFakeTalk()
	talk.last["write-chat"] = "ledger.go +9 -1"
	g, _ := managed(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
			switch job.Stage.Name {
			case "plan":
				return factory.StageResult{Done: true, Notes: []string{"Read the ledger first."}, Spent: 0.04}, nil
			}
			return factory.StageResult{Done: true, Chat: "write-chat", Spent: 0.27}, nil
		}),
		factory.StageCheck: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
			return factory.StageResult{Done: true, Output: "1 passed, 1 failed", Claims: []factory.Claim{
				{Text: "refunds count once", OK: true, Evidence: "exit 0"},
				{Text: "the meter is honest", OK: false},
			}}, nil
		}),
	}, talk, time.Hour)
	test := factory.Stage{Name: "test", Kind: factory.StageCheck, Ask: "go test", Until: factory.UntilProven, Max: 1}
	id := g.add("fix the ledger", chat("plan"), chat("write"), test)
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	q := "test is not proven after 1 round: 1 of 2 claims shown · one more round, or go on as is?"
	g.asked(id, q)
	m := "manager-" + strconv.Itoa(id)
	talk.waitSaid(t, m, "test failed 1 of 2 · asking you: "+q)
	if err := g.r.Answer(id, false, ""); err != nil {
		t.Fatal(err)
	}
	g.waitState(id, factory.StateLanded)
	talk.waitSaid(t, m, sayLanded)
	want := []string{
		"plan started",
		"plan done · $0.04 · Read the ledger first.",
		"write started",
		"write done · $0.27 · ledger.go +9 -1",
		"test started",
		"test failed 1 of 2 · asking you: " + q,
		"answered: no",
		"test done · 1 passed, 1 failed",
		"landed · proof sheet ready · your approval",
	}
	if got := talk.lines(m); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the manager heard:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestAStageDoneLineSaysHowLongWhatItCostAndWhatItSaid(t *testing.T) {
	if got := doneLine("plan", 2*time.Minute+10*time.Second, 0.04, "read the ledger\nfirst"); got != "plan done · 2m · $0.04 · read the ledger first" {
		t.Fatalf("done line %q", got)
	}
	if got := doneLine("write", 0, 0, ""); got != "write done" {
		t.Fatalf("an empty done line says %q", got)
	}
	if got := fmt.Sprintf(sayCapAsk, loopUSD(5)); got != "budget of $5 reached · asking you" {
		t.Fatalf("cap line %q", got)
	}
}

func TestWhatThePersonTypesDuringARunIsTheNextStagesSteer(t *testing.T) {
	talk := newFakeTalk()
	var mu sync.Mutex
	var writeNotes []string
	var g *rig
	g, _ = managed(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
			if job.Stage.Name == "plan" {
				talk.typed(job.Item.Talk, "use the second ledger")
			} else {
				mu.Lock()
				writeNotes = append([]string(nil), job.Notes...)
				mu.Unlock()
			}
			return done("ok"), nil
		}),
	}, talk, time.Hour)
	id := g.add("fix it", chat("plan"), chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	mu.Lock()
	notes := strings.Join(writeNotes, "|")
	mu.Unlock()
	if !strings.Contains(notes, "use the second ledger") {
		t.Fatalf("write's brief read %q, without the person's steer", notes)
	}
	if !logHas(it, "steer: use the second ledger") {
		t.Error("the item's log does not say the steer")
	}
	talk.waitSaid(t, it.Talk, "steer: use the second ledger")
	lines := talk.lines(it.Talk)
	steerAt, writeAt := -1, -1
	for k, l := range lines {
		switch l {
		case "steer: use the second ledger":
			steerAt = k
		case "write started":
			writeAt = k
		}
	}
	if steerAt < 0 || writeAt < steerAt {
		t.Fatalf("the steer is not said before write starts:\n%s", strings.Join(lines, "\n"))
	}
	if it.Heard.IsZero() {
		t.Error("the item does not remember what it heard")
	}
	for _, n := range it.Notes {
		if n == "use the second ledger" {
			t.Error("a steer during a run became the item's standing notes")
		}
	}
}

func TestWhatThePersonTypedBeforeARunIsTheFirstStagesNotes(t *testing.T) {
	talk := newFakeTalk()
	var mu sync.Mutex
	var planNotes []string
	g, made := managed(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
			if job.Stage.Name == "plan" {
				mu.Lock()
				planNotes = append([]string(nil), job.Notes...)
				mu.Unlock()
			}
			return done("ok"), nil
		}),
	}, talk, time.Hour)
	id := g.add("fix it", chat("plan"))
	m := "talked-before"
	if err := g.st.Update(id, func(it *factory.Item) error { it.Talk = m; return nil }); err != nil {
		t.Fatal(err)
	}
	talk.typed(m, "keep the old API")
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	if *made != 0 {
		t.Errorf("the item had a conversation and the launch made %d more", *made)
	}
	mu.Lock()
	got := strings.Join(planNotes, "|")
	mu.Unlock()
	if got != "keep the old API" {
		t.Fatalf("plan's brief read %q, want the person's brief", got)
	}
	if len(it.Notes) != 1 || it.Notes[0] != "keep the old API" || it.Heard.IsZero() {
		t.Fatalf("the item's notes are %q (heard %v)", it.Notes, it.Heard)
	}
	for _, l := range talk.lines(m) {
		if strings.HasPrefix(l, "steer: ") {
			t.Errorf("the brief was said back as a steer: %q", l)
		}
	}
}

func TestAYesTypedInTheManagerAnswersTheQuestionThatStands(t *testing.T) {
	talk := newFakeTalk()
	g, _ := managed(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) { return done("ok"), nil }),
	}, talk, 5*time.Millisecond)
	id := g.add("fix it", chat("plan"), chat("write"))
	if err := g.st.Update(id, func(it *factory.Item) error { it.Gate = factory.GatePlan; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	q := "plan is ready · go, or change it?"
	it := g.asked(id, q)
	talk.waitSaid(t, it.Talk, "asking you: "+q)
	talk.typed(it.Talk, "Yes.")
	it = g.waitState(id, factory.StateLanded)
	talk.waitSaid(t, it.Talk, "answered: yes")
	for _, l := range talk.lines(it.Talk) {
		if strings.HasPrefix(l, "steer: ") {
			t.Errorf("the answer was also taken as a steer: %q", l)
		}
	}
}

func TestATypedAnswerIsReadByTheQuestionsKind(t *testing.T) {
	for _, c := range []struct {
		words, kind string
		yes         bool
		said        string
		ok          bool
	}{
		{"yes", "cap", true, "", true},
		{"No.", "plan", false, "", true},
		{"no, use the other file", "plan", false, "no, use the other file", true},
		{"sure thing", "cap", true, "", true},
		{"yes, go", "plan", true, "", true},
		{"what does it cost?", "cap", false, "", false},
		{"split it in two", "gate", false, "split it in two", true},
	} {
		yes, said, ok := typedAnswer(c.words, c.kind)
		if yes != c.yes || said != c.said || ok != c.ok {
			t.Errorf("%q under %s read as (%v, %q, %v), want (%v, %q, %v)", c.words, c.kind, yes, said, ok, c.yes, c.said, c.ok)
		}
	}
}

func TestLinesAConversationCouldNotTakeAreSaidLaterInOrder(t *testing.T) {
	talk := newFakeTalk()
	talk.refuse = 2
	g, _ := managed(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) { return done("ok"), nil }),
	}, talk, 5*time.Millisecond)
	id := g.add("fix it", chat("plan"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	talk.waitSaid(t, it.Talk, sayLanded)
	want := "plan started\nplan done · ok\n" + sayLanded
	if got := strings.Join(talk.lines(it.Talk), "\n"); got != want {
		t.Fatalf("the manager heard:\n%s\nwant:\n%s", got, want)
	}
}
