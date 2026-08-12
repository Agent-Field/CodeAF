package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func openAdmissionStore(t *testing.T) *Store {
	t.Helper()
	graph, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	return graph
}

// The guard used to live in the head's commissioning tool, which meant it
// protected exactly one caller. Everything else that can commission work —
// `aforge do` running headless, a question answered into a continuation, a
// surface with its own composer — journaled twins freely and the person paid
// for both. This is the funnel refusing one without any conversation involved.
func TestTheFunnelRefusesATwinOfWorkAlreadyWaiting(t *testing.T) {
	graph := openAdmissionStore(t)
	first, err := graph.RequestCommand(Command{
		SessionID: "headless", Kind: CommandSplice,
		Instruction: "audit last quarter's billing code and write up what you find",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = graph.RequestCommand(Command{
		SessionID: "headless", Kind: CommandSplice,
		Instruction: "audit the billing code from last quarter and write up what you find",
	})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("the funnel admitted a twin: err=%v", err)
	}
	pending, err := graph.PendingCommands(10)
	if err != nil || len(pending) != 1 || pending[0].Seq != first.Seq {
		t.Fatalf("pending = %+v err=%v, want only the first ask", pending, err)
	}
}

// The override is the person's own, and it is the only one: they said in so
// many words that this runs beside the first.
func TestDeliberateWorkIsAdmittedBesideItsTwin(t *testing.T) {
	graph := openAdmissionStore(t)
	if _, err := graph.RequestCommand(Command{
		SessionID: "headless", Kind: CommandSplice,
		Instruction: "audit last quarter's billing code and write up what you find",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RequestCommand(Command{
		SessionID: "headless", Kind: CommandSplice, Deliberate: true,
		Instruction: "audit last quarter's billing code and write up what you find",
	}); err != nil {
		t.Fatalf("a deliberate second job was refused: %v", err)
	}
	if pending, _ := graph.PendingCommands(10); len(pending) != 2 {
		t.Fatalf("pending = %+v, want both", pending)
	}
}

// A false twin silently swallows a genuinely new job, which is worse than the
// duplicate the guard exists to prevent — a duplicate at least shows up on the
// board. So the bar is high, the guard is per-session, it only looks at work
// still WAITING, and a different number is always a different ask.
func TestTheTwinGuardDoesNotSwallowDifferentAsks(t *testing.T) {
	tests := []struct {
		name    string
		session string
		first   string
		second  string
	}{
		{name: "different subject", session: "room",
			first:  "research recent dev tool startups from top venture firms",
			second: "report tomorrow's weather in Toronto from an authoritative source"},
		{name: "different number", session: "room",
			first:  "work issue 12 on my repo and open a pull request",
			second: "work issue 41 on my repo and open a pull request"},
		{name: "different room", session: "",
			first:  "audit last quarter's billing code and write up what you find",
			second: "audit last quarter's billing code and write up what you find"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph := openAdmissionStore(t)
			if _, err := graph.RequestCommand(Command{
				SessionID: test.session, Kind: CommandSplice, Instruction: test.first,
			}); err != nil {
				t.Fatal(err)
			}
			session := test.session
			if session == "" {
				session = "another-room"
			}
			if _, err := graph.RequestCommand(Command{
				SessionID: session, Kind: CommandSplice, Instruction: test.second,
			}); err != nil {
				t.Fatalf("a different ask was refused as a twin: %v", err)
			}
			if pending, _ := graph.PendingCommands(10); len(pending) != 2 {
				t.Fatalf("pending = %+v, want both", pending)
			}
		})
	}
}

// Only new work is guarded. A redirect, a cancel and a charter edit are aimed
// at something that already exists, and two of them in a row is an ordinary
// thing for a person to do.
func TestOnlySplicesAreGuardedAgainstTwins(t *testing.T) {
	graph := openAdmissionStore(t)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Title: "A job", Brief: "do the job", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "room", Intent: "do the job"}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if _, err := graph.RequestCommand(Command{
			SessionID: "room", Kind: CommandRedirect, Target: "job",
			Instruction: "use the audited ledger rather than the raw export",
		}); err != nil {
			t.Fatalf("redirect %d was refused as a twin: %v", index, err)
		}
	}
	if pending, _ := graph.PendingCommands(10); len(pending) != 2 {
		t.Fatalf("pending = %+v, want both redirects", pending)
	}
}
