package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// ordersAgent is a conversation with the ambient side wired to a REAL store in
// a temp directory.
//
// The card's own tests run on a fake because what they are about is the card
// (standing_test.go). These are about what the DISK ends up holding — which
// exception was written, which reason a stop recorded, which order the shelves
// come back in — and a fake asked those questions would be answering its own.
func ordersAgent(t *testing.T) (*Agent, *standing.Store) {
	t.Helper()
	store, err := standing.Open(filepath.Join(t.TempDir(), "standing"))
	if err != nil {
		t.Fatalf("cannot open a store: %v", err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Standing = &Standing{Store: store}
	})
	return agent, store
}

// anOrder stands one item up directly, the way a ratified card would.
func anOrder(t *testing.T, store *standing.Store, words, workspace string, altitude standing.Altitude, session string) standing.Item {
	t.Helper()
	made, err := store.Create(standing.Item{
		Words:     words,
		Workspace: workspace,
		Origin:    standing.Origin{SessionID: session},
		When:      standing.When{Kind: standing.WhenAt, Words: words, At: time.Now().Add(time.Hour)},
		Does:      standing.Action{Kind: standing.ActionSay, Say: "time to leave"},
		Rails:     standing.Rails{PerRunUSD: 0.05, MaxPerDay: 1},
		Altitude:  altitude,
	})
	if err != nil {
		t.Fatalf("cannot stand %q up: %v", words, err)
	}
	return made
}

// said is what the assertions below read: the answer as a person would say it
// back, in the order it came.
func said(items []standing.Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Words)
	}
	return out
}

func TestWhatStandsHereIsThisChatThenThisProjectThenTheMachine(t *testing.T) {
	agent, store := ordersAgent(t)
	workspace, session := agent.standingPlace()
	house, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory to file a machine-wide order under: %v", err)
	}

	anOrder(t, store, "everywhere", house, standing.AltitudeMachine, session)
	anOrder(t, store, "in this project", workspace, standing.AltitudeProject, session)
	anOrder(t, store, "in another project", filepath.Join(workspace, "..", "elsewhere"), standing.AltitudeProject, session)
	anOrder(t, store, "in another chat", workspace, standing.AltitudeConversation, "another")
	anOrder(t, store, "just this chat", workspace, standing.AltitudeConversation, session)

	// The two the person kept out of here, from either direction: a project
	// order excepted from this conversation, and a machine order excepted from
	// this project.
	kept := anOrder(t, store, "not in this chat", workspace, standing.AltitudeProject, session)
	kept.Exceptions = []standing.Exception{{SessionID: session, At: time.Now()}}
	if err := store.Save(kept); err != nil {
		t.Fatalf("except: %v", err)
	}
	notHere := anOrder(t, store, "not in this project", house, standing.AltitudeMachine, session)
	notHere.Exceptions = []standing.Exception{{Workspace: workspace, At: time.Now()}}
	if err := store.Save(notHere); err != nil {
		t.Fatalf("except: %v", err)
	}

	stand, excepted := agent.StandingHere()
	want := []string{"just this chat", "in this project", "everywhere"}
	if got := said(stand); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("what stands here is %q, wanted %q", got, want)
	}
	if got := said(excepted); len(got) != 2 {
		t.Fatalf("the not-here lines are %q, wanted the two kept out of here", got)
	}
	for _, words := range []string{"not in this chat", "not in this project"} {
		if !strings.Contains(strings.Join(said(excepted), "|"), words) {
			t.Fatalf("%q is missing from the not-here lines %q", words, said(excepted))
		}
	}
}

// A paused order must not vanish from the page on the pause keypress, or the
// resume half of that one key becomes unreachable. The resolver itself still
// answers active only — the seams that spend money never see a paused item.
func TestAPausedOrderStillStandsOverItsPlaceSoResumeStaysReachable(t *testing.T) {
	agent, store := ordersAgent(t)
	workspace, session := agent.standingPlace()

	paused := anOrder(t, store, "paused but present", workspace, standing.AltitudeProject, session)
	paused.Status = standing.StatusPaused
	if err := store.Save(paused); err != nil {
		t.Fatalf("pause: %v", err)
	}
	retired := anOrder(t, store, "gone for good", workspace, standing.AltitudeProject, session)
	retired.Status = standing.StatusRetired
	if err := store.Save(retired); err != nil {
		t.Fatalf("retire: %v", err)
	}

	stand, _ := agent.StandingHere()
	if got := strings.Join(said(stand), "|"); !strings.Contains(got, "paused but present") {
		t.Fatalf("the paused order is missing from what stands here: %q", got)
	}
	if got := strings.Join(said(stand), "|"); strings.Contains(got, "gone for good") {
		t.Fatalf("a retired order is over and must not be listed: %q", got)
	}
	if status, err := agent.StandingPause(paused.ID); err != nil || status != standing.StatusActive {
		t.Fatalf("resume from the page came to (%q, %v), wanted active", status, err)
	}
}

func TestNothingStandsAnywhereWhenTheAmbientSideIsOff(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	stand, excepted := agent.StandingHere()
	if stand != nil || excepted != nil {
		t.Fatalf("a conversation with no ambient side answered %v and %v", stand, excepted)
	}
	if err := agent.StandingExcept("whatever"); err == nil {
		t.Fatal("excepting reported success with nothing to write to")
	}
	if err := agent.StandingStandDown("whatever"); err == nil {
		t.Fatal("standing down reported success with nothing to write to")
	}
	if _, err := agent.StandingPause("whatever"); err == nil {
		t.Fatal("pausing reported success with nothing to write to")
	}
}

func TestAnExceptionIsMadeAtYourAltitudeRelativeToTheOrder(t *testing.T) {
	agent, store := ordersAgent(t)
	workspace, session := agent.standingPlace()

	for _, tried := range []struct {
		name      string
		altitude  standing.Altitude
		workspace string
		session   string
	}{
		{name: "a machine-wide order is kept out of this project", altitude: standing.AltitudeMachine, workspace: workspace},
		{name: "a project order is kept out of this conversation", altitude: standing.AltitudeProject, session: session},
	} {
		t.Run(tried.name, func(t *testing.T) {
			made := anOrder(t, store, tried.name, workspace, tried.altitude, session)
			if err := agent.StandingExcept(made.ID); err != nil {
				t.Fatalf("except: %v", err)
			}
			back, err := store.Get(made.ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if len(back.Exceptions) != 1 {
				t.Fatalf("the order carries %d exceptions, wanted one", len(back.Exceptions))
			}
			except := back.Exceptions[0]
			if except.Workspace != tried.workspace || except.SessionID != tried.session {
				t.Fatalf("the exception names %q and %q, wanted %q and %q",
					except.Workspace, except.SessionID, tried.workspace, tried.session)
			}
			if back.AppliesTo(workspace, session) {
				t.Fatal("the order still reaches here")
			}

			// PRESSED TWICE IS ONE FACT. The second gesture is somebody making
			// sure, and it must not grow the document.
			if err := agent.StandingExcept(made.ID); err != nil {
				t.Fatalf("excepting twice: %v", err)
			}
			back, err = store.Get(made.ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if len(back.Exceptions) != 1 {
				t.Fatalf("excepting twice wrote %d exceptions", len(back.Exceptions))
			}
		})
	}

	t.Run("a conversation order has nowhere narrower to go", func(t *testing.T) {
		made := anOrder(t, store, "just this chat", workspace, standing.AltitudeConversation, session)
		err := agent.StandingExcept(made.ID)
		if err == nil {
			t.Fatal("a conversation order was excepted from its own conversation")
		}
		if !strings.Contains(err.Error(), "stand it down") {
			t.Fatalf("the refusal does not say what to do instead: %v", err)
		}
		back, err := store.Get(made.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if len(back.Exceptions) != 0 || back.Status != standing.StatusActive {
			t.Fatalf("the refused gesture changed the order: %+v", back)
		}
	})
}

func TestStandingOneDownRecordsThatYouStoppedIt(t *testing.T) {
	agent, store := ordersAgent(t)
	workspace, session := agent.standingPlace()
	made := anOrder(t, store, "keep main green", workspace, standing.AltitudeProject, session)

	if err := agent.StandingStandDown(made.ID); err != nil {
		t.Fatalf("stand down: %v", err)
	}
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if back.Status != standing.StatusRetired || back.RetiredWhy != standingStoppedWhy {
		t.Fatalf("the stopped order reads %q / %q, wanted retired and %q",
			back.Status, back.RetiredWhy, standingStoppedWhy)
	}
	// AND IT IS GONE FROM HERE. A retired order governs nothing.
	if stand, _ := agent.StandingHere(); len(stand) != 0 {
		t.Fatalf("a stopped order still stands here: %q", said(stand))
	}
}

func TestPauseIsAToggleAndAStoppedOrderIsNotResumedByIt(t *testing.T) {
	agent, store := ordersAgent(t)
	workspace, session := agent.standingPlace()
	made := anOrder(t, store, "every Monday, draft the update", workspace, standing.AltitudeProject, session)

	status, err := agent.StandingPause(made.ID)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if status != standing.StatusPaused {
		t.Fatalf("pausing answered %q", status)
	}
	// THE ROW MUST SURVIVE THE KEYPRESS: a paused order still lists among what
	// stands here — otherwise the resume half of the same key is unreachable.
	if stand, _ := agent.StandingHere(); len(stand) != 1 {
		t.Fatalf("a paused order vanished from the page: %q", said(stand))
	}

	status, err = agent.StandingPause(made.ID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if status != standing.StatusActive {
		t.Fatalf("the second press answered %q, wanted it running again", status)
	}
	if stand, _ := agent.StandingHere(); len(stand) != 1 {
		t.Fatalf("a resumed order does not stand here: %q", said(stand))
	}

	if err := agent.StandingStandDown(made.ID); err != nil {
		t.Fatalf("stand down: %v", err)
	}
	if _, err := agent.StandingPause(made.ID); err == nil {
		t.Fatal("a stopped order was resumed by the pause key")
	}
}

func TestTheProposedOrderCarriesItsAltitudeTitleAndGrant(t *testing.T) {
	agent, _ := ordersAgent(t)
	parsed := standArguments{
		Words:    "keep main green",
		Altitude: "machine",
		Title:    "main stays green",
		Grant:    "open a pull request, never merge one",
	}
	parsed.When.Kind = "every"
	parsed.When.Every = "20m"
	parsed.Does.Kind = "say"
	parsed.Does.Say = "main is red"

	item, problem := agent.standingItem(parsed, time.Now())
	if problem != "" {
		t.Fatalf("the call was refused: %s", problem)
	}
	if item.Altitude != standing.AltitudeMachine {
		t.Fatalf("the order reaches %q, wanted the machine", item.Altitude)
	}
	if item.Title() != "main stays green" || item.Grant != "open a pull request, never merge one" {
		t.Fatalf("the title and grant did not ride: %+v", item)
	}
	// THE WORDS ARE NEVER REWRITTEN, and a brief with no compiled prompt reads
	// as them.
	if item.Prompt() != "keep main green" {
		t.Fatalf("the working instruction is %q, wanted the person's own sentence", item.Prompt())
	}

	// SAID IN A PROJECT, IT IS ABOUT THAT PROJECT. Nothing about scope in the
	// call means the altitude everything already standing has.
	parsed.Altitude = ""
	item, problem = agent.standingItem(parsed, time.Now())
	if problem != "" {
		t.Fatalf("the call was refused: %s", problem)
	}
	if item.Altitude != standing.AltitudeProject {
		t.Fatalf("an order said in a project reaches %q", item.Altitude)
	}

	// And a word that is not one of the three is refused before anybody is
	// asked, by the admission law itself.
	parsed.Altitude = "galaxy"
	item, problem = agent.standingItem(parsed, time.Now())
	if problem != "" {
		t.Fatalf("the call was refused too early: %s", problem)
	}
	if err := item.Validate(); err == nil || !strings.Contains(err.Error(), "altitude") {
		t.Fatalf("an unknown altitude was admitted: %v", err)
	}
}
