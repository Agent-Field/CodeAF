package tui3

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// /crew, FROM THE SIDES A PERSON MEETS IT: the panel, the four shortcuts, a
// form that is none of them, and the crew line a routed task says.

// The bare form is the panel: the three seats (all auto on a profile nobody
// touched), the allowed models and the daily cap, over the conversation rather
// than written into it (crewpanel_test.go drives it).
func TestCrewIsThePanel(t *testing.T) {
	a, _ := sheetApp(t)
	a.slash("/crew")
	if !a.crewUI.open {
		t.Fatal("/crew opened no panel")
	}
	text := strings.Join(plainOverlay(a), "\n")
	for _, want := range []string{"worker", "planner", "checker", "auto", "models", "‹ all ›", "cap", "none"} {
		if !strings.Contains(text, want) {
			t.Errorf("the panel does not say %q:\n%s", want, text)
		}
	}
	for _, retired := range []string{"frugal", "balanced", "mastermind", "preset"} {
		if strings.Contains(text, retired) {
			t.Errorf("the panel still says the retired %q:\n%s", retired, text)
		}
	}
}

// A PIN IS WRITTEN, SHOWN WITH THE VOCABULARY'S PIN GLYPH, AND UNDONE.
func TestCrewPinAndUnpin(t *testing.T) {
	a, dir := sheetApp(t)
	// A pin that names a provider needs that provider connected.
	t.Setenv(config.APIKeyEnv, "sk-or-v1-test")
	a.slash("/crew pin checker moonshotai/kimi-k3@openrouter")
	pin, ok := config.CrewPinAt(dir, crewroute.Checker)
	if !ok || pin.Model != "moonshotai/kimi-k3" || pin.Provider != "openrouter" {
		t.Fatalf("the checker pin reads %+v (%v)", pin, ok)
	}
	if note := lastNote(t, a); !strings.Contains(note, a.icon(tokens.GPinned)) || !strings.Contains(note, "kimi-k3@openrouter") {
		t.Fatalf("the confirmation does not show the pin: %q", note)
	}
	if !a.crewUI.open {
		t.Fatal("the pin did not open the panel on what it changed")
	}
	if panel := strings.Join(plainOverlay(a), "\n"); !strings.Contains(panel, a.icon(tokens.GPinned)+" kimi-k3 @openrouter") {
		t.Fatalf("the panel does not mark the pinned seat:\n%s", panel)
	}
	a.slash("/crew unpin checker")
	if _, ok := config.CrewPinAt(dir, crewroute.Checker); ok {
		t.Fatal("unpin left the checker pinned")
	}
	// A seat that is not one of the three is refused with the form.
	a.slash("/crew pin judge vendor/x")
	if note := lastNote(t, a); !strings.Contains(note, "usage: /crew pin") {
		t.Fatalf("a pin on an unknown seat said %q", note)
	}
}

// A PIN OUTSIDE THE ALLOWED MODELS IS REFUSED, not written.
func TestCrewRefusesAPinOutsideTheAllowedModels(t *testing.T) {
	a, dir := sheetApp(t)
	a.slash("/crew models z-ai/glm-5.3-flash")
	if got := config.CrewAllowedAt(dir).String(); !strings.Contains(got, "glm-5.3-flash") {
		t.Fatalf("the allowed rule reads %q", got)
	}
	a.slash("/crew pin worker vendor/elsewhere")
	if _, ok := config.CrewPinAt(dir, crewroute.Worker); ok {
		t.Fatal("a pin outside the allowed models was written")
	}
	if note := lastNote(t, a); !strings.Contains(note, "could not pin") {
		t.Fatalf("the refusal said %q", note)
	}
}

// THE CAP IS WRITTEN AND SAID with today's spend beside it; `off` clears it.
func TestCrewCap(t *testing.T) {
	a, dir := sheetApp(t)
	a.slash("/crew cap 5")
	if got := config.CrewCapAt(dir); got != 5 {
		t.Fatalf("the cap reads %v", got)
	}
	if note := lastNote(t, a); !strings.Contains(note, "$5.00") || !strings.Contains(note, "spent today") {
		t.Fatalf("the cap confirmation said %q", note)
	}
}

// A FORM THAT IS NONE OF THE FOUR changes nothing and says them — and the
// retired preset words are exactly such forms now.
func TestCrewRefusesARetiredPresetWord(t *testing.T) {
	a, dir := sheetApp(t)
	a.slash("/crew frugal")
	if note := lastNote(t, a); !strings.Contains(note, "not a crew form") || !strings.Contains(note, "/crew pin") {
		t.Fatalf("a retired preset word said %q", note)
	}
	if pins := config.CrewPinsAt(dir); len(pins) != 0 {
		t.Fatalf("a retired word pinned %v", pins)
	}
}

// THE STATUS SEGMENT names the crew in the fewest cells: auto, and how many
// seats are pinned when any is.
func TestCrewSegment(t *testing.T) {
	a, dir := sheetApp(t)
	if got := a.crewSegment(); got != "crew auto" {
		t.Fatalf("an untouched profile's segment is %q", got)
	}
	if err := config.SetCrewPin(dir, crewroute.Planner, "vendor/planner"); err != nil {
		t.Fatal(err)
	}
	if got := a.crewSegment(); got != "crew auto · 1 pinned" {
		t.Fatalf("a profile with one pin reads %q", got)
	}
}

// A ROUTED TASK SAYS ITS CREW when it starts, with the estimate, and when it
// lands, with the actual beside the estimate and the door to asking again.
func TestARoutedTaskSaysItsCrewTwice(t *testing.T) {
	a, _ := sheetApp(t)
	crew := &crewroute.Decision{Class: crewroute.Bugfix, EstUSD: 0.02, Crew: []crewroute.Pick{
		{Seat: crewroute.Worker, Model: "z-ai/glm-5.3-flash", Provider: "openrouter"},
		{Seat: crewroute.Planner, Model: "z-ai/glm-5.3-flash", Provider: "openrouter"},
		{Seat: crewroute.Checker, Model: "moonshotai/kimi-k3", Provider: "openrouter", Pinned: true},
	}}
	a.sayTaskCrew(session.TaskNotice{ID: 7, State: session.TaskRunning, Crew: crew})
	a.sayTaskCrew(session.TaskNotice{ID: 7, State: session.TaskRunning, Crew: crew})
	started := lastNote(t, a)
	if !strings.Contains(started, "task 7 crew · bugfix · worker glm-5.3-flash (openrouter)") || !strings.Contains(started, "est $0.020") {
		t.Fatalf("the start line reads %q", started)
	}
	if !strings.Contains(started, a.icon(tokens.GPinned)+" kimi-k3") {
		t.Fatalf("the pinned checker is not marked: %q", started)
	}
	count := 0
	for _, e := range a.entries {
		if e.kind == entryNote && strings.Contains(e.text, "task 7 crew") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the start line was said %d times", count)
	}
	a.sayTaskCrew(session.TaskNotice{ID: 7, State: session.TaskDone, Crew: crew, CostUSD: 0.018})
	if landed := lastNote(t, a); !strings.Contains(landed, "$0.018 (est $0.020)") || !strings.Contains(landed, "/redo stronger") {
		t.Fatalf("the landing line reads %q", landed)
	}
}

// crewEffortAgent is a task door that records the effort word it was handed.
type crewEffortAgent struct {
	*fakeAgent
	effort, brief string
	redone        bool
}

func (c *crewEffortAgent) StartTask(ctx context.Context, brief string, solo bool) (uint64, string, string, error) {
	c.brief = brief
	return 1, brief, "", nil
}

func (c *crewEffortAgent) StartTaskEffort(ctx context.Context, brief string, solo bool, effort string) (uint64, string, string, error) {
	c.brief, c.effort = brief, effort
	return 1, brief, "", nil
}

func (c *crewEffortAgent) RedoStronger(ctx context.Context, row uint64) (uint64, string, error) {
	c.redone = true
	return 2, "again", nil
}

// `/task --best` and `/task --cheap` hand the word to the session and keep it
// out of the brief; a task without either uses the ordinary door.
func TestTaskEffortWordsReachTheSession(t *testing.T) {
	for _, c := range []struct{ typed, effort, brief string }{
		{"/task --best fix the parser", "best", "fix the parser"},
		{"/task --cheap rename the flag", "cheap", "rename the flag"},
		{"/task fix the parser --best", "", "fix the parser --best"},
	} {
		a, _ := sheetApp(t)
		door := &crewEffortAgent{fakeAgent: &fakeAgent{model: "openai/gpt-4.1-mini"}}
		a.agent = door
		runCmd(a.runTaskCommand(strings.TrimPrefix(c.typed, "/task ")))
		if door.effort != c.effort || door.brief != c.brief {
			t.Errorf("%q reached the session as effort %q brief %q", c.typed, door.effort, door.brief)
		}
	}
}

// `/redo stronger` is the one form of /redo, and it reaches the session.
func TestRedoStrongerReachesTheSession(t *testing.T) {
	a, _ := sheetApp(t)
	door := &crewEffortAgent{fakeAgent: &fakeAgent{model: "openai/gpt-4.1-mini"}}
	a.agent = door
	runCmd(a.runRedo("stronger"))
	if !door.redone {
		t.Fatal("/redo stronger never reached the session")
	}
	door.redone = false
	runCmd(a.runRedo("harder"))
	if door.redone {
		t.Fatal("/redo with another word reached the session")
	}
	if note := lastNote(t, a); !strings.Contains(note, "usage: /redo stronger") {
		t.Fatalf("/redo harder said %q", note)
	}
}

// A TASK THAT STOPPED ON A CAUSE A PERSON CAN FIX LEADS WITH THE FIX: the one
// action, first, no "/redo stronger" (a stronger crew meets the same wall),
// and no $0.000 for a run that spent nothing.
func TestAStoppedTaskLeadsWithItsOneAction(t *testing.T) {
	a, _ := sheetApp(t)
	crew := &crewroute.Decision{Class: crewroute.Bugfix, EstUSD: 0.02, Stopped: "add credit on openrouter to continue", Crew: []crewroute.Pick{
		{Seat: crewroute.Worker, Model: "z-ai/glm-5.3-flash", Provider: "openrouter"},
		{Seat: crewroute.Planner, Model: "z-ai/glm-5.3-flash", Provider: "openrouter"},
		{Seat: crewroute.Checker, Model: "moonshotai/kimi-k3", Provider: "openrouter"},
	}}
	a.sayTaskCrew(session.TaskNotice{ID: 3, State: session.TaskFailed, Crew: crew})
	line := lastNote(t, a)
	if !strings.HasPrefix(line, "task 3 crew · failed — add credit on openrouter to continue · ") {
		t.Errorf("the stopped line reads %q", line)
	}
	if strings.Contains(line, "/redo stronger") || strings.Contains(line, "$0.000") {
		t.Errorf("the stopped line offers a redo or a free success: %q", line)
	}
}

// A NEW CONVERSATION'S TASK 1 SAYS ITS CREW, though the last conversation's
// task 1 had landed: the lines are the conversation's, and go with it.
func TestANewConversationsTaskSaysItsCrew(t *testing.T) {
	a, _ := sheetApp(t)
	crew := &crewroute.Decision{Class: crewroute.Bugfix, EstUSD: 0.02, Crew: []crewroute.Pick{
		{Seat: crewroute.Worker, Model: "z-ai/glm-5.3-flash", Provider: "openrouter"},
		{Seat: crewroute.Planner, Model: "z-ai/glm-5.3-flash"}, {Seat: crewroute.Checker, Model: "moonshotai/kimi-k3"},
	}}
	a.sayTaskCrew(session.TaskNotice{ID: 1, State: session.TaskDone, Crew: crew, CostUSD: 0.01})
	a.dropTasks()
	before := len(a.entries)
	a.sayTaskCrew(session.TaskNotice{ID: 1, State: session.TaskRunning, Crew: crew})
	if len(a.entries) == before || !strings.Contains(lastNote(t, a), "task 1 crew · bugfix") {
		t.Fatalf("the new conversation's task 1 said no crew line")
	}
}
