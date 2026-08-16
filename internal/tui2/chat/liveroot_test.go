package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/golden"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE DEFECT: a working root read as a hung job.
//
// The job had six parts and every one of them showed done. Its root was running
// — on its second attempt after a deadline restart — and had been posting
// node-anchored progress the whole time: "benchmark done", "writeup nearly
// there", ten rows and more. What the person saw was a room with nothing alive
// in it and a card that said "1 part running", about a job with no part running
// at all: the number counts the ROOT, and the root doing delivery is not one of
// its own children.
//
// Two things were missing and neither was the data. The card had no word for a
// root that is finishing up, so it borrowed a word about parts. And the one
// progress line the room draws carried no clock, so a sentence written four
// seconds ago and one written twenty minutes ago were the same motionless row.

// deliveringBoard is the incident's shape: parts all settled, root still going,
// on its second attempt, talking to itself as it delivers.
func deliveringBoard() *boardBackend {
	backend := board()
	backend.nodes = append(backend.nodes,
		store.Node{ID: "job-9", Title: "svm-parity", Status: store.Running, Attempt: 2,
			CreatedSeq: 90, UpdatedSeq: 99,
			Brief: "Benchmark the SVM implementations and write up what holds."},
		store.Node{ID: "job-9/sweep", Parent: "job-9", Title: "Sweep", Status: store.Done,
			CreatedSeq: 91, UpdatedSeq: 92, Summary: "41 configurations measured."},
		store.Node{ID: "job-9/rank", Parent: "job-9", Title: "Rank", Status: store.Done,
			CreatedSeq: 92, UpdatedSeq: 93, Summary: "ranked by margin."},
	)
	backend.node["job-9"] = []store.Message{
		{Seq: 300, SessionID: testSession, Role: store.RoleAgent, NodeID: "job-9",
			Body: "benchmark done", Time: fixedNow().Add(-9 * time.Minute)},
		{Seq: 301, SessionID: testSession, Role: store.RoleAgent, NodeID: "job-9",
			Body: "writeup nearly there", Time: fixedNow().Add(-4 * time.Minute)},
	}
	return backend
}

// A root doing the work is never called a part.
func TestARootFinishingUpIsNotDescribedAsAPart(t *testing.T) {
	for _, probe := range []struct {
		name string
		root store.Node
		sum  roll
		want string
	}{
		{"root delivering, parts all settled",
			store.Node{ID: "job", Status: store.Running}, roll{parts: 6, active: 1}, "finishing up"},
		{"root with no parts at all",
			store.Node{ID: "job", Status: store.Running}, roll{}, "working"},
		{"root and two parts moving",
			store.Node{ID: "job", Status: store.Running}, roll{parts: 3, active: 3}, "2 parts running"},
		{"one part moving under a settled root",
			store.Node{ID: "job", Status: store.Done}, roll{parts: 3, active: 1}, "1 part running"},
		{"nothing has started",
			store.Node{ID: "job", Status: store.Pending}, roll{parts: 2, active: 3}, "2 parts running"},
	} {
		if got := liveWorkPhrase(probe.root, probe.sum); got != probe.want {
			t.Fatalf("%s: %q, want %q", probe.name, got, probe.want)
		}
	}
}

// The card, end to end: the room the person entered says what is true of the
// root, and says which attempt it is on.
func TestTheCardForADeliveringRootSaysSoAndNamesTheAttempt(t *testing.T) {
	backend := deliveringBoard()
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)

	var card string
	for _, row := range app.railModel.Rows() {
		if row.Name == "svm-parity" {
			card = row.Status
		}
	}
	if card == "" {
		t.Fatal("the delivering job is not on the rail")
	}
	if strings.Contains(card, "part") {
		t.Fatalf("the root is still being counted as one of its own parts: %q", card)
	}
	if !strings.Contains(card, "finishing up") {
		t.Fatalf("a root delivering after its parts finished says %q", card)
	}
	if !strings.Contains(card, "2nd attempt") {
		t.Fatalf("a restarted job's card does not say so: %q", card)
	}
}

// The room's live area: the root's own latest line, with how long ago it was
// said and which attempt said it.
func TestARunningRootsRoomShowsItsLatestLineAndHowLongAgo(t *testing.T) {
	backend := deliveringBoard()
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	openRecord(t, app, "job-9", "svm-parity")

	frame := app.Frame(100, 30)
	if !strings.Contains(frame, "writeup nearly there") {
		t.Fatalf("the running root's own latest line is nowhere in its room:\n%s", frame)
	}
	if strings.Contains(frame, "benchmark done") {
		t.Fatalf("the room drew an older progress line as well as the latest:\n%s", frame)
	}
	if !strings.Contains(frame, "4m") {
		t.Fatalf("the latest line carries no age, so a working root reads as a hung one:\n%s", frame)
	}
	if !strings.Contains(frame, "2nd attempt") {
		t.Fatalf("the room does not say the job is on its second attempt:\n%s", frame)
	}
}

// Progress rows count. A worker reporting through the structured column rather
// than in a body was invisible to the room's one live line, which is how a room
// went silent for exactly the reason its workers were being loudest.
func TestAStructuredProgressRowIsAsMuchALiveLineAsASentence(t *testing.T) {
	said := recordSaid(store.Message{
		NodeID: "job-9", Role: store.RoleAgent,
		Progress: &store.MessageProgress{Phase: "writing up", Done: 3, Total: 4,
			Latest: "figures placed"},
	})
	if !strings.Contains(said, "writing up") || !strings.Contains(said, "figures placed") {
		t.Fatalf("a progress row said %q", said)
	}
	if !strings.Contains(said, "3/4") {
		t.Fatalf("a progress row lost its counter: %q", said)
	}
	// A row with nothing on it at all is still nothing, and must not become a
	// blank heartbeat.
	if got := recordSaid(store.Message{NodeID: "job-9"}); got != "" {
		t.Fatalf("an empty row said %q", got)
	}
}

// The aside is absent when it would say nothing. A line said seconds ago on a
// first attempt spends none of the reader's attention.
func TestTheLiveLineSaysNothingItDoesNotHaveToSay(t *testing.T) {
	now := fixedNow()
	quiet := recordStatusAside(
		store.Message{Time: now}, store.Node{Status: store.Running, Attempt: 1}, now)
	if quiet != "now" {
		t.Fatalf("a fresh line on a first attempt drew %q", quiet)
	}
	// A settled job's attempt count is history's business, not the live line's.
	settledRoot := recordStatusAside(
		store.Message{Time: now.Add(-3 * time.Minute)},
		store.Node{Status: store.Done, Attempt: 3}, now)
	if settledRoot != "3m" {
		t.Fatalf("a settled job's live line drew %q", settledRoot)
	}
	// A row with no stamp has no age, and an invented one would be worse than
	// none (8.2.20).
	unstamped := recordStatusAside(store.Message{}, store.Node{Status: store.Running, Attempt: 2}, now)
	if unstamped != "2nd attempt" {
		t.Fatalf("an unstamped row drew %q", unstamped)
	}
}

// The stored frame. A room whose root is delivering, at the ordinary terminal
// and at the narrow one where the aside has to survive a measure the sentence
// cannot fit in.
func deliveringRoomView(profile tokens.Profile) golden.View {
	return func(width, height int, theme golden.Theme) []string {
		app := goldenApp(deliveringBoard(), profile, theme)
		drivePoll(app)
		app.openTaskRoom(rail.Row{ID: "job-9", Name: "svm-parity",
			Life: app.source.rowLife("job-9")}, "job-9")
		if cmd := app.readNodeCmd("job-9", 0); cmd != nil {
			if trail, ok := cmd().(nodeMessagesMsg); ok {
				app.applyNodeMessages(trail)
			}
		}
		return strings.Split(app.Frame(width, height), "\n")
	}
}

func TestGoldenDeliveringRoom(t *testing.T) {
	golden.RunSizes(t, "room-delivering-root", deliveringRoomView(tokens.NoColor),
		goldenSizes, goldenThemes)
	golden.Snap(t, "room-delivering-root-plain", deliveringRoomView(tokens.NoColor),
		100, 24, golden.Theme{Mode: golden.Dark})
	golden.Snap(t, "room-delivering-root-narrow", deliveringRoomView(tokens.NoColor),
		40, 16, golden.Theme{Mode: golden.Dark})
}
