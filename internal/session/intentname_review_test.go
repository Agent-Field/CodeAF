package session

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOptionalIntentNameRequiresConfiguredRoute(t *testing.T) {
	for _, one := range []bool{false, true} {
		t.Run(fmt.Sprint("one-model=", one), func(t *testing.T) {
			completer := intentSays(1, "Configured one-model title")
			agent, _ := intentAgent(t, completer, func(c *Config) { c.RolesSource = nil; c.OneModel = one })
			submitMessage(t, agent, Submission{Text: "repair the tokenizer"})
			settleNaming(agent)
			want := 0
			if one {
				want = 1
			}
			if got := completer.asks(); got != want {
				t.Fatalf("title calls=%d, want %d", got, want)
			}
			if !one && agent.Title() != OpeningName("repair the tokenizer", nil) {
				t.Fatalf("lost fallback: %q", agent.Title())
			}
			if one && completer.ask(0).model != "test/model" {
				t.Fatalf("one-model route=%q", completer.ask(0).model)
			}
		})
	}
}

func TestLegacyDeterministicNamingNeverBuysTitle(t *testing.T) {
	for _, intent := range []bool{false, true} {
		t.Run(fmt.Sprint("new-flag=", intent), func(t *testing.T) {
			completer := intentSays(1, "Must not be requested")
			agent, _ := intentAgent(t, completer, func(c *Config) { c.IntentNames = intent; c.DeterministicNames = true })
			words := "repair the tokenizer without buying a name"
			submitMessage(t, agent, Submission{Text: words})
			settleNaming(agent)
			if completer.asks() != 0 {
				t.Fatal("legacy no-model policy dispatched a title request")
			}
			if got := agent.Title(); got != FirstMessageName(words) {
				t.Fatalf("legacy title=%q", got)
			}
		})
	}
}

// The provider, accepted submission, journal, and standing subscription are real
// session paths. The observer delays one generated event between hub delivery
// and standing delivery, reproducing the scheduler gap that used to let a
// captured generated name arrive after a successful manual rename.
func TestManualRenameOrdersAfterDelayedGeneratedTitleEvent(t *testing.T) {
	agent, _ := intentAgent(t, intentSays(1, "Earned title"), nil)
	submitMessage(t, agent, Submission{Text: "repair the tokenizer"})
	settleNaming(agent)
	lane, leave := agent.WatchTitle()
	defer leave()
	_ = drainTitles(lane)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	agent.mu.Lock()
	original := agent.hub
	agent.hub = &eventHub{observe: func(e Event) {
		if e.Kind == EventTitleChanged && e.Text == "Earned title" {
			once.Do(func() { close(entered); <-release })
		}
	}}
	agent.mu.Unlock()
	defer func() { agent.mu.Lock(); agent.hub = original; agent.mu.Unlock() }()
	emitted := make(chan struct{})
	go func() { agent.announceTitle(context.Background()); close(emitted) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("generated event did not reach barrier")
	}
	renamed := make(chan error, 1)
	go func() { renamed <- agent.RenameByPerson("My tokenizer work") }()
	var premature bool
	select {
	case err := <-renamed:
		if err != nil {
			t.Fatal(err)
		}
		premature = true
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-emitted
	if !premature {
		if err := <-renamed; err != nil {
			t.Fatal(err)
		}
	}
	names := drainTitles(lane)
	if premature {
		t.Fatalf("manual rename overtook blocked generated delivery: %q", names)
	}
	if len(names) == 0 || names[len(names)-1] != "My tokenizer work" {
		t.Fatalf("last standing event=%q", names)
	}
}

func TestConcurrentManualNamesKeepJournalMetadataAndEventsAuthoritative(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	agent, path := intentAgent(t, intentSays(1, "Earned title"), func(c *Config) { c.Place = Place{Dir: dir, Workspace: c.Workspace} })
	submitMessage(t, agent, Submission{Text: "repair the tokenizer"})
	settleNaming(agent)
	lane, leave := agent.WatchTitle()
	defer leave()
	_ = drainTitles(lane)
	for round := 0; round < 4; round++ {
		var group sync.WaitGroup
		errs := make(chan error, 16)
		for n := 0; n < 16; n++ {
			group.Add(1)
			go func(n int) { defer group.Done(); errs <- agent.RenameByPerson(fmt.Sprintf("Manual %d/%d", round, n)) }(n)
		}
		group.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		agent.SettleWrites()
		want := agent.Title()
		meta, err := LoadMeta(dir)
		if err != nil || meta.Title != want || !meta.LiteralName {
			t.Fatalf("current=%q metadata=%+v error=%v", want, meta, err)
		}
		names := drainTitles(lane)
		if len(names) == 0 || names[len(names)-1] != want {
			t.Fatalf("current=%q events=%q", want, names)
		}
	}
	want := agent.Title()
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	lines := titleLines(t, path)
	if len(lines) == 0 || !containsLiteralTitleLine(lines[len(lines)-1], want) {
		t.Fatalf("current=%q last journal=%q", want, lines)
	}
}

func containsLiteralTitleLine(line, want string) bool {
	var entry struct {
		Title   string `json:"title"`
		Literal bool   `json:"literalName"`
	}
	return json.Unmarshal([]byte(line), &entry) == nil && entry.Title == want && entry.Literal
}
