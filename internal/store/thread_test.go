package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func openThreadStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPostAndTailMessages(t *testing.T) {
	s := openThreadStore(t)

	first, err := s.PostMessage(Message{SessionID: "s1", Role: RoleUser, Body: "review the PR"})
	if err != nil {
		t.Fatalf("post user message: %v", err)
	}
	second, err := s.PostMessage(Message{SessionID: "s1", Role: RoleAgent, Body: "on it — compiling your brief"})
	if err != nil {
		t.Fatalf("post agent message: %v", err)
	}
	if _, err := s.PostMessage(Message{SessionID: "s2", Role: RoleUser, Body: "other session"}); err != nil {
		t.Fatalf("post other-session message: %v", err)
	}

	all, err := s.Messages("s1", 0, 0)
	if err != nil {
		t.Fatalf("list session messages: %v", err)
	}
	if len(all) != 2 || all[0].Seq != first.Seq || all[1].Seq != second.Seq {
		t.Fatalf("session tail wrong: %+v", all)
	}
	if all[0].Role != RoleUser || all[1].Body != "on it — compiling your brief" {
		t.Fatalf("message content wrong: %+v", all)
	}

	tail, err := s.Messages("s1", first.Seq, 0)
	if err != nil {
		t.Fatalf("tail after seq: %v", err)
	}
	if len(tail) != 1 || tail[0].Seq != second.Seq {
		t.Fatalf("afterSeq tail wrong: %+v", tail)
	}

	everything, err := s.Messages("", 0, 0)
	if err != nil {
		t.Fatalf("list all messages: %v", err)
	}
	if len(everything) != 3 {
		t.Fatalf("expected 3 messages across sessions, got %d", len(everything))
	}
}

func TestPostMessageValidation(t *testing.T) {
	s := openThreadStore(t)

	if _, err := s.PostMessage(Message{Role: Role("robot"), Body: "hi"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad role: expected ErrInvalid, got %v", err)
	}
	if _, err := s.PostMessage(Message{Role: RoleUser, Body: "   "}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty body: expected ErrInvalid, got %v", err)
	}
	if _, err := s.PostMessage(Message{Role: RoleUser, Body: strings.Repeat("x", MaxMessageBytes+1)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversize body: expected ErrInvalid, got %v", err)
	}
	if _, err := s.PostMessage(Message{Role: RoleSystem, Body: "fold landed", NodeID: "ghost"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown node anchor: expected ErrNotFound, got %v", err)
	}
}

func TestCommandLifecycle(t *testing.T) {
	s := openThreadStore(t)

	command, err := s.RequestCommand(Command{
		SessionID:   "s1",
		Kind:        CommandSplice,
		Instruction: "benchmark the parser against main",
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}
	if command.Status != CommandPending {
		t.Fatalf("new command status = %s", command.Status)
	}

	pending, err := s.PendingCommands(0)
	if err != nil {
		t.Fatalf("pending commands: %v", err)
	}
	if len(pending) != 1 || pending[0].Seq != command.Seq {
		t.Fatalf("pending queue wrong: %+v", pending)
	}

	if err := s.ResolveCommand(command.Seq, CommandApplied, "spliced 5 nodes under root"); err != nil {
		t.Fatalf("resolve command: %v", err)
	}
	settled, ok, err := s.CommandBySeq(command.Seq)
	if err != nil || !ok {
		t.Fatalf("command by seq: ok=%v err=%v", ok, err)
	}
	if settled.Status != CommandApplied || settled.Result != "spliced 5 nodes under root" {
		t.Fatalf("settled command wrong: %+v", settled)
	}
	if settled.UpdatedSeq <= command.Seq {
		t.Fatalf("resolution did not advance updated_seq: %+v", settled)
	}

	pending, err = s.PendingCommands(0)
	if err != nil {
		t.Fatalf("pending after resolve: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("queue should be empty, got %+v", pending)
	}

	if err := s.ResolveCommand(command.Seq, CommandRejected, "again"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("double resolve: expected ErrInvalid, got %v", err)
	}
}

func TestCommandValidation(t *testing.T) {
	s := openThreadStore(t)

	if _, err := s.RequestCommand(Command{Kind: CommandKind("explode"), Instruction: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad kind: expected ErrInvalid, got %v", err)
	}
	if _, err := s.RequestCommand(Command{Kind: CommandSplice, Instruction: " "}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty instruction: expected ErrInvalid, got %v", err)
	}
	if _, err := s.RequestCommand(Command{Kind: CommandCancel, Instruction: "stop it"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cancel without target: expected ErrInvalid, got %v", err)
	}
	if _, err := s.RequestCommand(Command{Kind: CommandCancel, Target: "ghost", Instruction: "stop it"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancel unknown target: expected ErrNotFound, got %v", err)
	}
	if err := s.ResolveCommand(999, CommandApplied, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("resolve missing: expected ErrNotFound, got %v", err)
	}
	if err := s.ResolveCommand(999, CommandPending, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("resolve to pending: expected ErrInvalid, got %v", err)
	}
}

func TestRebuildReplaysThread(t *testing.T) {
	s := openThreadStore(t)

	posted, err := s.PostMessage(Message{SessionID: "s1", Role: RoleUser, Body: "cancel the benchmark part"})
	if err != nil {
		t.Fatalf("post message: %v", err)
	}
	command, err := s.RequestCommand(Command{SessionID: "s1", Kind: CommandSplice, Instruction: "do the thing"})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}
	if err := s.ResolveCommand(command.Seq, CommandRejected, "nothing to do"); err != nil {
		t.Fatalf("resolve command: %v", err)
	}

	if err := s.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: "spent", Brief: "work", Stage: 1}}},
		Provenance{Origin: OriginUser, SessionID: "s1", Intent: "spend"}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	if err := s.RecordUsage(NodeUsage{NodeID: "spent", PromptTokens: 500, CompletionTokens: 40, Cost: 0.02}); err != nil {
		t.Fatalf("record usage: %v", err)
	}

	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	total, err := s.Usage()
	if err != nil {
		t.Fatalf("usage after rebuild: %v", err)
	}
	if total.Nodes != 1 || total.PromptTokens != 500 || total.Cost < 0.019 {
		t.Fatalf("usage lost in rebuild: %+v", total)
	}

	messages, err := s.Messages("s1", 0, 0)
	if err != nil {
		t.Fatalf("messages after rebuild: %v", err)
	}
	if len(messages) != 1 || messages[0].Seq != posted.Seq || messages[0].Body != "cancel the benchmark part" {
		t.Fatalf("rebuilt messages wrong: %+v", messages)
	}

	rebuilt, ok, err := s.CommandBySeq(command.Seq)
	if err != nil || !ok {
		t.Fatalf("command after rebuild: ok=%v err=%v", ok, err)
	}
	if rebuilt.Status != CommandRejected || rebuilt.Result != "nothing to do" {
		t.Fatalf("rebuilt command wrong: %+v", rebuilt)
	}
}
