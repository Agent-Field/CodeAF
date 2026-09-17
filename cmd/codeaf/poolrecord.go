// The Model Pool's judge, wired to the chat door's landings.
//
// When a task lands, a model outside the crew scores each seat the work ran
// on. The scores go into the install's own sheet under the pool directory —
// the one the crew picker already reads beside the index — and, when the
// pool's mode allows sending, into the outbox beside it. The pure halves live
// in internal/pool/judge and internal/pool/record; this file owns the two
// things they cannot: the seam in the engine's landing (session.Config's
// TaskLanded) and the one provider call the judge's questions ride, billed to
// the judge's own seat.
//
// Quiet by design, for poolindex.go's reason: a landing that nobody could
// score is an ordinary state — a pool switched off, a catalog with nothing
// left to pick, a model that did not answer — and nothing the person is
// reading should move for it. Every error here is said only under the debug
// record's switch.
package main

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewpick"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/pool/judge"
	"github.com/Agent-Field/codeaf/internal/pool/outbox"
	"github.com/Agent-Field/codeaf/internal/pool/record"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// judgeTimeout bounds one landing's whole judging: every seat question rides
// the same context, so a slow model cannot hold one landing open forever. It
// is a bound on an errand nobody is waiting for, not on work the person asked
// for, and it is generous on purpose — the questions are one call each.
const judgeTimeout = 90 * time.Second

// poolJudgeHook builds the session's landing reader. Nil is off, for the pool
// index's reason: a mode that forbids reading runs no judge and writes
// nothing, and the door then hands the engine no hook at all.
func poolJudgeHook(settings config.Config, profileDir, workspace string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time) func(session.TaskLanding) {
	if !config.ModelPoolAt(profileDir).CanRead() {
		return nil
	}
	return func(landing session.TaskLanding) {
		defer guard.Recover("pool/judge")
		poolJudgeLanding(settings, profileDir, models, ask, now, landing)
	}
}

// poolJudgeLanding scores one landed task and records what came back. The
// order is the sheet's own law: every score is observed before the rows are
// appended, the sheet is saved once, and the picker's own-cells seam is
// repointed at the new cells so the very next pick in this process reads them.
func poolJudgeLanding(settings config.Config, profileDir string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time, landing session.TaskLanding) {
	pool := config.ModelPoolAt(profileDir)
	poolDir := config.ProfilePath(profileDir, "pool")
	seats := map[judge.Role]string{
		judge.RoleWorker: landing.Worker,
	}
	if landing.High != "" {
		seats[judge.RoleHigh] = landing.High
	}
	judgeID, ok := judge.Pick(models(), seats, judge.DefaultFloor)
	if !ok {
		if trace.Enabled() {
			log.Printf("model pool: no judge to ask about task %d", landing.ID)
		}
		return
	}
	rec := judge.Record{
		Brief:       landing.Brief,
		Deliverable: landing.Deliverable,
		Report:      landing.Report,
		Claim:       landing.Claim,
		Ending:      landing.Ending,
		Files:       landing.Wrote,
		Changed:     landing.Changed,
		Checks:      landing.Checks,
		Seats:       seats,
		Judge:       judgeID,
		CostUSD:     landing.CostUSD,
	}
	// A landing is news, not a turn: nobody is waiting on the answer, and the
	// one thing this context owes anybody is a bound on how long it holds the
	// pool's own goroutine.
	ctx, cancel := context.WithTimeout(context.Background(), judgeTimeout)
	defer cancel()
	scores, err := judge.Judge(ctx, ask(judgeID), rec)
	// The scores obtained are recorded even when the error names a seat: a
	// judge that scored the worker but not the high seat scored the worker,
	// and a seat the call failed on is the judge's evidence of that model too.
	if err != nil && trace.Enabled() {
		log.Printf("model pool: judging task %d: %v", landing.ID, err)
	}
	if len(scores) == 0 {
		return
	}
	sheet, err := record.LoadSheet(record.OwnSheetPath(poolDir))
	if err != nil {
		if trace.Enabled() {
			log.Printf("model pool: own sheet: %v", err)
		}
		return
	}
	recorder := &record.Recorder{Sheet: sheet}
	if pool.CanSend() {
		ob, err := outbox.Open(outboxPath(poolDir))
		if err != nil {
			if trace.Enabled() {
				log.Printf("model pool: outbox: %v", err)
			}
		} else {
			recorder.Outbox = ob
			defer ob.Close()
		}
	}
	day := now().UTC().Format("2006-01-02")
	if err := recorder.Record(scores, judgeID, "task", poolSize(landing.Tokens), day); err != nil && trace.Enabled() {
		log.Printf("model pool: record: %v", err)
	}
	if err := record.SaveSheet(record.OwnSheetPath(poolDir), sheet); err != nil {
		if trace.Enabled() {
			log.Printf("model pool: own sheet: %v", err)
		}
		return
	}
	// The next pick in this process reads the new cells at once, the same way
	// the picker reads them at start-up (poolindex.go's poolOwnCellsFor): a
	// closing one is the install's own evidence and is never held to the
	// index's min_installs.
	cells := record.Cells(sheet)
	config.AutoOwnCells = func() []crewpick.Cell { return cells }
}

// poolSize is the day-row bucket a landing's token count answers: S under
// 200k tokens, M under a million, L past it. The size says how much work the
// bill was carrying, which is the one thing a score alone does not say.
func poolSize(tokens int) string {
	switch {
	case tokens < 200_000:
		return "S"
	case tokens < 1_000_000:
		return "M"
	default:
		return "L"
	}
}

// outboxPath is the one path an install's measurements leave by, beside its
// own sheet under the pool directory.
func outboxPath(poolDir string) string {
	return filepath.Join(poolDir, "outbox.jsonl")
}

// poolJudgeAsk builds the maker of one judge's ask: a client per call, built
// from the model's own config, the answer read as the judge's plain string,
// and the call billed to the judge's own seat.
func poolJudgeAsk(settings config.Config, profileDir string) func(model string) judge.Ask {
	return func(model string) judge.Ask {
		return func(ctx context.Context, system, user string) (string, error) {
			client, err := provider.NewClient(settings.ClientConfig(model))
			if err != nil {
				return "", err
			}
			// The call says who it is for the way every errand does (chat.go's
			// errandContext): the role names the seat the bill lands on, the tag
			// names the rows in the model-call log. Streaming buys nothing — the
			// answer is one object nobody is reading as it arrives.
			ctx = provider.WithCallTag(provider.WithRole(settings.Context(ctx, "pool-judge"), lane.RoleJudge), "judge")
			ctx = provider.WithoutStream(ctx)
			messages := []ai.Message{
				judgeMessage("system", system),
				judgeMessage("user", user),
			}
			response, err := client.CompleteWithMessages(ctx, messages)
			if err != nil {
				return "", err
			}
			if response == nil || strings.TrimSpace(response.Text()) == "" {
				return "", errors.New("the model answered nothing")
			}
			recordPoolUsage(settings, model, response.Usage)
			return response.Text(), nil
		}
	}
}

// judgeMessage is one plain-text part in one role, the shape every
// single-question errand here builds its pair out of.
func judgeMessage(role, text string) ai.Message {
	return ai.Message{Role: role, Content: []ai.ContentPart{{Type: "text", Text: text}}}
}

// recordPoolUsage appends the call's one row to this machine's usage ledger.
// THE SEAT IS THE JUDGE'S, because the call was not a conversation's turn and
// no crew seat asked for it. The dollars are the provider's own receipt when
// one arrived, otherwise the model's published price over the tokens it
// counted — and zero when nobody published one, which reads as "nobody said",
// the same reading every other row's zero has.
func recordPoolUsage(settings config.Config, model string, used *ai.Usage) {
	line := session.UsageLine{
		Seat:  session.SeatJudge,
		Model: model,
		Calls: 1,
	}
	if used != nil {
		line.Input, line.Output = used.PromptTokens, used.CompletionTokens
		if used.Cost != nil {
			line.USD = *used.Cost
		} else if prompt, completion, known := settings.ClientConfig(model).ModelPrice(model); known {
			line.USD = float64(used.PromptTokens)*prompt + float64(used.CompletionTokens)*completion
		}
	}
	session.RecordUsage(session.UsageLedgerPath(), line)
}
