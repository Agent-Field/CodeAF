package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// THE LEDGER AND THE TRANSCRIPT HOLD THE SAME MONEY. A turn that seals writes
// both, and the line the machine keeps has to be able to say whose the money was.
func TestASealedTurnLandsInTheMachineLedger(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.usageLedger = ledger
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	agent.sealTurn(Usage{Input: 900, Output: 120, CostUSD: 0.31, Calls: 2}, time.Now().Add(-time.Second), "opus-4.1")
	FlushUsage()

	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("the seal wrote %d ledger lines, want 1", len(lines))
	}
	line := lines[0]
	if line.Model != "opus-4.1" || line.Calls != 2 || line.USD != 0.31 {
		t.Fatalf("the ledger line is %+v", line)
	}
	if line.Input != 900 || line.Output != 120 {
		t.Fatalf("the tokens are %+v", line)
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

// A FOLD IS NOT A CALL. A node journals its own turns and its total is folded
// into the conversation afterwards; counting both would double the machine's
// bill for every task it ever ran.
func TestFoldingAChildsTallyWritesNoSecondLedgerLine(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.usageLedger = ledger
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
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
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.usageLedger = ledger
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
		config.standingItemID = "item-6am"
		config.taskID = 4
	})
	agent.sealTurn(Usage{Input: 100, Output: 20, CostUSD: 0.004, Calls: 1}, time.Now(), "haiku-4.5")
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
