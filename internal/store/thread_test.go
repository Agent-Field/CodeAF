package store

import (
	"errors"
	"path/filepath"
	"reflect"
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

func TestImageAttachmentsSurviveMessageCommandProvenanceAndRebuild(t *testing.T) {
	s := openThreadStore(t)
	attachments := []string{"/tmp/one.png", "/tmp/two.webp"}
	options := []QuestionOption{{Label: "keep both"}, {Label: "swap them", Value: "swap"}}
	posted, err := s.PostMessage(Message{SessionID: "media", Role: RoleUser, Body: "inspect these",
		Attachments: attachments, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	messages, err := s.Messages("media", 0, 0)
	if err != nil || len(messages) != 1 || strings.Join(messages[0].Attachments, ",") != strings.Join(attachments, ",") {
		t.Fatalf("message attachments = %+v err=%v", messages, err)
	}
	if len(messages[0].Options) != 2 || messages[0].Options[1].Value != "swap" {
		t.Fatalf("message options = %+v", messages[0].Options)
	}
	command, err := s.RequestCommand(Command{SessionID: "media", Kind: CommandSplice, Instruction: posted.Body, Attachments: posted.Attachments})
	if err != nil {
		t.Fatal(err)
	}
	read, ok, err := s.CommandBySeq(command.Seq)
	if err != nil || !ok || strings.Join(read.Attachments, ",") != strings.Join(attachments, ",") {
		t.Fatalf("command attachments = %+v ok=%t err=%v", read.Attachments, ok, err)
	}
	if err := s.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: "media-task", Brief: "inspect", Stage: 1}}},
		Provenance{Origin: OriginUser, SessionID: "media", Intent: posted.Body, Attachments: attachments}); err != nil {
		t.Fatal(err)
	}
	if err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	node, ok, err := s.Node("media-task")
	if err != nil || !ok || strings.Join(node.Provenance.Attachments, ",") != strings.Join(attachments, ",") {
		t.Fatalf("rebuilt node attachments = %+v ok=%t err=%v", node.Provenance.Attachments, ok, err)
	}
	// One message may carry both selectable options and attachments; the
	// journal replay must keep both.
	rebuilt, err := s.Messages("media", 0, 0)
	if err != nil || len(rebuilt) != 1 {
		t.Fatalf("rebuilt messages = %+v err=%v", rebuilt, err)
	}
	if strings.Join(rebuilt[0].Attachments, ",") != strings.Join(attachments, ",") {
		t.Fatalf("rebuilt message attachments = %+v", rebuilt[0].Attachments)
	}
	if len(rebuilt[0].Options) != 2 || rebuilt[0].Options[0].Label != "keep both" || rebuilt[0].Options[1].Value != "swap" {
		t.Fatalf("rebuilt message options = %+v", rebuilt[0].Options)
	}
}

func TestSeenWatermarkIsJournaledAndSurvivesRebuild(t *testing.T) {
	s := openThreadStore(t)
	if _, found, err := s.LastSeen(); err != nil || found {
		t.Fatalf("empty last seen: found=%t err=%v", found, err)
	}
	attached, err := s.TouchSeen("tui", "session-a", SeenAttached)
	if err != nil {
		t.Fatal(err)
	}
	detached, err := s.TouchSeen("tui", "session-a", SeenDetached)
	if err != nil {
		t.Fatal(err)
	}
	if detached.Seq <= attached.Seq || detached.Time.Before(attached.Time) {
		t.Fatalf("watermarks out of order: attached=%+v detached=%+v", attached, detached)
	}
	last, found, err := s.LastSeen()
	if err != nil || !found || last.Seq != detached.Seq || last.State != SeenDetached ||
		last.Surface != "tui" || last.SessionID != "session-a" {
		t.Fatalf("last seen = %+v found=%t err=%v", last, found, err)
	}
	if err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, found, err := s.LastSeen()
	if err != nil || !found || rebuilt != last {
		t.Fatalf("rebuilt last seen = %+v, want %+v (found=%t err=%v)", rebuilt, last, found, err)
	}
}

func TestBriefMessageRoundTripsAndRebuilds(t *testing.T) {
	s := openThreadStore(t)
	want := &Brief{
		SinceSeq: 1, ThroughSeq: 7, Done: 1, Questions: 1, CostUSD: 1.4,
		Items: []BriefItem{
			{Kind: BriefDone, Body: "The report landed.", Ref: "report"},
			{Kind: BriefQuestion, Body: "The deploy needs a region."},
			{Kind: BriefSpend, Body: "$1.40 spent."},
		},
	}
	posted, err := s.PostMessage(Message{
		SessionID: "arrival", Role: RoleAgent,
		Body: "While you were away: the report landed, and one choice is waiting.", Brief: want,
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := s.Messages("arrival", 0, 0)
	if err != nil || len(read) != 1 || !reflect.DeepEqual(read[0].Brief, want) {
		t.Fatalf("brief read = %+v err=%v", read, err)
	}
	if err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := s.Messages("arrival", 0, 0)
	if err != nil || len(rebuilt) != 1 || rebuilt[0].Seq != posted.Seq ||
		!reflect.DeepEqual(rebuilt[0].Brief, want) {
		t.Fatalf("brief after rebuild = %+v err=%v", rebuilt, err)
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

func TestReflexCommandSurvivesJournalRebuild(t *testing.T) {
	s := openThreadStore(t)
	command, err := s.RequestCommand(Command{
		SessionID: "reflex-session", Kind: CommandSplice, Reflex: true,
		Instruction: "Read VERSION and report it verbatim.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, found, err := s.CommandBySeq(command.Seq)
	if err != nil || !found {
		t.Fatalf("command found=%t err=%v", found, err)
	}
	if !rebuilt.Reflex || rebuilt.Kind != CommandSplice ||
		rebuilt.SessionID != command.SessionID || rebuilt.Instruction != command.Instruction {
		t.Fatalf("rebuilt reflex command = %+v", rebuilt)
	}
	if _, err := s.RequestCommand(Command{
		Kind: CommandSplice, Reflex: true, Target: "existing", Instruction: "wrong shape",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("targeted reflex error = %v, want ErrInvalid", err)
	}
}

// TestHeadInterruptIsAGlobalCommand pins 12.8.10's store half: the turn-cancel
// kind is reachable from any surface through the ordinary funnel, needs no
// target because it targets no node, and still runs the ordinary node-target
// validation on the rare command that supplies one anyway (a caller error, not
// a shape the door ever produces — see internal/head/interrupt.go, which never
// sets Target).
func TestHeadInterruptIsAGlobalCommand(t *testing.T) {
	s := openThreadStore(t)

	if !isGlobalCommand(CommandHeadInterrupt) {
		t.Fatal("CommandHeadInterrupt must be a global command: it targets no node")
	}
	if !validCommandKind(CommandHeadInterrupt) {
		t.Fatal("CommandHeadInterrupt must be a valid command kind")
	}

	command, err := s.RequestCommand(Command{
		SessionID: "s1", Kind: CommandHeadInterrupt, Instruction: "stop the turn in flight",
	})
	if err != nil {
		t.Fatalf("untargeted head interrupt: expected acceptance, got %v", err)
	}
	if command.Status != CommandPending {
		t.Fatalf("new command status = %s", command.Status)
	}
	pending, err := s.PendingCommands(0)
	if err != nil || len(pending) != 1 || pending[0].Seq != command.Seq {
		t.Fatalf("pending queue = %+v, err=%v", pending, err)
	}
	if err := s.ResolveCommand(command.Seq, CommandApplied, "stopped"); err != nil {
		t.Fatalf("resolve command: %v", err)
	}

	// A node target is not required (unlike CommandCancel et al.), but a
	// caller that supplies one anyway still walks ordinary node validation
	// rather than being silently accepted because the kind is global.
	if _, err := s.RequestCommand(Command{
		Kind: CommandHeadInterrupt, Target: "ghost", Instruction: "stop it",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("head interrupt at unknown target: expected ErrNotFound, got %v", err)
	}
	if _, err := s.RequestCommand(Command{
		Kind: CommandHeadInterrupt, Target: RootID, Instruction: "stop it",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("head interrupt at the spine: expected ErrInvalid, got %v", err)
	}
}

// TestOldJournalsReplayToIdenticalCommandsAcrossHeadInterrupt is replay-compat
// for 12.8.10: a journal written before head_interrupt existed replays to the
// byte-identical commands table it always replayed to, and adding a head
// interrupt to that same journal changes nothing about the rows already there
// — mirroring sessions_test.go's TestOldJournalsReplayToIdenticalSessions.
func TestOldJournalsReplayToIdenticalCommandsAcrossHeadInterrupt(t *testing.T) {
	s := openThreadStore(t)

	spliced, err := s.RequestCommand(Command{
		SessionID: "s1", Kind: CommandSplice, Instruction: "benchmark the parser",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveCommand(spliced.Seq, CommandApplied, "spliced 3 nodes"); err != nil {
		t.Fatal(err)
	}

	journalBefore := readEventRows(t, s)
	commandsBefore := readCommandRows(t, s)
	if countEvents(t, s, EventCommandRequested) == 0 {
		t.Fatal("the fixture contains no command_requested events; it proves nothing")
	}

	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if journalAfter := readEventRows(t, s); !reflect.DeepEqual(journalBefore, journalAfter) {
		t.Fatalf("the rebuild rewrote the journal:\n after %v\n want %v", journalAfter, journalBefore)
	}
	if commandsAfter := readCommandRows(t, s); !reflect.DeepEqual(commandsBefore, commandsAfter) {
		t.Fatalf("a pre-head_interrupt journal replayed to %v, want the identical %v",
			commandsAfter, commandsBefore)
	}

	// Adding a head interrupt to that same journal changes nothing about the
	// rows already there: replay only ever adds a row.
	interrupted, err := s.RequestCommand(Command{
		SessionID: "s1", Kind: CommandHeadInterrupt, Instruction: "stop the turn in flight",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild after head interrupt: %v", err)
	}
	mixed := readCommandRows(t, s)
	if len(mixed) != len(commandsBefore)+1 {
		t.Fatalf("a mixed journal replayed to %d commands, want the old %d plus one",
			len(mixed), len(commandsBefore))
	}
	if !reflect.DeepEqual(mixed[:len(commandsBefore)], commandsBefore) {
		t.Fatalf("the pre-existing commands replayed to %v, want the identical %v",
			mixed[:len(commandsBefore)], commandsBefore)
	}
	rebuilt, ok, err := s.CommandBySeq(interrupted.Seq)
	if err != nil || !ok {
		t.Fatalf("head interrupt after rebuild: ok=%v err=%v", ok, err)
	}
	if rebuilt.Kind != CommandHeadInterrupt || rebuilt.Target != "" || rebuilt.SessionID != "s1" {
		t.Fatalf("rebuilt head interrupt = %+v", rebuilt)
	}
}

// readCommandRows renders the commands table as comparable strings, the same
// shape readSessionRows and readEventRows use for replay-compat proofs.
func readCommandRows(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.db.Query(`
		SELECT seq || '|' || ts || '|' || session_id || '|' || kind || '|' || issuer || '|' ||
			reflex || '|' || fresh || '|' || target || '|' || instruction || '|' || attachments || '|' ||
			status || '|' || result || '|' || updated_seq
		FROM commands ORDER BY seq`)
	if err != nil {
		t.Fatalf("read command rows: %v", err)
	}
	defer rows.Close()
	var rendered []string
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatalf("scan command row: %v", err)
		}
		rendered = append(rendered, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read command rows: %v", err)
	}
	return rendered
}
