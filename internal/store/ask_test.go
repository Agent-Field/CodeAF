package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// The typed contract, journaled and read back: the ask alone in the field every
// consumer reads as the person's words, the conversation in its own, and the
// two composed only where a compiler asks for both.
func TestACommandCarriesItsAskAndItsContextApart(t *testing.T) {
	s := openThreadStore(t)
	requested, err := s.RequestCommand(Command{
		SessionID:   "design",
		Kind:        CommandSplice,
		Instruction: "go and build the exporter",
		Context:     "them: the export keeps the column order from the source",
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}

	read, found, err := s.CommandBySeq(requested.Seq)
	if err != nil || !found {
		t.Fatalf("read command back: found=%t err=%v", found, err)
	}
	if read.Instruction != "go and build the exporter" {
		t.Fatalf("the ask came back as %q", read.Instruction)
	}
	if read.Context != "them: the export keeps the column order from the source" {
		t.Fatalf("the context came back as %q", read.Context)
	}
	brief := read.Brief()
	if !strings.HasPrefix(brief, "go and build the exporter") ||
		!strings.Contains(brief, ForkedContextPrefix) ||
		!strings.Contains(brief, "column order from the source") {
		t.Fatalf("the composed brief lost a half:\n%s", brief)
	}

	// A command that inherited no conversation is byte-identical to what it
	// always was, brief and all: the split costs the overwhelming majority of
	// work exactly nothing.
	plain, err := s.RequestCommand(Command{
		SessionID: "design", Kind: CommandSplice, Instruction: "  ship it  ", Deliberate: true,
	})
	if err != nil {
		t.Fatalf("request plain command: %v", err)
	}
	if plain.Brief() != "  ship it  " {
		t.Fatalf("a command with no context was tidied: %q", plain.Brief())
	}
}

// The journal is not rewritable, so the prose fence has to stay readable
// forever. What it must NOT be is a wire format anything new depends on: a
// composer that still glues the two halves together gets them split at the
// journaling door, and the row it writes is a two-field row like any other.
func TestAFencedInstructionSplitsOnTheWayInAndOnTheWayOut(t *testing.T) {
	s := openThreadStore(t)
	fenced := "research jepa models\n\n" + ForkedContextPrefix +
		"\nthem: launch a multi level dag on agentfield using qwen max"

	requested, err := s.RequestCommand(Command{
		SessionID: "jepa", Kind: CommandSplice, Instruction: fenced,
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}
	if requested.Instruction != "research jepa models" {
		t.Fatalf("the fence was not split at the door: %q", requested.Instruction)
	}
	if !strings.Contains(requested.Context, "multi level dag") {
		t.Fatalf("the fenced half did not become the context: %q", requested.Context)
	}

	// And the column itself holds the ask, so a reader that never calls
	// separated — a query, a later migration, a person with sqlite3 — sees the
	// same two facts the API does.
	var stored, storedContext string
	if err := s.db.QueryRow(`SELECT instruction, context FROM commands WHERE seq = ?`,
		requested.Seq).Scan(&stored, &storedContext); err != nil {
		t.Fatalf("read the row: %v", err)
	}
	if stored != "research jepa models" || !strings.Contains(storedContext, "multi level dag") {
		t.Fatalf("the row still carries the fence: instruction=%q context=%q", stored, storedContext)
	}
}

// The rows already on disk, written before the column existed: they carry both
// halves in one string and no typed context at all, and they must read out as
// though they had always been two. History does not get to regress into a
// second case every consumer has to know about.
func TestARowWrittenBeforeTheSplitReadsAsTwoFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pre-split.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	fenced := "research jepa models\n\n" + ForkedContextPrefix +
		"\nthem: launch a multi level dag on agentfield using qwen max"
	requested, err := s.RequestCommand(Command{
		SessionID: "jepa", Kind: CommandSplice, Instruction: "placeholder for the old row",
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}
	// Put the row back the way the old build wrote it: everything in the one
	// column, nothing in the new one.
	if _, err := s.db.Exec(`UPDATE commands SET instruction = ?, context = '' WHERE seq = ?`,
		fenced, requested.Seq); err != nil {
		t.Fatalf("write the legacy row: %v", err)
	}

	read, found, err := s.CommandBySeq(requested.Seq)
	if err != nil || !found {
		t.Fatalf("read the legacy row: found=%t err=%v", found, err)
	}
	if read.Instruction != "research jepa models" {
		t.Fatalf("the legacy ask read as %q", read.Instruction)
	}
	if !strings.Contains(read.Context, "multi level dag") {
		t.Fatalf("the legacy context read as %q", read.Context)
	}
	if strings.Contains(read.Instruction, ForkedContextPrefix) {
		t.Fatalf("the fence survived into the ask: %q", read.Instruction)
	}
	// Recomposed, it is the string the old row held — nothing was lost by
	// reading it as two.
	if !strings.Contains(read.Brief(), "multi level dag") ||
		!strings.HasPrefix(read.Brief(), "research jepa models") {
		t.Fatalf("the legacy brief did not recompose:\n%s", read.Brief())
	}
}

// A database opened by a newer build gains the column, and a replay of the
// events that predate it lands the same two fields.
func TestTheContextColumnArrivesByMigrationAndSurvivesRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-context-column.db")
	graph, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	requested, err := graph.RequestCommand(Command{
		SessionID:   "design",
		Kind:        CommandSplice,
		Instruction: "go and build the exporter",
		Context:     "them: the export keeps the column order",
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}
	// Take the column away, exactly as a database written by the previous build
	// would have it, and let the migration put it back.
	if _, err := graph.db.Exec(`ALTER TABLE commands DROP COLUMN context`); err != nil {
		t.Fatalf("drop the column: %v", err)
	}
	if err := graph.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopened.Close()
	if found, err := tableHasColumn(reopened.db, "commands", "context"); err != nil || !found {
		t.Fatalf("context column migration: found=%t err=%v", found, err)
	}
	// The event is the truth and the table is a view of it, so a rebuild is what
	// makes the recovered column say the right thing rather than the default.
	if err := reopened.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	read, found, err := reopened.CommandBySeq(requested.Seq)
	if err != nil || !found {
		t.Fatalf("read after rebuild: found=%t err=%v", found, err)
	}
	if read.Instruction != "go and build the exporter" || read.Context != "them: the export keeps the column order" {
		t.Fatalf("the replayed command = instruction %q context %q", read.Instruction, read.Context)
	}
}

// The duplicate guard is a test on the person's WORDS, and two forks out of one
// room share every word of the room. Reading the composed string would collapse
// two genuinely different asks into one job — the exact failure sameAsk's
// deliberately high bar exists to avoid, arrived at from the other direction.
func TestTwoDifferentAsksOutOfOneConversationAreNotTwins(t *testing.T) {
	s := openThreadStore(t)
	room := `them: the exporter needs the column order kept
you, earlier: understood, source order, header row stays
them: and the importer has the same problem`

	if _, err := s.RequestCommand(Command{
		SessionID: "design", Kind: CommandSplice,
		Instruction: "build the exporter", Context: room,
	}); err != nil {
		t.Fatalf("request the first ask: %v", err)
	}
	if _, err := s.RequestCommand(Command{
		SessionID: "design", Kind: CommandSplice,
		Instruction: "write the migration notes", Context: room,
	}); err != nil {
		t.Fatalf("the second ask was refused as a twin of the first: %v", err)
	}
	pending, err := s.PendingCommands(10)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending = %d err=%v, want both asks", len(pending), err)
	}
}
