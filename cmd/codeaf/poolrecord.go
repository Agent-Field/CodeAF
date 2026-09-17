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
	"encoding/json"
	"errors"
	"log"
	"os"
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

// judgeTimeout bounds ONE seat's question, and the landing as a whole gets one
// share per seat and one to spare, so a slow model cannot hold a landing open
// forever and a slow first answer does not eat the second question's time: a
// reasoning judge took 64 s over the worker and the checker's question was
// then cut at the landing's 90 s, and the checker seat went unscored. It is a
// bound on an errand nobody is waiting for, not on work the person asked for,
// and it is generous on purpose — the questions are one call each.
const judgeTimeout = 90 * time.Second

// judgeTries bounds how many candidates one landing is put through. A judge
// that answers no seat at all — every question 429ed, refused or timed out — is
// the judge's own failure rather than a verdict on the run, so the next
// candidate is asked; the cap is what keeps a landing no judge will answer from
// holding the pool's goroutine past a few candidates' worth of the bound above.
const judgeTries = 3

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

// judgeLast is the small record the hook leaves after every landing it
// handles, for the one reading that would otherwise need --debug: what the
// last judge did, and what became of it. Judge names the model that answered
// and Scored the seats it scored on a success; on a failure Judge is empty,
// Tried the candidates in the order they were asked and Reason the last
// error's one line. A landing the hook declines before asking carries the
// decline itself as the reason.
type judgeLast struct {
	At     time.Time `json:"at"`
	Task   uint64    `json:"task"`
	Judge  string    `json:"judge"`
	Tried  []string  `json:"tried"`
	Seats  []string  `json:"seats"`
	Scored []string  `json:"scored"`
	Reason string    `json:"reason"`
}

// errNoJudgeCandidate is the decline a landing with nothing to ask says: the
// one sentence a person reads when the record holds no candidates at all.
const errNoJudgeCandidate = "no judge: every candidate is in the crew, unpriced, free, or below the floor"

// writeJudgeLast leaves the record under the pool directory, mode 0600 like
// the sheet beside it. It never panics and its errors are debug-only, for the
// hook's own reason: a record that could not be written must not cost a
// landing its scores, and nobody reading the chat surface moves for it.
func writeJudgeLast(poolDir string, last judgeLast) {
	defer guard.Recover("pool/judge-last")
	data, err := json.Marshal(last)
	if err != nil {
		if trace.Enabled() {
			log.Printf("model pool: judge-last: %v", err)
		}
		return
	}
	if err := os.MkdirAll(poolDir, 0o700); err != nil {
		if trace.Enabled() {
			log.Printf("model pool: judge-last: %v", err)
		}
		return
	}
	if err := os.WriteFile(filepath.Join(poolDir, "judge-last.json"), data, 0o600); err != nil && trace.Enabled() {
		log.Printf("model pool: judge-last: %v", err)
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
	candidates := judge.Candidates(models(), seats, judge.DefaultFloor)
	// The seats are said in the crew's own order — the worker, then the high
	// seat when the run held one — so the record reads the way the crew ran.
	held := []string{landing.Worker}
	if landing.High != "" {
		held = append(held, landing.High)
	}
	if len(candidates) == 0 {
		if trace.Enabled() {
			log.Printf("model pool: no judge to ask about task %d", landing.ID)
		}
		writeJudgeLast(poolDir, judgeLast{At: now(), Task: landing.ID, Seats: held, Reason: errNoJudgeCandidate})
		return
	}
	if len(candidates) > judgeTries {
		candidates = candidates[:judgeTries]
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
		CostUSD:     landing.CostUSD,
	}
	// A landing is news, not a turn: nobody is waiting on the answer, and the
	// one thing this context owes anybody is a bound on how long it holds the
	// pool's own goroutine. Each candidate gets that whole bound afresh, so the
	// first judge's share is spent by the first judge and the next starts with a
	// full one rather than the first's leftovers.
	var judgeID string
	var scores []judge.Score
	var err error
	for _, candidate := range candidates {
		rec.Judge = candidate
		ctx, cancel := context.WithTimeout(context.Background(), judgeTimeout*time.Duration(len(seats)+1))
		one := ask(candidate)
		perSeat := func(ctx context.Context, system, user string) (string, error) {
			qctx, cancel := context.WithTimeout(ctx, judgeTimeout)
			defer cancel()
			return one(qctx, system, user)
		}
		scores, err = judge.Judge(ctx, perSeat, rec)
		cancel()
		if len(scores) > 0 {
			judgeID = candidate
			break
		}
		// No seat at all came back: that is the judge's own failure — a 429, a
		// timeout, a refusal — and not a verdict on the run, so its reason is
		// said here and the next candidate is asked.
		if trace.Enabled() {
			log.Printf("model pool: judge %s scored no seat of task %d: %v", candidate, landing.ID, err)
		}
	}
	// What the judge did is said before anything else is recorded, so the
	// record stands even when the sheet or the outbox refuses it: a failure
	// names the candidates in the order they were asked and the last error's
	// one line; a success names the judge that answered and the seats it
	// scored, with no reason to say.
	if len(scores) == 0 {
		reason := "no judge answered"
		if err != nil {
			reason = oneLine(err.Error())
		}
		writeJudgeLast(poolDir, judgeLast{At: now(), Task: landing.ID, Tried: candidates, Seats: held, Reason: reason})
		if trace.Enabled() {
			log.Printf("model pool: no judge scored task %d", landing.ID)
		}
		return
	}
	scored := make([]string, 0, len(scores))
	for _, seat := range scores {
		scored = append(scored, seat.Model)
	}
	writeJudgeLast(poolDir, judgeLast{At: now(), Task: landing.ID, Judge: judgeID, Seats: held, Scored: scored})
	// The scores obtained are recorded even when the error names a seat: a
	// judge that scored the worker but not the high seat scored the worker,
	// and a seat the call failed on is the judge's evidence of that model too.
	if err != nil && trace.Enabled() {
		log.Printf("model pool: judging task %d: %v", landing.ID, err)
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
	// The sheet is saved, so what the recorder appended is the install's own
	// evidence now; the copies waiting in the outbox leave for the relay
	// here, on this hook's own goroutine (the session runs TaskLanded on
	// one), under the push's own bound.
	pushCtx, cancelPush := context.WithTimeout(context.Background(), poolPushBudget)
	defer cancelPush()
	poolPush(pushCtx, profileDir, pool, poolPushBudget)
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
