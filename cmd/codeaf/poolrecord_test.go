package main

// The Model Pool's judge, at the chat door: what a landing records, what it
// costs, and what it refuses to do.
//
// Every test here runs the real hook against a temp profile — a fake ask
// standing in for the provider call, a fake catalog standing in for the
// list — and reads back the three records the brief names: the own sheet, the
// outbox, and the usage ledger. The pool's mode is pinned from the
// environment, the same word `model_pool` resolves through in production.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/pool/judge"
	"github.com/Agent-Field/codeaf/internal/pool/record"
	"github.com/Agent-Field/codeaf/internal/session"
)

// poolTestCatalog is three rows: the two models the crew held, and one model
// outside it, dear enough at its published coding index to be picked and with
// a price and "tools" published, which is what Pick asks a candidate to carry.
func poolTestCatalog() []catalog.Model {
	return []catalog.Model{
		{ID: "crew/worker", PromptPrice: 1, CompletionPrice: 2, CodingIndex: 80, Parameters: []string{"tools"}},
		{ID: "crew/high", PromptPrice: 1, CompletionPrice: 2, CodingIndex: 80, Parameters: []string{"tools"}},
		{ID: "other/judge", PromptPrice: 0.5, CompletionPrice: 1, CodingIndex: 90, Parameters: []string{"tools"}},
	}
}

// poolTestAsk is the fake ask: it records the model it was asked for, bills
// the call exactly as the real ask bills one — the provider's own receipt, a
// fifth of a cent — and answers the one JSON object a seat's answer is.
func poolTestAsk(settings config.Config, asked *[]string) func(model string) judge.Ask {
	receipt := 0.0002
	return func(model string) judge.Ask {
		return func(context.Context, string, string) (string, error) {
			*asked = append(*asked, model)
			recordPoolUsage(settings, model, &ai.Usage{PromptTokens: 10, CompletionTokens: 20, Cost: &receipt})
			return `{"score": 88, "reason": "the delivered work does what the brief asked"}`, nil
		}
	}
}

// poolTestLanding is one landed task with both judged seats held.
func poolTestLanding() session.TaskLanding {
	return session.TaskLanding{
		ID:          7,
		State:       session.TaskDone,
		Brief:       "read ALPHA and say what it holds",
		Deliverable: "the reading",
		Report:      "the report the node landed with",
		Wrote:       []string{"alpha.md"},
		Changed:     1,
		Checks:      []string{"go test ./..."},
		Worker:      "crew/worker",
		High:        "crew/high",
		Tokens:      150_000,
	}
}

// restoreOwnCells puts the picker's own-cells seam back after a test that
// repointed it, so no other test in the process reads this test's sheet.
func restoreOwnCells(t *testing.T) {
	t.Helper()
	previous := config.AutoOwnCells
	t.Cleanup(func() { config.AutoOwnCells = previous })
}

// TestPoolJudgeHookScoresALandedTaskIntoItsOwnSheetAndAnswersTheNewCells runs
// one landing through the real hook and reads back the whole of it: one cell
// per held seat in the install's own sheet, the seam answering those cells at
// once, the judge's questions asked of no model the crew held, and the calls
// billed to the judge's seat.
func TestPoolJudgeHookScoresALandedTaskIntoItsOwnSheetAndAnswersTheNewCells(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")
	restoreOwnCells(t)

	profileDir := t.TempDir()
	settings := config.Config{}
	var asked []string
	hook := poolJudgeHook(settings, profileDir, t.TempDir(), poolTestCatalog, poolTestAsk(settings, &asked), time.Now)
	if hook == nil {
		t.Fatal("a pool whose mode allows reading built no hook")
	}
	hook(poolTestLanding())

	sheet, err := record.LoadSheet(record.OwnSheetPath(config.ProfilePath(profileDir, "pool")))
	if err != nil {
		t.Fatalf("the own sheet: %v", err)
	}
	cells := record.Cells(sheet)
	if len(cells) != 2 {
		t.Fatalf("the sheet holds %d cells, want one per held seat: %+v", len(cells), cells)
	}
	seen := map[string]bool{}
	for _, cell := range cells {
		seen[cell.Role+"/"+cell.Model] = true
		if cell.N != 1 {
			t.Fatalf("the %s seat's cell holds %d readings, want 1", cell.Role, cell.N)
		}
	}
	for _, seat := range []string{"worker/crew/worker", "high/crew/high"} {
		if !seen[seat] {
			t.Fatalf("the sheet holds no cell for %s: %+v", seat, cells)
		}
	}

	if config.AutoOwnCells == nil {
		t.Fatal("the picker's own-cells seam was not repointed after the landing")
	}
	if answered := config.AutoOwnCells(); len(answered) != 2 {
		t.Fatalf("the seam answers %d cells, want the 2 the sheet now holds", len(answered))
	}

	if len(asked) != 2 || asked[0] != "other/judge" || asked[1] != "other/judge" {
		t.Fatalf("the judge asked %v, want only other/judge — never a model the crew held", asked)
	}

	session.FlushUsage()
	rows, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatalf("the usage ledger: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the ledger holds %d rows, want one per seat question", len(rows))
	}
	for _, row := range rows {
		if row.Seat != session.SeatJudge {
			t.Fatalf("a call was billed to seat %q, want judge", row.Seat)
		}
		if row.Model != "other/judge" {
			t.Fatalf("a call was billed to model %q", row.Model)
		}
		if row.Input != 10 || row.Output != 20 {
			t.Fatalf("a row carries %d in / %d out", row.Input, row.Output)
		}
	}
}

// TestPoolJudgeHookAppendsOutboxRowsOnlyWhenTheModeSends walks the three
// postures one after another: a mode that sends appends one row per seat, a
// mode whose submit address was emptied sends nothing, and a read-only mode
// records locally only.
func TestPoolJudgeHookAppendsOutboxRowsOnlyWhenTheModeSends(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	restoreOwnCells(t)

	profileDir := t.TempDir()
	outboxFile := filepath.Join(config.ProfilePath(profileDir, "pool"), "outbox.jsonl")
	settings := config.Config{}
	var asked []string
	models := poolTestCatalog

	hook := func() func(session.TaskLanding) {
		return poolJudgeHook(settings, profileDir, t.TempDir(), models, poolTestAsk(settings, &asked), time.Now)
	}

	// A pool that sends: one row per seat question, outbox beside the sheet.
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")
	hook()(poolTestLanding())
	data, err := os.ReadFile(outboxFile)
	if err != nil {
		t.Fatalf("the outbox: %v", err)
	}
	if lines := countLines(data); lines != 2 {
		t.Fatalf("the outbox holds %d rows, want 2", lines)
	}

	// A submit address emptied on purpose: the sheet is still observed, and
	// nothing waits to leave.
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "")
	os.Remove(outboxFile)
	hook()(poolTestLanding())
	if _, err := os.Stat(outboxFile); !os.IsNotExist(err) {
		t.Fatalf("a pool with no submit address wrote %s", outboxFile)
	}

	// A read-only pool: the same, and the sheet is still kept.
	t.Setenv("CODEAF_MODEL_POOL", "read")
	hook()(poolTestLanding())
	if _, err := os.Stat(outboxFile); !os.IsNotExist(err) {
		t.Fatalf("a read-only pool wrote %s", outboxFile)
	}
	sheet, err := record.LoadSheet(record.OwnSheetPath(config.ProfilePath(profileDir, "pool")))
	if err != nil {
		t.Fatalf("the own sheet after a read-only landing: %v", err)
	}
	if got := len(record.Cells(sheet)); got != 2 {
		t.Fatalf("the sheet holds %d cells after three landings, want 2 (one per seat, observed three times)", got)
	}
}

// TestPoolJudgeHookDoesNothingUnderModeOff is the switch: a pool whose mode
// forbids reading builds no hook at all, so a landing costs no call and
// writes nothing.
func TestPoolJudgeHookDoesNothingUnderModeOff(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "off")
	restoreOwnCells(t)

	profileDir := t.TempDir()
	settings := config.Config{}
	var asked []string
	if hook := poolJudgeHook(settings, profileDir, t.TempDir(), poolTestCatalog, poolTestAsk(settings, &asked), time.Now); hook != nil {
		t.Fatal("a pool whose mode forbids reading built a hook anyway")
	}
	if _, err := os.Stat(record.OwnSheetPath(config.ProfilePath(profileDir, "pool"))); !os.IsNotExist(err) {
		t.Fatal("a pool whose mode forbids reading wrote an own sheet")
	}
	if len(asked) != 0 {
		t.Fatalf("a pool whose mode forbids reading asked %v", asked)
	}
}

// TestPoolJudgeAskBillsTheJudgeSeatFromTheModelsPrice pins the two readings
// the bill can take: the provider's own receipt when one arrived, and the
// model's published price over the tokens when none did — with zero meaning
// nobody published one, which reads as "nobody said" and never as free.
func TestPoolJudgeAskBillsTheJudgeSeatFromTheModelsPrice(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())

	// The receipt wins, whatever the price says.
	receipt := 0.0007
	recordPoolUsage(config.Config{}, "other/judge", &ai.Usage{PromptTokens: 10, CompletionTokens: 20, Cost: &receipt})
	// No receipt and no published price: the tokens still happened, so the row
	// carries them with no dollars — zero reads as "nobody said", never free.
	recordPoolUsage(config.Config{}, "other/judge", &ai.Usage{PromptTokens: 10, CompletionTokens: 20})
	session.FlushUsage()

	rows, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatalf("the usage ledger: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the ledger holds %d rows, want 2", len(rows))
	}
	if rows[0].Seat != session.SeatJudge || rows[0].Model != "other/judge" {
		t.Fatalf("the row names seat %q model %q", rows[0].Seat, rows[0].Model)
	}
	if rows[0].USD != receipt {
		t.Fatalf("the row says $%v, want the provider's own $%v", rows[0].USD, receipt)
	}
	if rows[1].USD != 0 || rows[1].Input != 10 || rows[1].Output != 20 {
		t.Fatalf("an unpriced row says $%v over %d/%d tokens", rows[1].USD, rows[1].Input, rows[1].Output)
	}
}

// TestPoolJudgeHookGivesEachSeatsQuestionItsOwnShareOfTheLandingTime pins
// the share itself: the landing's context spans every seat and one to spare,
// but each seat's question is wrapped in its own judgeTimeout, so the second
// question's deadline sits a full share after the moment it was asked and a
// slow first answer cannot eat it. The first answer is slow — it holds its
// question before answering — and the share is read off the deadlines, not
// waited out: judgeTimeout is a constant nobody wants a test to sit through.
func TestPoolJudgeHookGivesEachSeatsQuestionItsOwnShareOfTheLandingTime(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")
	restoreOwnCells(t)

	profileDir := t.TempDir()
	settings := config.Config{}
	const slow = 50 * time.Millisecond
	var askedAt []time.Time
	var deadlines []time.Time
	ask := func(model string) judge.Ask {
		return func(ctx context.Context, system, user string) (string, error) {
			askedAt = append(askedAt, time.Now())
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("a seat's question carries no deadline")
			}
			deadlines = append(deadlines, deadline)
			if len(askedAt) == 1 {
				// The first answer takes its time; the share it spends is the
				// share the second question must not have lost.
				select {
				case <-time.After(slow):
				case <-ctx.Done():
					return "", ctx.Err()
				}
			}
			return `{"score": 88, "reason": "the delivered work does what the brief asked"}`, nil
		}
	}
	hook := poolJudgeHook(settings, profileDir, t.TempDir(), poolTestCatalog, ask, time.Now)
	if hook == nil {
		t.Fatal("a pool whose mode allows reading built no hook")
	}
	hook(poolTestLanding())

	if len(deadlines) != 2 || len(askedAt) != 2 {
		t.Fatalf("the judge asked %d questions, want 2", len(deadlines))
	}
	// The second question is asked after the first has answered, so its own
	// share is judged from where it stands, not from the landing's start: a
	// full judgeTimeout ahead of the asking, and no more than that — the
	// landing's wider bound is not what the question was handed.
	share := deadlines[1].Sub(askedAt[1])
	if share < judgeTimeout-time.Second {
		t.Fatalf("the second question holds %v of context, want at least its own %v share", share, judgeTimeout-time.Second)
	}
	if share > judgeTimeout {
		t.Fatalf("the second question holds %v of context, want its own %v share and not the landing's", share, judgeTimeout)
	}
	// The time the first answer spent moved the second deadline out by as
	// much; it did not come off the second question's share.
	if moved := deadlines[1].Sub(deadlines[0]); moved < slow {
		t.Fatalf("the second deadline sits %v after the first, want at least the %v the first answer took", moved, slow)
	}

	sheet, err := record.LoadSheet(record.OwnSheetPath(config.ProfilePath(profileDir, "pool")))
	if err != nil {
		t.Fatalf("the own sheet: %v", err)
	}
	cells := record.Cells(sheet)
	if len(cells) != 2 {
		t.Fatalf("the sheet holds %d cells, want one per held seat: %+v", len(cells), cells)
	}
	seen := map[string]bool{}
	for _, cell := range cells {
		seen[cell.Role+"/"+cell.Model] = true
	}
	for _, seat := range []string{"worker/crew/worker", "high/crew/high"} {
		if !seen[seat] {
			t.Fatalf("the sheet holds no cell for %s: %+v", seat, cells)
		}
	}
}

// TestPoolJudgeHookStillScoresTheSecondSeatWhenTheFirstSeatsShareRunsOut is
// the failure the share exists for: the first question's share runs out and
// the answer is the context's own error, and the second seat is scored anyway
// with a full share of its own — its share began when its question was asked,
// not when the landing did. The share running out is what a provider call
// answers once its context gives out; the ninety seconds themselves are not
// waited out here.
func TestPoolJudgeHookStillScoresTheSecondSeatWhenTheFirstSeatsShareRunsOut(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")
	restoreOwnCells(t)

	profileDir := t.TempDir()
	settings := config.Config{}
	question := 0
	var secondAskedAt, secondDeadline time.Time
	ask := func(model string) judge.Ask {
		return func(ctx context.Context, system, user string) (string, error) {
			question++
			if question == 1 {
				return "", context.DeadlineExceeded
			}
			secondAskedAt = time.Now()
			secondDeadline, _ = ctx.Deadline()
			return `{"score": 88, "reason": "the delivered work does what the brief asked"}`, nil
		}
	}
	hook := poolJudgeHook(settings, profileDir, t.TempDir(), poolTestCatalog, ask, time.Now)
	if hook == nil {
		t.Fatal("a pool whose mode allows reading built no hook")
	}
	hook(poolTestLanding())

	if question != 2 {
		t.Fatalf("the judge asked %d questions, want the second seat asked after the first's share ran out", question)
	}
	if share := secondDeadline.Sub(secondAskedAt); share < judgeTimeout-time.Second {
		t.Fatalf("the second question holds %v of context, want at least its own %v share", share, judgeTimeout-time.Second)
	}

	sheet, err := record.LoadSheet(record.OwnSheetPath(config.ProfilePath(profileDir, "pool")))
	if err != nil {
		t.Fatalf("the own sheet: %v", err)
	}
	cells := record.Cells(sheet)
	if len(cells) != 1 || cells[0].Role != "high" || cells[0].Model != "crew/high" {
		t.Fatalf("the sheet holds %+v, want the high seat scored after the worker's share ran out", cells)
	}
	if cells[0].N != 1 {
		t.Fatalf("the high seat's cell holds %d readings, want 1", cells[0].N)
	}
}

// countLines counts the newlines a row-per-line file holds.
func countLines(data []byte) int {
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	return lines
}
