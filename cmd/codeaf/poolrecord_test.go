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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/pool/judge"
	"github.com/Agent-Field/codeaf/internal/pool/outbox"
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

// poolTildeCatalog is the crew's two seats' models beside a same-vendor row
// dearer than the pick and the one row that should be picked: with the seats'
// ids spelled the way the client routes them — the `~` alias marker on — the
// pool's judge must not come from the crew's own vendor.
func poolTildeCatalog() []catalog.Model {
	return []catalog.Model{
		{ID: "vendor/worker", PromptPrice: 1, CompletionPrice: 2, CodingIndex: 80, Parameters: []string{"tools"}},
		{ID: "vendor/high", PromptPrice: 1, CompletionPrice: 2, CodingIndex: 80, Parameters: []string{"tools"}},
		{ID: "vendor/cheap", PromptPrice: 0.25, CompletionPrice: 0.25, CodingIndex: 90, Parameters: []string{"tools"}},
		{ID: "other/judge", PromptPrice: 0.5, CompletionPrice: 1, CodingIndex: 90, Parameters: []string{"tools"}},
	}
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

	profileDir := t.TempDir()
	settings := config.Config{}
	var asked []string
	hook := poolJudgeHook(settings, profileDir, t.TempDir(), poolTestCatalog, poolTestAsk(settings, &asked), time.Now, "task")
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

// TestPoolJudgeHookSpellsASeatByItsBareModelID is the fresh install's road: a
// seat arrives spelled the way the client routes it — the `~` alias marker on
// the front — and the pool's copy of that id must be the bare `<vendor>/<id>`,
// the spelling the own sheet, the outbox, the judge-last record and the relay's
// schema all read. The judge picked must also stay outside the seats' own
// vendor, which the marker otherwise hides.
func TestPoolJudgeHookSpellsASeatByItsBareModelID(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")

	profileDir := t.TempDir()
	settings := config.Config{}
	var asked []string
	hook := poolJudgeHook(settings, profileDir, t.TempDir(), poolTildeCatalog, poolTestAsk(settings, &asked), time.Now, "task")
	if hook == nil {
		t.Fatal("a pool whose mode allows reading built no hook")
	}
	landing := poolTestLanding()
	landing.Worker = "~vendor/worker"
	landing.High = "~vendor/high"
	hook(landing)

	sheet, err := record.LoadSheet(record.OwnSheetPath(config.ProfilePath(profileDir, "pool")))
	if err != nil {
		t.Fatalf("the own sheet: %v", err)
	}
	cells := record.Cells(sheet)
	seen := map[string]bool{}
	for _, cell := range cells {
		seen[cell.Role+"/"+cell.Model] = true
	}
	for _, seat := range []string{"worker/vendor/worker", "high/vendor/high"} {
		if !seen[seat] {
			t.Fatalf("the sheet holds no cell for %s spelled bare: %+v", seat, cells)
		}
	}
	if seen["worker/~vendor/worker"] || seen["high/~vendor/high"] {
		t.Fatalf("the sheet holds a seat under its tilde spelling: %+v", cells)
	}

	data, err := os.ReadFile(filepath.Join(config.ProfilePath(profileDir, "pool"), "outbox.jsonl"))
	if err != nil {
		t.Fatalf("the outbox: %v", err)
	}
	rows := decodeOutboxRows(t, data)
	if len(rows) != 2 {
		t.Fatalf("the outbox holds %d rows, want one per held seat", len(rows))
	}
	for _, row := range rows {
		if row.Model != "vendor/worker" && row.Model != "vendor/high" {
			t.Fatalf("an outbox row names model %q, want the seat spelled bare", row.Model)
		}
	}

	last := decodeJudgeLast(t, profileDir)
	if strings.Join(last.Seats, ", ") != "vendor/worker, vendor/high" {
		t.Fatalf("the record holds seats %v, want them spelled bare", last.Seats)
	}
	if strings.Join(last.Scored, ", ") != "vendor/worker, vendor/high" {
		t.Fatalf("the record scored %v, want the seats spelled bare", last.Scored)
	}
	if len(asked) != 2 || asked[0] != "other/judge" || asked[1] != "other/judge" {
		t.Fatalf("the judge asked %v, want only other/judge — never a row from the seats' own vendor", asked)
	}
}

// TestWritePendingLandingSpellsThePoolSeatsBare pins the pending file: a row
// the headless doors leave is read back by the restart sweep and scored into
// the pool's records from it, so the seats it carries are written already
// spelled bare — the pool's copy of the id, not the client's routing spelling.
func TestWritePendingLandingSpellsThePoolSeatsBare(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())

	profileDir := t.TempDir()
	landing := poolTestLanding()
	landing.Worker = "~vendor/worker"
	landing.High = "~vendor/high"
	if err := writePendingLanding(profileDir, "exec", landing); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(pendingPath(config.ProfilePath(profileDir, "pool")))
	if err != nil {
		t.Fatalf("the pending file: %v", err)
	}
	var row pendingLanding
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatalf("the pending row does not parse: %v", err)
	}
	if row.Landing.Worker != "vendor/worker" || row.Landing.High != "vendor/high" {
		t.Fatalf("the pending row spells its seats %q and %q, want them bare", row.Landing.Worker, row.Landing.High)
	}
}

// TestPoolJudgeHookAppendsOutboxRowsOnlyWhenTheModeSends walks the three
// postures one after another: a mode that sends appends one row per seat, a
// mode whose submit address was emptied sends nothing, and a read-only mode
// records locally only.
func TestPoolJudgeHookAppendsOutboxRowsOnlyWhenTheModeSends(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())

	profileDir := t.TempDir()
	outboxFile := filepath.Join(config.ProfilePath(profileDir, "pool"), "outbox.jsonl")
	settings := config.Config{}
	var asked []string
	models := poolTestCatalog

	hook := func() func(session.TaskLanding) {
		return poolJudgeHook(settings, profileDir, t.TempDir(), models, poolTestAsk(settings, &asked), time.Now, "task")
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

	profileDir := t.TempDir()
	settings := config.Config{}
	var asked []string
	if hook := poolJudgeHook(settings, profileDir, t.TempDir(), poolTestCatalog, poolTestAsk(settings, &asked), time.Now, "task"); hook != nil {
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
	hook := poolJudgeHook(settings, profileDir, t.TempDir(), poolTestCatalog, ask, time.Now, "task")
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
	hook := poolJudgeHook(settings, profileDir, t.TempDir(), poolTestCatalog, ask, time.Now, "task")
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

// decodeJudgeLast reads the hook's own record of the last landing it judged.
func decodeJudgeLast(t *testing.T, profileDir string) judgeLast {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(config.ProfilePath(profileDir, "pool"), "judge-last.json"))
	if err != nil {
		t.Fatalf("the judge-last record: %v", err)
	}
	var last judgeLast
	if err := json.Unmarshal(data, &last); err != nil {
		t.Fatalf("the judge-last record does not parse: %v", err)
	}
	return last
}

// The hook names the judge that answered and the seats it scored in the small
// record beside the sheet, so a judge failure reads without --debug.
func TestPoolJudgeHookRecordsTheJudgeThatScoredTheLanding(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")

	profileDir := t.TempDir()
	settings := config.Config{}
	var asked []string
	hook := poolJudgeHook(settings, profileDir, t.TempDir(), poolTestCatalog, poolTestAsk(settings, &asked), time.Now, "task")
	hook(poolTestLanding())

	last := decodeJudgeLast(t, profileDir)
	if last.Judge != "other/judge" {
		t.Fatalf("the record names judge %q, want the one that answered", last.Judge)
	}
	if last.Reason != "" {
		t.Fatalf("a scored landing carries a reason: %q", last.Reason)
	}
	if strings.Join(last.Scored, ", ") != "crew/worker, crew/high" {
		t.Fatalf("the record scored %v, want both held seats", last.Scored)
	}
	if strings.Join(last.Seats, ", ") != "crew/worker, crew/high" {
		t.Fatalf("the record holds seats %v, want the crew's own order", last.Seats)
	}
	if last.Task != 7 {
		t.Fatalf("the record names task %d, want 7", last.Task)
	}
	if last.At.IsZero() {
		t.Fatal("the record carries no moment")
	}
	info, err := os.Stat(filepath.Join(config.ProfilePath(profileDir, "pool"), "judge-last.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the record's mode is %v with err %v, want 0600", info, err)
	}
}

// When every candidate fails, the record says which were asked and the last
// error's one line — the reading a person needs when no score landed.
func TestPoolJudgeHookRecordsTheCandidatesAndReasonWhenEveryJudgeFails(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")

	profileDir := t.TempDir()
	settings := config.Config{}
	ask := func(model string) judge.Ask {
		return func(context.Context, string, string) (string, error) {
			return "", errors.New("the model answered with a 429")
		}
	}
	hook := poolJudgeHook(settings, profileDir, t.TempDir(), poolTwoJudgeCatalog, ask, time.Now, "task")
	hook(poolTestLanding())

	last := decodeJudgeLast(t, profileDir)
	if last.Judge != "" {
		t.Fatalf("a landing no judge scored names judge %q", last.Judge)
	}
	if strings.Join(last.Tried, ", ") != "other/flaky, other/steady" {
		t.Fatalf("the record tried %v, want both candidates in order", last.Tried)
	}
	if !strings.Contains(last.Reason, "the model answered with a 429") || strings.Contains(last.Reason, "\n") {
		t.Fatalf("the record's reason is %q, want the last error's one line", last.Reason)
	}
}

// A landing the hook declines before asking — nothing outside the crew to
// pick — is written too, with the decline itself as the reason.
func TestPoolJudgeHookRecordsTheDeclineWhenThereIsNoCandidate(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")

	profileDir := t.TempDir()
	settings := config.Config{}
	var asked []string
	crewOnly := func() []catalog.Model {
		return poolTestCatalog()[:2]
	}
	hook := poolJudgeHook(settings, profileDir, t.TempDir(), crewOnly, poolTestAsk(settings, &asked), time.Now, "task")
	hook(poolTestLanding())

	if len(asked) != 0 {
		t.Fatalf("a declined landing asked %v", asked)
	}
	last := decodeJudgeLast(t, profileDir)
	if last.Judge != "" || len(last.Tried) != 0 {
		t.Fatalf("a declined landing names judge %q tried %v", last.Judge, last.Tried)
	}
	if last.Reason != "no judge: every candidate is in the crew, unpriced, free, or below the floor" {
		t.Fatalf("the record's reason is %q, want the decline's own sentence", last.Reason)
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

// poolTwoJudgeCatalog is poolTestCatalog plus a second judge dearer than the
// first, so a landing has a candidate to move to when the cheapest answers
// nothing at all.
func poolTwoJudgeCatalog() []catalog.Model {
	return []catalog.Model{
		{ID: "crew/worker", PromptPrice: 1, CompletionPrice: 2, CodingIndex: 80, Parameters: []string{"tools"}},
		{ID: "crew/high", PromptPrice: 1, CompletionPrice: 2, CodingIndex: 80, Parameters: []string{"tools"}},
		{ID: "other/flaky", PromptPrice: 0.4, CompletionPrice: 0.4, CodingIndex: 90, Parameters: []string{"tools"}},
		{ID: "other/steady", PromptPrice: 0.6, CompletionPrice: 0.6, CodingIndex: 90, Parameters: []string{"tools"}},
	}
}

// poolTwoJudgeAsk answers as poolTestAsk does, except for the cheapest judge,
// which answers every question with an error and no score — the rate-limited
// row's own failure, and the one a landing must move past.
func poolTwoJudgeAsk(settings config.Config, asked *[]string) func(model string) judge.Ask {
	receipt := 0.0002
	return func(model string) judge.Ask {
		return func(context.Context, string, string) (string, error) {
			*asked = append(*asked, model)
			if model == "other/flaky" {
				return "", errors.New("the model answered with a 429")
			}
			recordPoolUsage(settings, model, &ai.Usage{PromptTokens: 10, CompletionTokens: 20, Cost: &receipt})
			return `{"score": 88, "reason": "the delivered work does what the brief asked"}`, nil
		}
	}
}

// decodeOutboxRows reads a row-per-line outbox back into the payloads it
// carries, skipping the marker lines that retire rows.
func decodeOutboxRows(t *testing.T, data []byte) []record.Row {
	t.Helper()
	var rows []record.Row
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var envelope struct {
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			t.Fatalf("an outbox envelope: %v", err)
		}
		if len(envelope.Payload) == 0 {
			continue
		}
		var row record.Row
		if err := json.Unmarshal(envelope.Payload, &row); err != nil {
			t.Fatalf("an outbox payload: %v", err)
		}
		rows = append(rows, row)
	}
	return rows
}

// TestPoolJudgeHookMovesToTheNextCandidateWhenTheCheapestAnswersNothing is the
// brief's rule: a judge that answers no seat at all — a 429, a timeout, a
// refusal — is replaced by the next candidate, and the scores that land are
// recorded under the judge that actually answered, not the first one asked.
func TestPoolJudgeHookMovesToTheNextCandidateWhenTheCheapestAnswersNothing(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")

	profileDir := t.TempDir()
	settings := config.Config{}
	var asked []string
	hook := poolJudgeHook(settings, profileDir, t.TempDir(), poolTwoJudgeCatalog, poolTwoJudgeAsk(settings, &asked), time.Now, "task")
	if hook == nil {
		t.Fatal("a pool whose mode allows reading built no hook")
	}
	hook(poolTestLanding())

	if len(asked) == 0 || asked[0] != "other/flaky" {
		t.Fatalf("the judge asked %v, want the cheapest candidate asked first", asked)
	}
	steady := 0
	for _, model := range asked {
		if model == "other/steady" {
			steady++
		}
	}
	if steady != 2 {
		t.Fatalf("the second judge was asked %d questions, want one per held seat", steady)
	}

	poolsheet, err := record.LoadSheet(record.OwnSheetPath(config.ProfilePath(profileDir, "pool")))
	if err != nil {
		t.Fatalf("the own sheet: %v", err)
	}
	cells := record.Cells(poolsheet)
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

	data, err := os.ReadFile(filepath.Join(config.ProfilePath(profileDir, "pool"), "outbox.jsonl"))
	if err != nil {
		t.Fatalf("the outbox: %v", err)
	}
	rows := decodeOutboxRows(t, data)
	if len(rows) != 2 {
		t.Fatalf("the outbox holds %d rows, want one per held seat", len(rows))
	}
	for _, row := range rows {
		if row.Judge != "other/steady" {
			t.Fatalf("a row names judge %q, want other/steady — never the candidate that answered nothing", row.Judge)
		}
	}

	session.FlushUsage()
	usage, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatalf("the usage ledger: %v", err)
	}
	if len(usage) != 2 {
		t.Fatalf("the ledger holds %d rows, want one per seat question the answering judge took", len(usage))
	}
	for _, row := range usage {
		if row.Seat != session.SeatJudge || row.Model != "other/steady" {
			t.Fatalf("a call names seat %q model %q, want the answering judge's own seat", row.Seat, row.Model)
		}
	}
}

// TestPoolJudgeHookWritesNothingWhenEveryCandidateAnswersNothing is the other
// end: when no candidate returns a single score, the landing is left unrecorded
// and the hook returns without a panic.
func TestPoolJudgeHookWritesNothingWhenEveryCandidateAnswersNothing(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")

	profileDir := t.TempDir()
	settings := config.Config{}
	var asked []string
	ask := func(model string) judge.Ask {
		return func(context.Context, string, string) (string, error) {
			asked = append(asked, model)
			return "", errors.New("the model answered with a 429")
		}
	}
	hook := poolJudgeHook(settings, profileDir, t.TempDir(), poolTwoJudgeCatalog, ask, time.Now, "task")
	if hook == nil {
		t.Fatal("a pool whose mode allows reading built no hook")
	}
	hook(poolTestLanding())

	if len(asked) != 4 {
		t.Fatalf("the judge asked %d questions, want both candidates asked for both seats", len(asked))
	}
	if _, err := os.Stat(record.OwnSheetPath(config.ProfilePath(profileDir, "pool"))); !os.IsNotExist(err) {
		t.Fatal("a landing no judge could score wrote an own sheet")
	}
	if _, err := os.Stat(filepath.Join(config.ProfilePath(profileDir, "pool"), "outbox.jsonl")); !os.IsNotExist(err) {
		t.Fatal("a landing no judge could score wrote an outbox")
	}
	session.FlushUsage()
	usage, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatalf("the usage ledger: %v", err)
	}
	if len(usage) != 0 {
		t.Fatalf("a landing no judge could score billed %d rows", len(usage))
	}
}

// decodeSweepLast reads the sweep's own record of the run it made.
func decodeSweepLast(t *testing.T, profileDir string) sweepLast {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(config.ProfilePath(profileDir, "pool"), "sweep-last.json"))
	if err != nil {
		t.Fatalf("the sweep-last record: %v", err)
	}
	var last sweepLast
	if err := json.Unmarshal(data, &last); err != nil {
		t.Fatalf("the sweep-last record does not parse: %v", err)
	}
	return last
}

// pendingOutboxRows counts the rows still waiting in the outbox a sweep
// recorded into: the sweep's push is pinned to a machine that does not answer,
// so every row the sweep recorded is one still pending, and a row that left
// for the relay is one this count is short.
func pendingOutboxRows(t *testing.T, profileDir string) int {
	t.Helper()
	box, err := outbox.Open(outboxPath(config.ProfilePath(profileDir, "pool")))
	if err != nil {
		t.Fatalf("the outbox: %v", err)
	}
	defer box.Close()
	return len(box.Pending())
}

// TestPoolJudgeSweepJudgesThePendingRowsAndLeavesARecordOfItself walks two
// waiting rows through the real sweep and reads back the record it leaves: what
// it judged, what its budget left, and — the pending file claimed whole — that
// the file is gone and the record is none the less there.
func TestPoolJudgeSweepJudgesThePendingRowsAndLeavesARecordOfItself(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")

	profileDir := t.TempDir()
	settings := config.Config{APIKey: "test-key"}
	var asked []string
	first := poolTestLanding()
	first.ID, first.Attempt = 7, 1
	second := poolTestLanding()
	second.ID, second.Attempt = 9, 1
	if err := writePendingLanding(profileDir, "do", first); err != nil {
		t.Fatal(err)
	}
	if err := writePendingLanding(profileDir, "exec", second); err != nil {
		t.Fatal(err)
	}
	poolJudgeSweep(settings, profileDir, "", poolTestCatalog, poolTestAsk(settings, &asked), time.Now)

	if len(asked) != 4 {
		t.Fatalf("the sweep asked %d questions, want both rows' seats", len(asked))
	}
	last := decodeSweepLast(t, profileDir)
	if last.Judged != 2 || last.Left != 0 || last.Cut {
		t.Fatalf("the sweep's record says judged %d left %d cut %v, want both rows judged", last.Judged, last.Left, last.Cut)
	}
	if last.At.IsZero() {
		t.Fatal("the sweep's record carries no moment")
	}
	info, err := os.Stat(filepath.Join(config.ProfilePath(profileDir, "pool"), "sweep-last.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the sweep's record's mode is %v with err %v, want 0600", info, err)
	}
	if _, err := os.Stat(pendingPath(config.ProfilePath(profileDir, "pool"))); !os.IsNotExist(err) {
		t.Fatal("a completed sweep left the pending file behind")
	}
	// And nothing left for the relay: the push the landing ran asked the dead
	// address the test pinned, so the four rows the sweep recorded are still
	// in the outbox, waiting.
	if rows := pendingOutboxRows(t, profileDir); rows != 4 {
		t.Fatalf("the outbox holds %d rows after the sweep, want all four still pending — none sent", rows)
	}
}

// TestPoolJudgeSweepCutByItsBudgetLeavesTheRestAndSaysSo is the other end: the
// budget runs out after the first row and the second waits for the next start,
// and the record says judged one, left one, cut — the reading that keeps a cut
// sweep from looking like a whole one.
func TestPoolJudgeSweepCutByItsBudgetLeavesTheRestAndSaysSo(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")

	profileDir := t.TempDir()
	settings := config.Config{APIKey: "test-key"}
	var asked []string
	inner := poolTestAsk(settings, &asked)
	base := time.Now()
	late := false
	now := func() time.Time {
		if late {
			return base.Add(poolSweepBudget + time.Minute)
		}
		return base
	}
	// The first question judged is where the budget ends: from the moment the
	// ask answers, the sweep's clock reads past its own deadline, so the row
	// after the one being judged is the one the budget leaves.
	ask := func(model string) judge.Ask {
		one := inner(model)
		return func(ctx context.Context, system, user string) (string, error) {
			late = true
			return one(ctx, system, user)
		}
	}
	first := poolTestLanding()
	first.ID, first.Attempt = 7, 1
	second := poolTestLanding()
	second.ID, second.Attempt = 9, 1
	if err := writePendingLanding(profileDir, "do", first); err != nil {
		t.Fatal(err)
	}
	if err := writePendingLanding(profileDir, "exec", second); err != nil {
		t.Fatal(err)
	}
	poolJudgeSweep(settings, profileDir, "", poolTestCatalog, ask, now)

	if len(asked) != 2 {
		t.Fatalf("the sweep asked %d questions, want the first row's seats only", len(asked))
	}
	last := decodeSweepLast(t, profileDir)
	if last.Judged != 1 || last.Left != 1 || !last.Cut {
		t.Fatalf("a cut sweep's record says judged %d left %d cut %v, want the second row left", last.Judged, last.Left, last.Cut)
	}
	// And the first landing's two rows went nowhere: the push met the dead
	// address the test pinned, so both wait in the outbox for the next one.
	if rows := pendingOutboxRows(t, profileDir); rows != 2 {
		t.Fatalf("the outbox holds %d rows after the cut sweep, want both still pending — none sent", rows)
	}
}

// A pool whose mode forbids reading runs no sweep and leaves no record: the
// rows simply wait, and status says none yet until one runs.
func TestPoolJudgeSweepLeavesNoRecordWhenThePoolCannotRead(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "off")

	profileDir := t.TempDir()
	poolJudgeSweep(config.Config{APIKey: "test-key"}, profileDir, "", poolTestCatalog, poolTestAsk(config.Config{}, new([]string)), time.Now)
	if _, err := os.Stat(filepath.Join(config.ProfilePath(profileDir, "pool"), "sweep-last.json")); !os.IsNotExist(err) {
		t.Fatal("a pool whose mode forbids reading left a sweep record")
	}
}
