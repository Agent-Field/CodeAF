package store

import (
	"errors"
	"path/filepath"
	"testing"
)

// spliceIssuerTree gives the authority check something to be wrong about: two
// unrelated jobs, each with a stage and a leaf, so "inside my subtree" and
// "inside somebody else's" are both reachable answers.
func spliceIssuerTree(t *testing.T, graph *Store) {
	t.Helper()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "mine", Brief: "the task that is asking", Stage: 3},
		{ID: "mine-stage", Parent: "mine", Brief: "a stage of it", Stage: 2},
		{ID: "mine-leaf", Parent: "mine-stage", Brief: "the actual work", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "issuers", Intent: "one task"}); err != nil {
		t.Fatalf("splice own tree: %v", err)
	}
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "theirs", Brief: "somebody else's job", Stage: 2},
		{ID: "theirs-leaf", Parent: "theirs", Brief: "their work", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "issuers", Intent: "another task"}); err != nil {
		t.Fatalf("splice sibling tree: %v", err)
	}
}

// TestLegacyCommandsCarryTheTrustedIssuer is the behaviour-identity contract:
// every path that existed before the axis names nobody, is read as the person,
// and is checked exactly as much as it was before — which is not at all.
func TestLegacyCommandsCarryTheTrustedIssuer(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "legacy.db"))
	spliceIssuerTree(t, graph)

	splice, err := graph.RequestCommand(Command{
		SessionID: "issuers", Kind: CommandSplice, Instruction: "do a new thing",
	})
	if err != nil {
		t.Fatalf("untargeted splice: %v", err)
	}
	cancel, err := graph.RequestCommand(Command{
		SessionID: "issuers", Kind: CommandCancel, Target: "theirs-leaf", Instruction: "stop that",
	})
	if err != nil {
		t.Fatalf("targeted cancel: %v", err)
	}
	for _, command := range []Command{splice, cancel} {
		if command.Issuer != "" {
			t.Fatalf("legacy command %d journaled issuer %q, want the empty legacy value", command.Seq, command.Issuer)
		}
		if command.Authority() != IssuerUser {
			t.Fatalf("legacy command %d reads as %q, want %q", command.Seq, command.Authority(), IssuerUser)
		}
		stored, found, err := graph.CommandBySeq(command.Seq)
		if err != nil || !found {
			t.Fatalf("read back %d: %v found=%t", command.Seq, err, found)
		}
		if stored.Issuer != "" || stored.Authority() != IssuerUser {
			t.Fatalf("stored command %d = %q, want the empty legacy value", command.Seq, stored.Issuer)
		}
	}
}

// TestCommandIssuerRoundTripsThroughTheJournal proves the axis is durable
// rather than decorative: it survives the write, the read, and a full replay
// from the events that recorded it.
func TestCommandIssuerRoundTripsThroughTheJournal(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "roundtrip.db"))
	spliceIssuerTree(t, graph)

	written := map[int64]CommandIssuer{}
	for _, issuer := range []CommandIssuer{IssuerUser, IssuerMain, IssuerReconciler, TaskIssuer("mine")} {
		command, err := graph.RequestCommand(Command{
			SessionID: "issuers", Kind: CommandPause, Issuer: issuer,
			Target: "mine-leaf", Instruction: "hold it",
		})
		if err != nil {
			t.Fatalf("request as %q: %v", issuer, err)
		}
		if command.Issuer != issuer || command.Authority() != issuer {
			t.Fatalf("request as %q returned %q", issuer, command.Issuer)
		}
		written[command.Seq] = issuer
	}
	// A padded issuer is the same issuer: it is normalized once, at the funnel,
	// so no reader downstream has to know that it might not be.
	padded, err := graph.RequestCommand(Command{
		SessionID: "issuers", Kind: CommandPause, Issuer: "  main  ",
		Target: "mine-leaf", Instruction: "hold it again",
	})
	if err != nil {
		t.Fatalf("request with padding: %v", err)
	}
	written[padded.Seq] = IssuerMain

	assert := func(stage string) {
		t.Helper()
		for seq, issuer := range written {
			stored, found, err := graph.CommandBySeq(seq)
			if err != nil || !found {
				t.Fatalf("%s: read %d: %v found=%t", stage, seq, err, found)
			}
			if stored.Issuer != issuer {
				t.Fatalf("%s: command %d issuer = %q, want %q", stage, seq, stored.Issuer, issuer)
			}
		}
	}
	assert("after write")
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	assert("after rebuild")
}

// TestTaskIssuedCommandsStayInsideTheirSubtree is the whole authority rule, and
// every way of being outside it: another task, the spine, and no node at all.
func TestTaskIssuedCommandsStayInsideTheirSubtree(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "subtree.db"))
	spliceIssuerTree(t, graph)

	for _, target := range []string{"mine", "mine-stage", "mine-leaf"} {
		if _, err := graph.RequestCommand(Command{
			SessionID: "issuers", Kind: CommandPause, Issuer: TaskIssuer("mine"),
			Target: target, Instruction: "hold my own work",
		}); err != nil {
			t.Fatalf("task command onto %q was refused: %v", target, err)
		}
	}

	refusals := []struct {
		name    string
		command Command
	}{
		{"another task's root", Command{Kind: CommandPause, Issuer: TaskIssuer("mine"), Target: "theirs", Instruction: "hold theirs"}},
		{"another task's leaf", Command{Kind: CommandCancel, Issuer: TaskIssuer("mine"), Target: "theirs-leaf", Instruction: "stop theirs"}},
		{"a targeted splice outside", Command{Kind: CommandSplice, Issuer: TaskIssuer("mine"), Target: "theirs", Instruction: "add work there"}},
		{"an untargeted splice", Command{Kind: CommandSplice, Issuer: TaskIssuer("mine"), Instruction: "add work anywhere"}},
		{"a global command", Command{Kind: CommandStandingWatchEnable, Issuer: TaskIssuer("mine"), Instruction: "watch for me"}},
		{"a root the graph never had", Command{Kind: CommandPause, Issuer: TaskIssuer("ghost"), Target: "mine-leaf", Instruction: "hold it"}},
		{"an unknown issuer", Command{Kind: CommandPause, Issuer: "janitor", Target: "mine-leaf", Instruction: "hold it"}},
		{"a task naming no root", Command{Kind: CommandPause, Issuer: "task:", Target: "mine-leaf", Instruction: "hold it"}},
		{"a task naming only blanks", Command{Kind: CommandPause, Issuer: "task:   ", Target: "mine-leaf", Instruction: "hold it"}},
	}
	for _, refusal := range refusals {
		refusal.command.SessionID = "issuers"
		if _, err := graph.RequestCommand(refusal.command); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: err = %v, want ErrInvalid", refusal.name, err)
		}
	}

	// A refused command is a command that never happened: the journal holds the
	// three that were allowed and nothing else.
	commands, err := graph.PendingCommands(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 3 {
		t.Fatalf("journaled %d commands, want the 3 authorized ones: %+v", len(commands), commands)
	}
}

// TestSubtreeAuthorizationReadsTheGraph pins the walk itself, away from the
// funnel: a node is inside its own subtree, inside every ancestor's, and inside
// nothing else — including a root that does not exist.
func TestSubtreeAuthorizationReadsTheGraph(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "walk.db"))
	spliceIssuerTree(t, graph)

	cases := []struct {
		root, id string
		want     bool
	}{
		{"mine", "mine", true},
		{"mine", "mine-stage", true},
		{"mine", "mine-leaf", true},
		{RootID, "mine-leaf", true},
		{"mine", "theirs-leaf", false},
		{"mine-stage", "mine", false},
		{"mine", RootID, false},
		{"ghost", "mine-leaf", false},
		{"mine", "ghost", false},
	}
	for _, want := range cases {
		inside, err := withinSubtree(graph.db, want.root, want.id)
		if err != nil {
			t.Fatalf("within %q under %q: %v", want.id, want.root, err)
		}
		if inside != want.want {
			t.Fatalf("within %q under %q = %t, want %t", want.id, want.root, inside, want.want)
		}
	}
}
