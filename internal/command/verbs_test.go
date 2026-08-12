package command

import (
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func openVerbStore(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "verbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	return graph
}

// A verb drawn on a page and the same verb said out loud have to leave the same
// record. These are the exact ids two UI lanes resolve, and each one journals
// the kind the head's own tools journal for the same act.
func TestInvokingAnItemVerbJournalsTheCommandTheHeadWouldHave(t *testing.T) {
	graph := openVerbStore(t)
	charter, err := graph.DraftCharter("digest", "room", 0, store.CharterSpec{
		Invariant: "Every morning, the overnight digest goes out.",
		Watch: store.CharterWatch{
			Kind: store.WatchCron, Cadence: "every morning", Schedule: "0 9 * * *",
		},
		Sentinel: "Is a delivery due?", Action: "Send the digest.",
		Rails: store.CharterSpecRails{
			EstimatedCostUSD: 0.05, MaxPerDay: 1,
			MaxPerDayJustification: "one scheduled delivery", Expiry: "never",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	commander := New(Options{Store: graph, SessionID: "room"})

	for _, want := range []struct {
		id, target, words string
		kind              store.CommandKind
		instruction       string
	}{
		{id: "charter.pause", target: charter.ID, kind: store.CommandCharterPause, instruction: "charter_pause"},
		{id: "charter.probation", target: charter.ID, kind: store.CommandCharterProbation, instruction: "charter_probation"},
		{id: "charter.retire", target: charter.ID, kind: store.CommandCharterRetire, instruction: "charter_retire"},
		{id: "craft.run", target: "release-notes", kind: store.CommandCraftRun, instruction: "run release-notes"},
		{id: "craft.run", target: "release-notes", words: "for the 0.4 tag",
			kind: store.CommandCraftRun, instruction: "for the 0.4 tag"},
		{id: "craft.retire", target: "release-notes", kind: store.CommandCraftRetire,
			instruction: "you asked me to stop using this"},
	} {
		command, err := commander.InvokeVerb(want.id, want.target, want.words)
		if err != nil {
			t.Fatalf("%s: %v", want.id, err)
		}
		if command.Kind != want.kind || command.Target != want.target ||
			command.Instruction != want.instruction || command.SessionID != "room" {
			t.Fatalf("%s journaled %+v", want.id, command)
		}
	}
}

// The auto-restart toggle is one verb going both ways, and which way it goes is
// read off the words exactly the way the head's own service tool spells it.
func TestTheAutoRestartVerbGoesBothWays(t *testing.T) {
	graph := openVerbStore(t)
	service, err := graph.PromoteService(store.Service{
		ID: "svc", Name: "api", Command: "run api", Dir: t.TempDir(),
		LogPath: t.TempDir() + "/service.log",
		Health:  store.ServiceHealth{Kind: store.ServiceHealthPort, Value: "5100"},
		PID:     4242, StartedAt: time.Now().Add(-time.Hour),
		Provenance: store.ServiceProvenance{OriginJobID: 4242, LeafNodeID: store.RootID},
	})
	if err != nil {
		t.Fatal(err)
	}
	commander := New(Options{Store: graph, SessionID: "room"})

	on, err := commander.InvokeVerb("service.autorestart", service.ID, "")
	if err != nil || on.Instruction != "auto-restart" {
		t.Fatalf("turning it on journaled %+v err=%v", on, err)
	}
	off, err := commander.InvokeVerb("service.autorestart", service.ID, "turn it off")
	if err != nil || off.Instruction != "disable-auto-restart" {
		t.Fatalf("turning it off journaled %+v err=%v", off, err)
	}
	stop, err := commander.InvokeVerb("service.stop", service.ID, "")
	if err != nil || stop.Kind != store.CommandServiceStop {
		t.Fatalf("stopping journaled %+v err=%v", stop, err)
	}
}

// Forgetting a belief is not graph work, so it does not become a command: it
// goes through the same retraction the belt's forget tool and the notebook page
// already share, and the belief goes quiet at once.
func TestForgettingABeliefActsAtOnceRatherThanQueueing(t *testing.T) {
	graph := openVerbStore(t)
	fact, err := graph.RecordFact(store.RootID, "user", store.FactPlain, "prefers tables over prose")
	if err != nil {
		t.Fatal(err)
	}
	commander := New(Options{Store: graph, SessionID: "room"})

	command, err := commander.InvokeVerb("belief.forget", strconv.FormatInt(fact.Seq, 10), "")
	if err != nil {
		t.Fatal(err)
	}
	if command.Seq != 0 {
		t.Fatalf("forgetting queued a command: %+v", command)
	}
	after, found, err := graph.FactBySeq(fact.Seq)
	if err != nil || !found || after.Status != store.FactQuarantined {
		t.Fatalf("the belief is %+v", after)
	}
	if _, err := commander.InvokeVerb("belief.forget", "not-a-number", ""); err == nil {
		t.Fatal("forgetting accepted something that was not a belief number")
	}
}

// The three verbs that carry an argument are said, not clicked. Firing one from
// here would mean this package inventing the words the head is supposed to read.
func TestSteeringVerbsRefuseToFireAndSeedInstead(t *testing.T) {
	commander := New(Options{Store: openVerbStore(t), SessionID: "room"})
	for _, id := range []string{"charter.cadence", "belief.edit", "craft.revert"} {
		if _, err := commander.InvokeVerb(id, "digest", "every Tuesday instead"); !errors.Is(err, ErrSteerVerb) {
			t.Fatalf("%s fired instead of steering: %v", id, err)
		}
	}
}

func TestInvokingSomethingThatIsNotAVerbSaysSo(t *testing.T) {
	commander := New(Options{Store: openVerbStore(t), SessionID: "room"})
	if _, err := commander.InvokeVerb("craft.explode", "release-notes", ""); !errors.Is(err, ErrUnknownVerb) {
		t.Fatalf("an invented verb answered %v", err)
	}
	if _, err := commander.InvokeVerb("slash.help", "", ""); err == nil {
		t.Fatal("a verb with nothing to act on was accepted")
	}
	if _, err := commander.InvokeVerb("craft.run", "  ", ""); err == nil {
		t.Fatal("a run with no workflow named was accepted")
	}
	blind := New(Options{})
	if _, err := blind.InvokeVerb("craft.run", "release-notes", ""); err == nil {
		t.Fatal("a window with no journal claimed to have run something")
	}
}
