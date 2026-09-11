package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// recordUsage writes one line AND WAITS for it to reach the disk, which is what
// a test wants and what a turn must never do: [RecordUsage] hands the row to a
// background writer, so a test that read the file straight afterwards — or wrote
// its own next byte to it — would be racing that writer.
func recordUsage(t *testing.T, path string, line UsageLine) {
	t.Helper()
	RecordUsage(path, line)
	FlushUsage()
}

// billedCall is one provider answer with an accounting block on it — the shape
// the ledger's per-call row is written from.
func billedCall(model string, in, out int, usd float64) *ai.Response {
	return &ai.Response{Model: model, Usage: &ai.Usage{
		PromptTokens: in, CompletionTokens: out, Cost: &usd,
	}}
}

// bankCall drives the door a real turn drives: the per-call site that folds one
// answer into the turn and the session's meter and writes the machine's ledger
// row in the same breath ([Agent.addUsage]). Since issue #269 that is the ONLY
// place a ledger row for a turn's own call comes from, so a test that wants a
// row makes a call rather than sealing a turn.
func bankCall(agent *Agent, turn *Usage, model string, in, out int, usd float64, lane laneFacts) {
	agent.addUsage(turn, billedCall(model, in, out, usd), model, "", lane)
}

// ledgerAgent is an agent writing to a ledger of its own, which is what every
// test in this file needs and nothing else.
func ledgerAgent(t *testing.T, ledger string, more ...func(*Config)) (*Agent, string) {
	t.Helper()
	return newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.usageLedger = ledger
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
		for _, apply := range more {
			apply(config)
		}
	})
}

func usageAt(t *testing.T, day string) time.Time {
	t.Helper()
	at, err := time.ParseInLocation("2006-01-02 15:04", day, time.Local)
	if err != nil {
		t.Fatalf("bad test date %q: %v", day, err)
	}
	return at
}

// The whole point of the file is that it can be read back from anywhere, so the
// first thing to pin is that a row survives the round trip whole.
func TestAUsageLineComesBackTheWayItWasWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spend", UsageLedgerName)
	at := usageAt(t, "2026-08-25 13:11")
	recordUsage(t, path, UsageLine{
		At: at, Model: "opus-4.1", Role: "title", Calls: 3,
		Input: 1200, Output: 340, USD: 0.42,
		Session: "aaaa1111aaaa1111", Task: "7", Standing: "", Workspace: "/repo",
	})

	lines, err := ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("read %d lines, want 1: %+v", len(lines), lines)
	}
	line := lines[0]
	if !line.At.Equal(at) {
		t.Fatalf("At is %s, want %s", line.At, at)
	}
	if line.Day != "2026-08-25" {
		t.Fatalf("Day is %q, want the local calendar day", line.Day)
	}
	if line.Model != "opus-4.1" || line.Role != "title" || line.Calls != 3 {
		t.Fatalf("the call is %+v", line)
	}
	if line.Input != 1200 || line.Output != 340 || line.USD != 0.42 {
		t.Fatalf("the figures are %+v", line)
	}
	if line.Session != "aaaa1111aaaa1111" || line.Task != "7" || line.Workspace != "/repo" {
		t.Fatalf("the ids are %+v", line)
	}
}

// THE EMPTINESS LAW, applied to a file: a day with no line in it is a day
// nothing was spent, and a row of zeroes would make it look measured.
func TestACallThatSpentNothingWritesNoLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	recordUsage(t, path, UsageLine{At: time.Now(), Model: "opus-4.1", Calls: 1})
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a zero call created a ledger")
	}
	lines, err := ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatalf("a missing ledger is a machine that has spent nothing, not an error: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("read %d lines from nothing", len(lines))
	}
}

// Two processes appending can interleave a row in the limit. One bad line costs
// one call's record and never the page.
func TestABadLineCostsOneRowAndNotTheLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	at := usageAt(t, "2026-08-24 09:00")
	recordUsage(t, path, UsageLine{At: at, Model: "a", Calls: 1, Input: 10, USD: 0.01})
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := file.WriteString("{\"at\":\"not a time\"\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	file.Close()
	recordUsage(t, path, UsageLine{At: at.Add(time.Hour), Model: "b", Calls: 1, Input: 10, USD: 0.02})

	lines, err := ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("read %d lines around the bad one, want 2: %+v", len(lines), lines)
	}
}

// A window is what every reader of this file asks for, so the floor has to be
// exact at its own edge.
func TestReadUsageKeepsTheFloorAndDropsWhatIsBelowIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	floor := usageAt(t, "2026-08-20 12:00")
	for _, at := range []time.Time{floor.Add(-time.Hour), floor, floor.Add(time.Hour)} {
		recordUsage(t, path, UsageLine{At: at, Model: "m", Calls: 1, Input: 5, USD: 0.01})
	}
	lines, err := ReadUsage(path, floor)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("read %d lines at or after the floor, want 2", len(lines))
	}
	if lines[0].At.Before(floor) {
		t.Fatalf("the first line is %s, below the floor %s", lines[0].At, floor)
	}
}

// THE CACHE'S WHOLE REASON: home's clock beats every three seconds and the file
// grows by a line per call, so a grown file must be read from where the last
// read stopped rather than from the beginning.
func TestTheCacheReadsOnlyWhatWasAppended(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	at := usageAt(t, "2026-08-25 08:00")
	recordUsage(t, path, UsageLine{At: at, Model: "a", Calls: 1, Input: 10, USD: 0.10})

	cache := &UsageCache{Path: path}
	first, err := cache.Read(time.Time{})
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first read has %d lines, want 1", len(first))
	}
	consumed := cache.read
	if consumed == 0 {
		t.Fatal("the cache consumed nothing and yet answered a line")
	}

	// The same file, unchanged: answered from memory, with the offset untouched.
	if again, err := cache.Read(time.Time{}); err != nil || len(again) != 1 {
		t.Fatalf("unchanged read gave %d lines, %v", len(again), err)
	}

	// Grown by one line: the cache must hold two and must have read only the
	// second one's bytes.
	recordUsage(t, path, UsageLine{At: at.Add(time.Hour), Model: "b", Calls: 1, Input: 10, USD: 0.20})
	// A modification time with a one-second resolution would otherwise make the
	// second write invisible; the size changed too, and the cache tests both.
	grown, err := cache.Read(time.Time{})
	if err != nil {
		t.Fatalf("grown read: %v", err)
	}
	if len(grown) != 2 {
		t.Fatalf("grown read has %d lines, want 2", len(grown))
	}
	if cache.read <= consumed {
		t.Fatalf("the cache offset did not move: %d then %d", consumed, cache.read)
	}
	if grown[0].Model != "a" || grown[1].Model != "b" {
		t.Fatalf("the tail read landed out of order: %+v", grown)
	}
}

// A file that SHRANK is a different file wearing the same name — rotated,
// truncated, replaced — and nothing the cache holds is about it.
func TestTheCacheStartsOverWhenTheLedgerShrinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	at := usageAt(t, "2026-08-25 08:00")
	for i := 0; i < 4; i++ {
		recordUsage(t, path, UsageLine{At: at.Add(time.Duration(i) * time.Hour), Model: "a", Calls: 1, Input: 10, USD: 0.10})
	}
	cache := &UsageCache{Path: path}
	if lines, err := cache.Read(time.Time{}); err != nil || len(lines) != 4 {
		t.Fatalf("first read gave %d lines, %v", len(lines), err)
	}
	if err := os.Truncate(path, 0); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	recordUsage(t, path, UsageLine{At: at.Add(9 * time.Hour), Model: "z", Calls: 1, Input: 10, USD: 0.10})
	lines, err := cache.Read(time.Time{})
	if err != nil {
		t.Fatalf("read after truncate: %v", err)
	}
	if len(lines) != 1 || lines[0].Model != "z" {
		t.Fatalf("the cache kept lines from a file that is gone: %+v", lines)
	}
}

// A REPLACEMENT THAT GREW PAST THE OLD ONE IS STILL A REPLACEMENT. A ledger
// rotated away and started again can be longer than what the cache had already
// parsed by the time the next beat looks at it — so a cache that asked only
// "did it shrink" would keep the rows of a file that is gone AND seek into the
// new one past a prefix it never read, adding two ledgers together with the
// middle missing. Identity is the question, not size.
func TestTheCacheStartsOverWhenTheLedgerIsReplacedAndRegrows(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	at := usageAt(t, "2026-08-25 08:00")
	for i := 0; i < 3; i++ {
		recordUsage(t, path, UsageLine{At: at.Add(time.Duration(i) * time.Hour),
			Model: "old", Calls: 1, Input: 10, USD: 0.10})
	}
	cache := &UsageCache{Path: path}
	if lines, err := cache.Read(time.Time{}); err != nil || len(lines) != 3 {
		t.Fatalf("first read gave %d lines, %v", len(lines), err)
	}

	// The rotation: the ledger is moved aside and a NEW file takes its name,
	// then grows past the length of the one it replaced before anybody looks.
	// The rows are appended directly, because the ledger's own writer is holding
	// the descriptor that the rename carried away with it.
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	for i := 0; i < 6; i++ {
		row, err := json.Marshal(UsageLine{At: at.Add(time.Duration(24+i) * time.Hour),
			Day: "2026-08-26", Model: "new", Calls: 1, Input: 10, USD: 0.20})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := appendRaw(path, string(row)+"\n"); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	lines, err := cache.Read(time.Time{})
	if err != nil {
		t.Fatalf("read after the rotation: %v", err)
	}
	if len(lines) != 6 {
		t.Fatalf("the cache holds %d lines, want the six of the ledger that is there: %+v", len(lines), lines)
	}
	for _, line := range lines {
		if line.Model != "new" {
			t.Fatalf("a row from the rotated-away ledger survived: %+v", lines)
		}
	}
}

// The same size and the same modification time are not the same file. A ledger
// replaced by one that happens to match both would otherwise be answered out of
// memory for as long as the window stayed open.
func TestTheCacheNoticesAReplacementOfTheSameSizeAndTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	at := usageAt(t, "2026-08-25 08:00")
	recordUsage(t, path, UsageLine{At: at, Model: "aaa", Calls: 1, Input: 10, USD: 0.10})
	cache := &UsageCache{Path: path}
	if lines, err := cache.Read(time.Time{}); err != nil || len(lines) != 1 {
		t.Fatalf("first read gave %d lines, %v", len(lines), err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	// A different file of the same length, wearing the same name and the same
	// modification time: exactly what a rotation with a same-shaped replacement
	// leaves behind.
	replacement := filepath.Join(filepath.Dir(path), "replacement")
	recordUsage(t, replacement, UsageLine{At: at, Model: "zzz", Calls: 1, Input: 10, USD: 0.10})
	if err := os.Rename(replacement, path); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if err := os.Chtimes(path, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	lines, err := cache.Read(time.Time{})
	if err != nil {
		t.Fatalf("read after the replacement: %v", err)
	}
	if len(lines) != 1 || lines[0].Model != "zzz" {
		t.Fatalf("the cache answered from the file that is gone: %+v", lines)
	}
}

// THE ROWS ARE NOT IN TIME ORDER AND THE FLOOR MUST NOT ASSUME THEY ARE. This
// ledger is machine-wide: a second aforge can stamp a call at 09:00 and land it
// after this one's 10:00 row, because its own turn ran in between. A floor read
// as a prefix cut stops at the 10:00 row and hands back everything behind it,
// which includes an hour nobody asked about.
func TestTheCachesFloorAsksEveryRowAndNotJustTheFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	early := usageAt(t, "2026-08-25 09:00")
	floor := usageAt(t, "2026-08-25 09:30")
	late := usageAt(t, "2026-08-25 10:00")
	// The order on disk is the order the appends landed, not the order of the
	// stamps: the late row first, then the early one behind it.
	recordUsage(t, path, UsageLine{At: late, Model: "late", Calls: 1, Input: 10, USD: 0.20})
	recordUsage(t, path, UsageLine{At: early, Model: "early", Calls: 1, Input: 10, USD: 0.10})

	cache := &UsageCache{Path: path}
	lines, err := cache.Read(floor)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(lines) != 1 || lines[0].Model != "late" {
		t.Fatalf("the floor let %d rows through, want only the one above it: %+v", len(lines), lines)
	}
	// And the cache still holds both, so a later question about a wider window
	// is answered without re-reading the file.
	if all, err := cache.Read(time.Time{}); err != nil || len(all) != 2 {
		t.Fatalf("the cache holds %d rows with no floor, want both: %v", len(all), err)
	}
}

// ONE ROW PER CALL, WRITTEN AS THE CALL IS MADE — the restatement issue #269
// asks for of what used to be "a sealed turn lands in the machine ledger".
//
// The old shape wrote ONE row at the turn's seal carrying the whole turn's
// tally, so `calls` on a row was a turn's worth (an observed 41) and a turn that
// never sealed wrote nothing at all. Now each call writes its own row as it is
// decoded, every row says `calls: 1`, and the row still has to be able to say
// whose the money was.
func TestEveryCallLandsInTheMachineLedgerAsItIsMade(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, workspace := ledgerAgent(t, ledger)

	var turn Usage
	bankCall(agent, &turn, "opus-4.1", 900, 120, 0.31, laneFacts{})
	bankCall(agent, &turn, "opus-4.1", 400, 60, 0.09, laneFacts{})
	FlushUsage()

	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("two calls wrote %d ledger lines, want one each", len(lines))
	}
	for _, line := range lines {
		if line.Calls != 1 {
			t.Fatalf("a row covers %d calls, want exactly 1: %+v", line.Calls, line)
		}
		if line.Model != "opus-4.1" {
			t.Fatalf("the row names model %q: %+v", line.Model, line)
		}
		if line.Session == "" {
			t.Fatal("the ledger line names no conversation")
		}
		if line.Task != "" {
			t.Fatalf("a conversation's line claims task %q", line.Task)
		}
		if line.Workspace != workspace {
			t.Fatalf("the workspace is %q, want %q", line.Workspace, workspace)
		}
	}
	line := lines[0]
	if line.USD != 0.31 || line.Input != 900 || line.Output != 120 {
		t.Fatalf("the first call's row is %+v, want its own figures and not the turn's sum", line)
	}
	// AND THE SEAL WRITES NOTHING HERE ANY MORE. A turn's shape is the
	// transcript's business; the ledger's grain is the call.
	agent.sealTurn(turn, time.Now().Add(-time.Second), "opus-4.1")
	FlushUsage()
	after, err := ReadUsage(ledger, time.Time{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("the seal added %d rows on top of the calls, want none", len(after)-2)
	}
}

// A FOLD IS NOT A CALL. A node journals its own turns and its total is folded
// into the conversation afterwards; counting both would double the machine's
// bill for every task it ever ran.
func TestFoldingAChildsTallyWritesNoSecondLedgerLine(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger)
	cost := 0.44
	agent.addFoldedUsage(&ai.Response{Usage: &ai.Usage{
		PromptTokens: 800, CompletionTokens: 200, Cost: &cost,
	}}, "sonnet-4.5", 12)
	FlushUsage()

	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("a fold wrote %d ledger lines: %+v", len(lines), lines)
	}
	// The session's own books still moved — the fold is right there and only
	// there.
	if used := agent.Usage(); used.CostUSD != cost {
		t.Fatalf("the session's total is %v, want the folded %v", used.CostUSD, cost)
	}
}

// A standing firing's money belongs to the promise, not to the one-run folder
// the firing happened in.
func TestAStandingFiringsLineNamesTheItem(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger, func(config *Config) {
		config.standingItemID = "item-6am"
		config.taskID = 4
	})
	var turn Usage
	bankCall(agent, &turn, "haiku-4.5", 100, 20, 0.004, laneFacts{})
	FlushUsage()

	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil || len(lines) != 1 {
		t.Fatalf("read %d lines, %v", len(lines), err)
	}
	if lines[0].Standing != "item-6am" {
		t.Fatalf("the line names standing %q", lines[0].Standing)
	}
	if lines[0].Task != "4" {
		t.Fatalf("the line names task %q, want the node it ran as", lines[0].Task)
	}
	// And the subject rollup prefers the promise over the run it spawned.
	rows := UsageBySubject(lines)
	if len(rows) != 1 || rows[0].Kind != SubjectStanding || rows[0].ID != "item-6am" {
		t.Fatalf("the subject rollup is %+v", rows)
	}
}

// Absent ids stay off the wire: a ledger of a million conversation lines must
// not each carry four empty strings.
func TestAConversationLineSpellsNoEmptyIds(t *testing.T) {
	line, err := json.Marshal(UsageLine{At: time.Now(), Day: "2026-08-25", Model: "m", Calls: 1, Input: 5, USD: 0.01, Session: "abc"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, absent := range []string{`"task"`, `"standing"`, `"workspace"`, `"role"`} {
		if strings.Contains(string(line), absent) {
			t.Fatalf("an empty %s reached the file: %s", absent, line)
		}
	}
}

// A WRITE CAUGHT MID-FLIGHT COSTS NOTHING. The tail read's offset must land on
// the beginning of the half-written row, not past it, or every line appended
// after it is lost forever without a word.
func TestAHalfWrittenLineIsReadWholeOnTheNextLook(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	at := usageAt(t, "2026-08-25 08:00")
	recordUsage(t, path, UsageLine{At: at, Model: "a", Calls: 1, Input: 10, USD: 0.10})

	whole, err := json.Marshal(UsageLine{At: at.Add(time.Hour), Day: "2026-08-25", Model: "b", Calls: 1, Input: 10, USD: 0.20})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	half, rest := whole[:len(whole)/2], whole[len(whole)/2:]
	if err := appendRaw(path, string(half)); err != nil {
		t.Fatalf("append: %v", err)
	}

	cache := &UsageCache{Path: path}
	lines, err := cache.Read(time.Time{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(lines) != 1 || lines[0].Model != "a" {
		t.Fatalf("a half-written row was read as a row: %+v", lines)
	}

	if err := appendRaw(path, string(rest)+"\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	lines, err = cache.Read(time.Time{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(lines) != 2 || lines[1].Model != "b" {
		t.Fatalf("the completed row did not come back whole: %+v", lines)
	}
}

// The offset counts BYTES and not tokens. A line carrying anything the reader
// might be tempted to strip — a carriage return before the newline — must move
// the offset by exactly what it occupies, or the next read starts mid-row.
func TestTheTailOffsetCountsEveryByteOfALine(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	at := usageAt(t, "2026-08-25 08:00")
	first, err := json.Marshal(UsageLine{At: at, Day: "2026-08-25", Model: "a", Calls: 1, Input: 10, USD: 0.10})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := appendRaw(path, string(first)+"\r\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	cache := &UsageCache{Path: path}
	if lines, err := cache.Read(time.Time{}); err != nil || len(lines) != 1 {
		t.Fatalf("first read gave %d lines, %v", len(lines), err)
	}
	recordUsage(t, path, UsageLine{At: at.Add(time.Hour), Model: "b", Calls: 1, Input: 10, USD: 0.20})
	lines, err := cache.Read(time.Time{})
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if len(lines) != 2 || lines[1].Model != "b" {
		t.Fatalf("the tail read drifted off the line boundary: %+v", lines)
	}
}

func appendRaw(path, text string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(text)
	return err
}

// ── THE LANE HALF OF A ROW ──────────────────────────────────────────────────
//
// Five fields were added to a file that already holds a year of spending, and
// the whole of their safety is the emptiness law: a zero writes nothing, so an
// ordinary row is exactly as wide as it was, an old row decodes with all five
// absent, and any figure a reader DOES find on a row is one somebody measured.
// These four tests hold that, from both directions.

// TestALaneRowCarriesTheMachineAndTheWaitItMade pins the shape on the wire —
// the field names, not the Go names, because the names on the wire are what a
// person greps and what every other reader of this file has to agree with.
func TestALaneRowCarriesTheMachineAndTheWaitItMade(t *testing.T) {
	at := usageAt(t, "2026-08-25 13:11")
	line := usageFromResponse(
		UsageLine{At: at, Day: "2026-08-25", Model: "deepseek/deepseek-v4-flash", Calls: 1, USD: 0.02},
		"Cloudflare", 768*time.Millisecond, 4*time.Second, 232, true, 0.004,
	)
	encoded, err := json.Marshal(line)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for field, want := range map[string]any{
		"lane":            "Cloudflare",
		"ttft_ms":         float64(768),
		"tps":             58.0,
		"hedged":          true,
		"hedge_waste_usd": 0.004,
	} {
		got, present := wire[field]
		if !present {
			t.Fatalf("the row has no %q: %s", field, encoded)
		}
		if got != want {
			t.Fatalf("%q is %v, want %v", field, got, want)
		}
	}
}

// TestAnOrdinaryRowIsExactlyAsWideAsItWas is the emptiness law held on a file.
//
// Every call that no lane named itself on — every endpoint that is not a
// router, every call this build has ever made until Decision 10's transport
// half lands — must leave the row it always left. A `"lane":""` or a `"tps":0`
// would be this build claiming to have measured a machine it never saw.
func TestAnOrdinaryRowIsExactlyAsWideAsItWas(t *testing.T) {
	at := usageAt(t, "2026-08-25 13:11")
	plain := UsageLine{At: at, Day: "2026-08-25", Model: "m", Calls: 1, Input: 10, Output: 4, USD: 0.01}
	before, err := json.Marshal(plain)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	after, err := json.Marshal(usageFromResponse(plain, "", 0, 0, 4, false, 0))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("a call with no lane widened its row:\n before %s\n  after %s", before, after)
	}
	for _, word := range []string{"reconciled", "lane", "ttft_ms", "tps", "hedged", "hedge_waste_usd"} {
		if strings.Contains(string(after), word) {
			t.Fatalf("the row names %q with nothing to say: %s", word, after)
		}
	}
}

// TestARateIsOnlyWrittenWhenThereWasSomethingToRate keeps the one derivation in
// this file honest. An answer too short or too quick to rate rates the
// handshake and not the lane, and a probe's single token is the extreme case of
// it — so no rate at all is the only true answer there.
func TestARateIsOnlyWrittenWhenThereWasSomethingToRate(t *testing.T) {
	base := UsageLine{At: time.Now(), Model: "m", Calls: 1, USD: 0.01}
	for _, probe := range []struct {
		name   string
		gen    time.Duration
		output int
		want   float64
	}{
		{"no generation window", 0, 400, 0},
		{"no tokens", 4 * time.Second, 0, 0},
		{"a real answer", 4 * time.Second, 232, 58},
	} {
		got := usageFromResponse(base, "Cloudflare", time.Second, probe.gen, probe.output, false, 0).TPS
		if got != probe.want {
			t.Fatalf("%s: the rate is %v, want %v", probe.name, got, probe.want)
		}
	}
	// The first token is separable from the rest for the same reason, and it is
	// written whenever it was timed at all — including on an answer nobody
	// could rate.
	if got := usageFromResponse(base, "Cloudflare", 768*time.Millisecond, 0, 0, false, 0).TTFTms; got != 768 {
		t.Fatalf("the first-token wait is %dms, want 768", got)
	}
}

// TestARowWrittenBeforeLanesExistedStillDecodes is the wire-compatibility half.
// The ledger is append-only and years long; a reader that could not read what
// it wrote last August would be a spend page that lies about last August.
func TestARowWrittenBeforeLanesExistedStillDecodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spend", UsageLedgerName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("make the directory: %v", err)
	}
	const old = `{"at":"2026-08-25T13:11:00Z","day":"2026-08-25","model":"opus-4.1",` +
		`"calls":1,"in":1200,"out":340,"usd":0.42,"session":"aaaa1111aaaa1111"}`
	if err := os.WriteFile(path, []byte(old+"\n"), 0o644); err != nil {
		t.Fatalf("write the old row: %v", err)
	}
	lines, err := ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("read %d lines, want 1", len(lines))
	}
	line := lines[0]
	if line.Model != "opus-4.1" || line.Input != 1200 || line.USD != 0.42 {
		t.Fatalf("the old row lost its figures: %+v", line)
	}
	if line.Lane != "" || line.TTFTms != 0 || line.TPS != 0 || line.Hedged || line.HedgeWasteUSD != 0 {
		t.Fatalf("a row from before lanes existed came back believing something: %+v", line)
	}
}

// ── the lane half, filled by the turn that measured it ──────────────────────
//
// The four tests above pin the SHAPE of the five fields. These pin the WIRING:
// that a running turn stamps a hedge report on its own call, times its own
// stream, and hands both down to the row it seals — and that a turn which
// measured none of it writes a row with those keys absent rather than zeroed.

// rawUsageRows reads the ledger as JSON objects rather than as [UsageLine].
//
// THE EMPTINESS LAW IS ABOUT KEYS. A field nobody measured is absent from the
// row, and a decode into the struct cannot tell an absent key from a zero one —
// which is exactly the difference these tests exist to hold.
func rawUsageRows(t *testing.T, path string) []map[string]any {
	t.Helper()
	FlushUsage()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	var rows []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		row := map[string]any{}
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			t.Fatalf("decode %q: %v", raw, err)
		}
		rows = append(rows, row)
	}
	return rows
}

// sealRow is the one row a sealed turn wrote. A turn may run errands beside
// itself and each of those writes a row of its own, so the seal is picked out
// by the thing that is true of it alone: it names no role.
func sealRow(t *testing.T, path string) map[string]any {
	t.Helper()
	var seals []map[string]any
	for _, row := range rawUsageRows(t, path) {
		if _, named := row["role"]; !named {
			seals = append(seals, row)
		}
	}
	if len(seals) != 1 {
		t.Fatalf("the turn wrote %d rows with no role, want the one seal: %v", len(seals), seals)
	}
	return seals[0]
}

func wantRowFields(t *testing.T, row map[string]any, want map[string]any) {
	t.Helper()
	for field, value := range want {
		got, present := row[field]
		if !present {
			t.Fatalf("the row has no %q: %v", field, row)
		}
		if got != value {
			t.Fatalf("%q is %v, want %v", field, got, value)
		}
	}
}

func wantRowSilentAbout(t *testing.T, row map[string]any, fields ...string) {
	t.Helper()
	for _, field := range fields {
		if got, present := row[field]; present {
			t.Fatalf("the row says %q is %v with nobody having measured it: %v", field, got, row)
		}
	}
}

// A sealed turn's row names the machine that answered it and the wait it made,
// which is the whole question a person asks after a slow afternoon: was it the
// model, or was it the machine we happened to be routed to.
func TestACallsRowNamesTheMachineAndTheWaitItMade(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger)

	began := time.Now()
	agent.turnLane.sent(began)
	agent.turnLane.token(began.Add(768 * time.Millisecond))
	agent.turnLane.token(began.Add(4768 * time.Millisecond))
	facts := agent.turnLane.answered(hedgeSeen{lane: "Cloudflare"}, 232)
	var turn Usage
	bankCall(agent, &turn, "deepseek/deepseek-v4-flash", 900, 232, 0.31, facts)

	row := sealRow(t, ledger)
	wantRowFields(t, row, map[string]any{
		"lane":    "Cloudflare",
		"ttft_ms": float64(768),
		"tps":     58.0,
	})
	// Nothing was rescued, so nothing claims to have been.
	wantRowSilentAbout(t, row, "hedged", "hedge_waste_usd")
}

// A hedge is the one thing in this build that can spend money twice, so a bill
// that cannot be told apart from an ordinary one is a mechanism nobody can
// audit. The winner of the race is the machine the row names.
func TestAHedgedCallsRowCarriesTheRescueAndWhatItWasted(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger)

	began := time.Now()
	agent.turnLane.sent(began)
	agent.turnLane.token(began.Add(400 * time.Millisecond))
	agent.turnLane.token(began.Add(2400 * time.Millisecond))
	facts := agent.turnLane.answered(hedgeSeen{lane: "Baidu", hedged: true, waste: 0.004}, 150)
	var turn Usage
	bankCall(agent, &turn, "deepseek/deepseek-v4-flash", 900, 150, 0.12, facts)

	wantRowFields(t, sealRow(t, ledger), map[string]any{
		"lane":            "Baidu",
		"ttft_ms":         float64(400),
		"tps":             75.0,
		"hedged":          true,
		"hedge_waste_usd": 0.004,
	})
}

// THE EMPTINESS LAW. A turn nobody measured writes a row with none of the five
// keys on it — not a lane of "", not a wait of zero — because a figure a reader
// finds on a row must be one somebody measured.
func TestACallNobodyMeasuredWritesNoneOfTheLaneKeys(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger)
	var turn Usage
	bankCall(agent, &turn, "opus-4.1", 900, 120, 0.31, laneFacts{})

	row := sealRow(t, ledger)
	wantRowSilentAbout(t, row, "lane", "ttft_ms", "tps", "hedged", "hedge_waste_usd")
	// And the row it always wrote is untouched beside them.
	wantRowFields(t, row, map[string]any{"model": "opus-4.1", "usd": 0.31})
}

// A PATH FAULT IS NOT A LANE'S FAULT. When the watch judged the failure to be
// the path rather than the machine there is no fact about the endpoint in the
// seconds at all, so the row must not carry them — the same refusal
// internal/provider's streamWatch.sighting makes about a belief. What the
// rescue cost is still true and stays.
func TestAPathFaultIsNotBlamedOnTheLane(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger)

	began := time.Now()
	agent.turnLane.sent(began)
	agent.turnLane.token(began.Add(9 * time.Second))
	agent.turnLane.token(began.Add(11 * time.Second))
	facts := agent.turnLane.answered(hedgeSeen{lane: "Cloudflare", hedged: true, waste: 0.002, fault: true}, 232)
	var turn Usage
	bankCall(agent, &turn, "deepseek/deepseek-v4-flash", 900, 232, 0.31, facts)

	row := sealRow(t, ledger)
	wantRowSilentAbout(t, row, "ttft_ms", "tps")
	wantRowFields(t, row, map[string]any{
		"lane":            "Cloudflare",
		"hedged":          true,
		"hedge_waste_usd": 0.002,
	})
}

// An errand made a call of its own and nothing watched it, so its row says
// nothing about a machine. The turn's own figures are not the errand's, and a
// row that borrowed them would file one measurement against another request.
func TestAnErrandsRowCarriesNoLaneTheTurnMeasured(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger)

	began := time.Now()
	agent.turnLane.sent(began)
	agent.turnLane.token(began.Add(768 * time.Millisecond))
	agent.turnLane.token(began.Add(4768 * time.Millisecond))
	facts := agent.turnLane.answered(hedgeSeen{lane: "Cloudflare"}, 232)

	cost := 0.002
	agent.addAuxiliaryUsageAs(&ai.Response{Usage: &ai.Usage{
		PromptTokens: 300, CompletionTokens: 12, Cost: &cost,
	}}, "haiku-4.5", 1, auxRoleTitle)
	var turn Usage
	bankCall(agent, &turn, "deepseek/deepseek-v4-flash", 900, 232, 0.31, facts)

	rows := rawUsageRows(t, ledger)
	if len(rows) != 2 {
		t.Fatalf("wrote %d rows, want the errand and the turn's own call: %v", len(rows), rows)
	}
	for _, row := range rows {
		if row["role"] == auxRoleTitle {
			wantRowSilentAbout(t, row, "lane", "ttft_ms", "tps", "hedged", "hedge_waste_usd")
		}
	}
	// And the errand did not eat the turn's own measurement on the way past.
	wantRowFields(t, sealRow(t, ledger), map[string]any{"lane": "Cloudflare", "ttft_ms": float64(768)})
}

// A measurement belongs to exactly one row. A second call that found the first
// one's figures still sitting in the witness would write a lane and a wait that
// nobody measured for it.
func TestASecondCallDoesNotInheritTheFirstsLane(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger)

	began := time.Now()
	agent.turnLane.sent(began)
	agent.turnLane.token(began.Add(768 * time.Millisecond))
	facts := agent.turnLane.answered(hedgeSeen{lane: "Cloudflare"}, 232)
	var turn Usage
	bankCall(agent, &turn, "m", 900, 232, 0.31, facts)
	// The witness was emptied by the answer above, so the second call's own
	// reading is the empty one it actually has.
	bankCall(agent, &turn, "m", 100, 20, 0.01, agent.turnLane.answered(hedgeSeen{}, 20))

	rows := rawUsageRows(t, ledger)
	if len(rows) != 2 {
		t.Fatalf("wrote %d rows, want two calls: %v", len(rows), rows)
	}
	wantRowFields(t, rows[0], map[string]any{"lane": "Cloudflare"})
	wantRowSilentAbout(t, rows[1], "lane", "ttft_ms", "tps")
}

// readHedge is the one place the transport's report is turned into the plain
// values everything downstream of it works in, and the nil report every call in
// a build with no watch behind it has must read as "nobody said".
func TestReadHedgeNamesTheMachineWithoutInventingOne(t *testing.T) {
	seen := readHedge(nil, "  Baidu  ")
	if seen.lane != "Baidu" {
		t.Fatalf("the lane is %q, want the endpoint that answered", seen.lane)
	}
	if seen.hedged || seen.waste != 0 || seen.fault {
		t.Fatalf("a call with no report claims %+v", seen)
	}
	if empty := readHedge(&provider.HedgeReport{}, ""); empty.lane != "" || empty.hedged {
		t.Fatalf("a call nothing named claims %+v", empty)
	}
}

// AND THE WIRING, end to end: a real turn stamps a hedge report on the context
// its request rides, times its own stream across the send seam, and writes a row
// on the call itself carrying what it measured — while naming no lane, because
// nothing on a scripted completer's answer ever names one.
func TestATurnStampsAHedgeReportAndTimesItsOwnStream(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			if provider.HedgeReportFrom(ctx) == nil {
				t.Error("the turn made a call with no hedge report on it")
			}
			// Real waits, because the row records milliseconds and a stream that
			// arrives inside one of them was not timed at all.
			time.Sleep(5 * time.Millisecond)
			provider.Emit(ctx, provider.StreamDelta, "one ")
			time.Sleep(5 * time.Millisecond)
			provider.Emit(ctx, provider.StreamDelta, "two")
			return textResponse("one two"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.usageLedger = ledger
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})

	collect(t, mustSubmit(t, agent, "hello"))

	row := sealRow(t, ledger)
	if _, timed := row["ttft_ms"]; !timed {
		t.Fatalf("a turn that watched its own stream timed nothing: %v", row)
	}
	if _, rated := row["tps"]; !rated {
		t.Fatalf("a turn that watched its own stream rated nothing: %v", row)
	}
	// Nothing named a machine, so the row names none — the emptiness law and the
	// measurement in the same row.
	wantRowSilentAbout(t, row, "lane", "hedged", "hedge_waste_usd")
}
