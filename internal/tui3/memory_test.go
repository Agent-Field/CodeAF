package tui3

import (
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// rememberingAgent is a fakeAgent that also has a brain — the optional
// interface memory.go asserts. It is a separate type rather than three more
// fields on fakeAgent for exactly the reason the interface is separate: a
// session without memory is the ordinary case, and every other test in this
// package has to keep meaning what it means.
type rememberingAgent struct {
	fakeAgent

	kept    []session.MemoryLine
	failing error
	off     bool

	remembered, forgotten, listed []string
}

func (r *rememberingAgent) Remembers() bool { return !r.off }

func (r *rememberingAgent) Remember(text string) (string, error) {
	r.remembered = append(r.remembered, text)
	if r.failing != nil {
		return "", r.failing
	}
	line := session.MemoryLine{ID: "mem_1", Title: "prefers tabs", Text: text}
	r.kept = append(r.kept, line)
	return line.Title, nil
}

func (r *rememberingAgent) Forget(query string) (string, error) {
	r.forgotten = append(r.forgotten, query)
	if r.failing != nil {
		return "", r.failing
	}
	for index, line := range r.kept {
		if strings.Contains(strings.ToLower(line.Text), strings.ToLower(query)) {
			r.kept = append(r.kept[:index], r.kept[index+1:]...)
			return line.Title, nil
		}
	}
	return "", nil
}

func (r *rememberingAgent) Memories(query string) ([]session.MemoryLine, error) {
	r.listed = append(r.listed, query)
	if r.failing != nil {
		return nil, r.failing
	}
	if strings.TrimSpace(query) == "" {
		return r.kept, nil
	}
	var found []session.MemoryLine
	for _, line := range r.kept {
		if strings.Contains(strings.ToLower(line.Text), strings.ToLower(query)) {
			found = append(found, line)
		}
	}
	return found, nil
}

// ── the table ───────────────────────────────────────────────────────────────

func TestTheThreeMemoryCommandsAreOnTheList(t *testing.T) {
	help := helpText("")
	for _, name := range []string{"memories", "remember", "forget"} {
		var found bool
		for _, c := range commands {
			found = found || c.name == name
		}
		if !found {
			t.Fatalf("/%s is not on the command list", name)
		}
		if !strings.Contains(help, "/"+name) {
			t.Fatalf("/%s is not in /help", name)
		}
	}
	if got := canonicalCommand("memory"); got != "memories" {
		t.Fatalf("/memory ran as /%s", got)
	}
	if err := checkCommands(commands); err != nil {
		t.Fatalf("the table stopped being a table: %v", err)
	}
}

// ── the happy paths ─────────────────────────────────────────────────────────

func TestRememberKeepsOneThingAndSaysWhatItKept(t *testing.T) {
	agent := &rememberingAgent{}
	a := newTestApp(agent)

	a.slash("/remember I prefer tabs over spaces in Go")

	if len(agent.remembered) != 1 || agent.remembered[0] != "I prefer tabs over spaces in Go" {
		t.Fatalf("the agent was asked to remember %v", agent.remembered)
	}
	if text := lastNote(t, a); !strings.Contains(text, "remembered") || !strings.Contains(text, "prefers tabs") {
		t.Fatalf("the answer was %q", text)
	}
}

func TestMemoriesListsOnePerLineWithTheIdThatNamesIt(t *testing.T) {
	agent := &rememberingAgent{kept: []session.MemoryLine{
		{ID: "mem_1", Title: "prefers tabs", Text: "prefers tabs over spaces in Go"},
		{ID: "mem_2", Title: "standup time", Text: "standup is at 9:15"},
	}}
	a := newTestApp(agent)

	a.slash("/memories")

	text := lastNote(t, a)
	lines := strings.Split(text, "\n")
	if len(lines) != 2 {
		t.Fatalf("the list is %d lines:\n%s", len(lines), text)
	}
	if !strings.Contains(lines[0], "prefers tabs — prefers tabs over spaces in Go") || !strings.Contains(lines[0], "(mem_1)") {
		t.Fatalf("a row reads %q", lines[0])
	}
}

func TestMemoriesWithAQueryNarrowsTheList(t *testing.T) {
	agent := &rememberingAgent{kept: []session.MemoryLine{
		{ID: "mem_1", Title: "prefers tabs", Text: "prefers tabs over spaces in Go"},
		{ID: "mem_2", Title: "standup time", Text: "standup is at 9:15"},
	}}
	a := newTestApp(agent)

	a.slash("/memories standup")

	if len(agent.listed) != 1 || agent.listed[0] != "standup" {
		t.Fatalf("the agent was asked for %v", agent.listed)
	}
	text := lastNote(t, a)
	if strings.Contains(text, "tabs") || !strings.Contains(text, "standup") {
		t.Fatalf("the narrowed list is:\n%s", text)
	}
}

func TestForgetDropsTheMatchAndNamesIt(t *testing.T) {
	agent := &rememberingAgent{kept: []session.MemoryLine{
		{ID: "mem_2", Title: "standup time", Text: "standup is at 9:15"},
	}}
	a := newTestApp(agent)

	a.slash("/forget standup")

	if text := lastNote(t, a); !strings.Contains(text, "forgot") || !strings.Contains(text, "standup time") {
		t.Fatalf("the answer was %q", text)
	}
	if len(agent.kept) != 0 {
		t.Fatalf("the memory is still kept: %v", agent.kept)
	}
}

// ── the empty and the missing ───────────────────────────────────────────────

// A COMMAND TYPED ON PURPOSE ALWAYS ANSWERS. An empty store, an empty search
// and a no-match forget are three different facts and each gets its own line —
// silence would read as a command that broke.
func TestTheEmptyStatesEachSayWhichEmptinessItIs(t *testing.T) {
	agent := &rememberingAgent{}
	a := newTestApp(agent)

	a.slash("/memories")
	if text := lastNote(t, a); text != "nothing is remembered yet" {
		t.Fatalf("an empty store answered %q", text)
	}

	agent.kept = []session.MemoryLine{{ID: "mem_1", Title: "prefers tabs", Text: "prefers tabs over spaces in Go"}}
	a.slash("/memories pineapples")
	if text := lastNote(t, a); !strings.Contains(text, "nothing remembered matches pineapples") {
		t.Fatalf("an empty search answered %q", text)
	}

	a.slash("/forget pineapples")
	if text := lastNote(t, a); !strings.Contains(text, "nothing matched pineapples") {
		t.Fatalf("a no-match forget answered %q", text)
	}
}

func TestTheTwoArgumentCommandsAskForTheirArgument(t *testing.T) {
	a := newTestApp(&rememberingAgent{})

	a.slash("/remember")
	if text := lastNote(t, a); !strings.Contains(text, "what should be kept") {
		t.Fatalf("/remember with nothing answered %q", text)
	}
	a.slash("/forget   ")
	if text := lastNote(t, a); !strings.Contains(text, "what should be dropped") {
		t.Fatalf("/forget with nothing answered %q", text)
	}
}

// A SESSION WITH NO BRAIN SAYS SO AND NAMES THE ROW. "no" without "and here is
// how to change that" is the half of an answer that sends somebody to the
// manual.
func TestWithoutABrainAllThreeSayMemoryIsOff(t *testing.T) {
	// Two shapes of "no brain", and they must read the same: an agent that has
	// never heard of memory (every other test's fake, and every surface built
	// before this feature), and a real session whose door opened no store.
	for _, agent := range []Agent{&fakeAgent{model: "m"}, &rememberingAgent{off: true}} {
		a := newTestApp(agent)
		for _, line := range []string{"/memories", "/remember something", "/forget something"} {
			a.slash(line)
			if text := lastNote(t, a); !strings.Contains(text, "memory is off") || !strings.Contains(text, "/settings") {
				t.Fatalf("%s answered %q", line, text)
			}
		}
	}
}

func TestAFailureIsReportedAndNotSwallowed(t *testing.T) {
	agent := &rememberingAgent{failing: errors.New("the brain is locked")}
	a := newTestApp(agent)

	a.slash("/remember I prefer tabs")
	if text := lastNote(t, a); !strings.Contains(text, "the brain is locked") {
		t.Fatalf("a failed write answered %q", text)
	}
}
