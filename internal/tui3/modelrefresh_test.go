package tui3

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// The refresh key, asked of a fake door: it returns rows or an error, never the
// network. The door's contract is that [Options.Models] reads what a landed
// fetch brought back, so the fake swaps the list the picker's closure reads,
// exactly as cmd/aforge's shelf does.
type fakeRefresh struct {
	list  []Model
	next  []Model
	err   error
	calls int
}

func (f *fakeRefresh) models() []Model { return f.list }

func (f *fakeRefresh) fetch(context.Context) ([]Model, time.Time, error) {
	f.calls++
	if f.err != nil {
		return nil, time.Time{}, f.err
	}
	f.list = f.next
	return f.next, time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC), nil
}

// refreshApp is a picker app on a sixty-cell frame whose door offers the key.
func refreshApp(t *testing.T, door *fakeRefresh) *app {
	t.Helper()
	a := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, pickerCatalog)
	door.list = pickerCatalog
	a.models = door.models
	a.refreshModels = door.fetch
	return a
}

// pressRefresh presses the key and hands back the command it left, unrun, so
// a test can look at the picker while the fetch is out.
func pressRefresh(a *app) tea.Cmd { return a.key(key(refreshModelsKey)) }

// boxLine is the frame's filter-box row: the line the placeholder is drawn on.
func boxLine(t *testing.T, a *app) string {
	t.Helper()
	for _, line := range strings.Split(plain(frame(a)), "\n") {
		if strings.Contains(line, "filter ·") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "›"))
		}
	}
	t.Fatalf("no filter box on the frame:\n%s", plain(frame(a)))
	return ""
}

// RULE 2: the key is named on the picker's own lines. On sixty cells the
// placeholder is the ranked tail — whole keys, from the right, `enter · esc`
// given up first — and a filter that matches nothing offers the newest list.
func TestTheRefreshKeyIsNamedInThePlaceholderAndTheEmptyList(t *testing.T) {
	a := refreshApp(t, &fakeRefresh{})
	typeLine(t, a, "/model")

	want := "filter · ↑↓ · → lanes · ctrl+t effort · " + refreshModelsHint
	if got := boxLine(t, a); got != want {
		t.Fatalf("on sixty cells the box reads %q, want %q", got, want)
	}
	if !strings.HasPrefix(pickerHint, want) {
		t.Fatalf("the sixty-cell line %q is not the head of %q", want, pickerHint)
	}

	typeInto(t, a, "zzz")
	if got := plain(frame(a)); !strings.Contains(got, noModelMatchesFetch) {
		t.Fatalf("an empty list has to offer the newest list:\n%s", got)
	}
}

// RULE 3: a door with no refresh has no key. Nothing names it, the placeholder
// is whole on sixty cells, and pressing it does nothing at all.
func TestADoorWithNoRefreshHasNoKey(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, pickerCatalog)
	typeLine(t, a, "/model")

	if got := boxLine(t, a); got != rowAll(pickerHintFieldsBare) {
		t.Fatalf("with no refresh the box reads %q, want the whole bare line", got)
	}
	if cmd := pressRefresh(a); cmd != nil || a.pick.fetching || !a.pick.open {
		t.Fatalf("the key did something on a door with no refresh (cmd %v, fetching %v)", cmd != nil, a.pick.fetching)
	}
	typeInto(t, a, "zzz")
	got := plain(frame(a))
	if !strings.Contains(got, noModelMatches) || strings.Contains(got, refreshModelsKey) {
		t.Fatalf("a door with no refresh named the key:\n%s", got)
	}
}

// RULE 4: while the fetch is out the picker says so once, at the head of the
// list, keeps answering every key, and a second press starts nothing.
func TestWhileTheListIsFetchingThePickerKeepsAnswering(t *testing.T) {
	door := &fakeRefresh{next: pickerCatalog}
	a := refreshApp(t, door)
	typeLine(t, a, "/model")

	fetch := pressRefresh(a)
	if fetch == nil {
		t.Fatal("the key left no fetch")
	}
	got := plain(frame(a))
	if !strings.Contains(got, modelsFetching) {
		t.Fatalf("nothing says the list is being fetched:\n%s", got)
	}
	if strings.Contains(boxLine(t, a), refreshModelsKey) {
		t.Fatalf("the key is offered while a fetch is already out: %q", boxLine(t, a))
	}
	if again := pressRefresh(a); again != nil {
		t.Fatal("a second press while one is out started another fetch")
	}
	typeInto(t, a, "gpt")
	if ids := pickerIDs(a); len(ids) != 3 || !a.pick.open {
		t.Fatalf("the filter stopped answering while the fetch was out: %v", ids)
	}
	drive(t, a, runCmd(fetch)...)
	if door.calls != 1 || a.modelsFetching || strings.Contains(plain(frame(a)), modelsFetching) {
		t.Fatalf("after the answer: %d calls, still fetching %v", door.calls, a.modelsFetching)
	}
}

// RULE 5: a landed list is re-ranked under the filter as typed, the cursor goes
// back to the model in use, and the note counts what is new and names up to
// three — never what vanished.
func TestALandedListKeepsTheFilterAndSaysWhatIsNew(t *testing.T) {
	next := []Model{
		{ID: "openai/gpt-6"}, {ID: "openai/gpt-6-mini"}, {ID: "openai/gpt-6-nano"},
		{ID: "openai/gpt-6-pro"},
		{ID: "openai/gpt-4.1-mini", ContextLength: 1_000_000},
		{ID: "gpt-5-classic", ContextLength: 400_000},
		{ID: "moonshotai/kimi-k3"},
	}
	a := refreshApp(t, &fakeRefresh{next: next})
	typeLine(t, a, "/model")
	typeInto(t, a, "gpt")
	drive(t, a, runCmd(pressRefresh(a))...)

	if got := a.pick.filter.String(); got != "gpt" {
		t.Fatalf("the filter came back as %q", got)
	}
	if ids := pickerIDs(a); len(ids) != 6 {
		t.Fatalf("the landed list ranked to %v, want every gpt row", ids)
	}
	if chosen, _ := a.pick.choice(); chosen.ID != "openai/gpt-4.1-mini" {
		t.Fatalf("the cursor landed on %q, want the model in use", chosen.ID)
	}
	// anthropic/claude-gpt-echo vanished, and a refresh never says so.
	want := "models · 7 · 4 new · openai/gpt-6, openai/gpt-6-mini, openai/gpt-6-nano"
	if got := plain(lastNote(t, a)); got != want {
		t.Fatalf("the note reads %q, want %q", got, want)
	}
}

// RULE 5, the quiet case: nothing new is said in words, not as a zero.
func TestALandedListWithNothingNewSaysSo(t *testing.T) {
	a := refreshApp(t, &fakeRefresh{next: pickerCatalog})
	typeLine(t, a, "/model")
	drive(t, a, runCmd(pressRefresh(a))...)
	if got, want := plain(lastNote(t, a)), "models · 4 · "+modelsNothingNew; got != want {
		t.Fatalf("the note reads %q, want %q", got, want)
	}
}

// RULE 6: a failure keeps every row, says what happened in one line, and
// offers the key again.
func TestAFailedFetchKeepsTheListAndSaysWhy(t *testing.T) {
	a := refreshApp(t, &fakeRefresh{err: errors.New("dial tcp: lookup openrouter.ai:\nno such host")})
	typeLine(t, a, "/model")
	before := strings.Join(pickerIDs(a), ",")
	drive(t, a, runCmd(pressRefresh(a))...)

	if after := strings.Join(pickerIDs(a), ","); after != before {
		t.Fatalf("a failed fetch changed the list: %s → %s", before, after)
	}
	if !strings.Contains(boxLine(t, a), refreshModelsHint) {
		t.Fatalf("the key is not offered again: %q", boxLine(t, a))
	}
	want := ModelsFetchFailed + " · dial tcp: lookup openrouter.ai: no such host"
	if got := plain(lastNote(t, a)); got != want {
		t.Fatalf("the note reads %q, want %q — one line, as the door said it", got, want)
	}
}
