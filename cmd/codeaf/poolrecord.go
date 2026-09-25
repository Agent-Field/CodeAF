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
	"fmt"
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
	"github.com/Agent-Field/codeaf/internal/roles"
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
func poolJudgeHook(settings config.Config, profileDir, workspace string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time, door string) func(session.TaskLanding) {
	if !config.ModelPoolAt(profileDir).CanRead() {
		return nil
	}
	return func(landing session.TaskLanding) {
		defer guard.Recover("pool/judge")
		poolJudgeLanding(settings, profileDir, models, ask, now, door, landing)
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

// poolSeatID is the spelling of a seat's model once it enters the pool's
// records: the thinking level and the leading `~` alias marker come off — the
// same two things internal/catalog's normaliser takes off before a lookup,
// mirrored here because that one (normalizeID) is unexported. The pool's rows
// and the relay's schema name a model by its bare `<vendor>/<id>`; the marker
// is this client's own routing and rides only the paths that call the
// provider, never the pool's copy of the id.
func poolSeatID(seat string) string {
	model, _ := roles.SplitEffort(seat)
	return strings.TrimPrefix(model, "~")
}

// poolJudgeLanding scores one landed task and records what came back. The
// order is the sheet's own law: every score is observed before the rows are
// appended, the sheet is saved once, and the picker's own-cells seam is
// repointed at the new cells so the very next pick in this process reads them.
func poolJudgeLanding(settings config.Config, profileDir string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time, door string, landing session.TaskLanding) {
	poolJudgeLandingContext(context.Background(), settings, profileDir, models, ask, now, door, landing)
}

func poolJudgeLandingContext(ctx context.Context, settings config.Config, profileDir string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time, door string, landing session.TaskLanding) {
	pool := config.ModelPoolAt(profileDir)
	poolDir := config.ProfilePath(profileDir, "pool")
	// A seat's id enters the pool spelled bare (poolSeatID): the sheet, the
	// outbox and the relay read the bare `<vendor>/<id>`, and the same-vendor
	// exclusion below must see the vendor the marker rides on.
	seats := map[judge.Role]string{
		judge.RoleWorker: poolSeatID(landing.Worker),
	}
	if landing.High != "" {
		seats[judge.RoleHigh] = poolSeatID(landing.High)
	}
	candidates := judge.Candidates(models(), seats, judge.DefaultFloor)
	// The seats are said in the crew's own order — the worker, then the high
	// seat when the run held one — so the record reads the way the crew ran.
	held := []string{poolSeatID(landing.Worker)}
	if landing.High != "" {
		held = append(held, poolSeatID(landing.High))
	}
	if len(candidates) == 0 {
		if trace.Enabled() {
			log.Printf("model pool: no judge to ask about task %d", landing.ID)
		}
		writeJudgeLast(poolDir, judgeLast{At: now(), Task: landing.ID, Seats: held, Reason: errNoJudgeCandidate})
		markJudged(poolDir, landing.ID, landing.Attempt)
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
		ctx, cancel := context.WithTimeout(ctx, judgeTimeout*time.Duration(len(seats)+1))
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
		// A close cancelled the sweep mid-judge: every candidate failed on the
		// parent context, not on the run, so this is not a verdict. Leave the
		// landing unjudged and unrecorded so the next start judges it, rather than
		// burning it with a permanent marker. ctx here is the parent, the
		// per-candidate context inside the loop above is out of scope.
		if ctx.Err() != nil {
			return
		}
		reason := "no judge answered"
		if err != nil {
			reason = oneLine(err.Error())
		}
		writeJudgeLast(poolDir, judgeLast{At: now(), Task: landing.ID, Tried: candidates, Seats: held, Reason: reason})
		if trace.Enabled() {
			log.Printf("model pool: no judge scored task %d", landing.ID)
		}
		markJudged(poolDir, landing.ID, landing.Attempt)
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
	if err := recorder.Record(scores, judgeID, door, poolSize(landing.Tokens), day); err != nil && trace.Enabled() {
		log.Printf("model pool: record: %v", err)
	}
	if err := record.SaveSheet(record.OwnSheetPath(poolDir), sheet); err != nil {
		if trace.Enabled() {
			log.Printf("model pool: own sheet: %v", err)
		}
		return
	}
	// The run is judged and its scores are saved: mark it so the restart sweep
	// never judges it again. The live hook writes this but does not read it, so a
	// resettle still re-judges; only the sweep reads it.
	markJudged(poolDir, landing.ID, landing.Attempt)
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

// poolSweepBudget bounds the whole restart-time sweep, checked between landings,
// so a backlog of unjudged runs cannot hold the sweep's own goroutine open past a
// few landings' worth of the per-landing bound. It is generous because the sweep
// runs on its own goroutine and nobody waits on it.
const poolSweepBudget = 10 * time.Minute

// pendingLanding is one row of the pool's pending file: a landing a door recorded
// for the restart sweep to judge, under the door it ran on. At is the moment the
// row was written — the only when a row has, and what status ages it by; rows
// written before the stamp existed read as zero and say an unknown age rather
// than inventing one. The optional fields are forward room for a later grader (an
// acceptable/source verdict, a role->model map, a lease propensity); this build
// writes only At, Door and Landing, and the sweep ignores fields it does not know
// rather than refusing a row.
type pendingLanding struct {
	At         time.Time           `json:"at,omitempty"`
	Door       string              `json:"door"`
	Landing    session.TaskLanding `json:"landing"`
	ByModel    map[string]string   `json:"by_model,omitempty"`
	Propensity *float64            `json:"propensity,omitempty"`
	Acceptable *bool               `json:"acceptable,omitempty"`
	Source     string              `json:"source,omitempty"`
}

func pendingPath(poolDir string) string { return filepath.Join(poolDir, "pending.jsonl") }

// writePendingLanding appends one landing to the pool's pending file for the
// restart sweep to judge later, under the door it ran on. It is the seam the
// headless doors (do, exec, run) use: they have no session graph and so no live
// landing hook, and the chat door judges live and does not write here. The write
// is ONE O_APPEND of one line, so concurrent doors sharing a profile never tear
// each other's rows, and the row is stamped with the moment it was written,
// which is the only when a row has and what status ages it by.
func writePendingLanding(profileDir, door string, landing session.TaskLanding) error {
	poolDir := config.ProfilePath(profileDir, "pool")
	if err := os.MkdirAll(poolDir, 0o700); err != nil {
		return err
	}
	// The row is what the restart sweep scores from, so the seats it carries
	// are written spelled bare (poolSeatID), not in the door's routing spelling.
	landing.Worker = poolSeatID(landing.Worker)
	landing.High = poolSeatID(landing.High)
	data, err := json.Marshal(pendingLanding{At: time.Now(), Door: door, Landing: landing})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(pendingPath(poolDir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// The judged markers record, per run, that a landing was judged, so the restart
// sweep never judges the same run twice. The key is id AND attempt because a
// re-audit (resettle) re-judges the same node id and a re-run lands a new attempt
// of it — a per-id-only marker would swallow either.
func judgedDir(poolDir string) string { return filepath.Join(poolDir, "judged") }

func judgedMarkerPath(poolDir string, id uint64, attempt int) string {
	return filepath.Join(judgedDir(poolDir), fmt.Sprintf("%d-%d", id, attempt))
}

func alreadyJudged(poolDir string, id uint64, attempt int) bool {
	_, err := os.Stat(judgedMarkerPath(poolDir, id, attempt))
	return err == nil
}

func markJudged(poolDir string, id uint64, attempt int) {
	defer guard.Recover("pool/judged-mark")
	if err := os.MkdirAll(judgedDir(poolDir), 0o700); err != nil {
		if trace.Enabled() {
			log.Printf("model pool: judged marker: %v", err)
		}
		return
	}
	if err := os.WriteFile(judgedMarkerPath(poolDir, id, attempt), []byte{}, 0o600); err != nil && trace.Enabled() {
		log.Printf("model pool: judged marker: %v", err)
	}
}

// sweepLast is the small record the restart sweep leaves at its end, for the
// one reading that would otherwise need --debug: what it judged, what its
// budget left waiting, and whether the deadline ended it with rows left. Cut
// is Left's own word — rows only wait past a sweep the deadline cut short —
// and BudgetUsed is the seconds the sweep spent of its own poolSweepBudget.
type sweepLast struct {
	At         time.Time `json:"at"`
	Judged     int       `json:"judged"`
	Left       int       `json:"left"`
	BudgetUsed int       `json:"budget_used"`
	Cut        bool      `json:"cut"`
}

// writeSweepLast leaves the record under the pool directory, mode 0600 like
// the records beside it. Its errors are debug-only, for the sweep's own
// reason: a record that could not be written must not matter to a sweep
// nobody is waiting on, and nobody reading the chat surface moves for it.
func writeSweepLast(poolDir string, last sweepLast) {
	defer guard.Recover("pool/sweep-last")
	data, err := json.Marshal(last)
	if err != nil {
		if trace.Enabled() {
			log.Printf("model pool: sweep-last: %v", err)
		}
		return
	}
	if err := os.MkdirAll(poolDir, 0o700); err != nil {
		if trace.Enabled() {
			log.Printf("model pool: sweep-last: %v", err)
		}
		return
	}
	if err := os.WriteFile(filepath.Join(poolDir, "sweep-last.json"), data, 0o600); err != nil && trace.Enabled() {
		log.Printf("model pool: sweep-last: %v", err)
	}
}

// poolJudgeSweep judges, at chat start, every landed run that never was: the
// headless doors' pending rows, and the resumed session's own final-state nodes
// that a process death left unjudged. It runs on its own goroutine, so the
// session's checkpoint already reflects whatever recovery made of it and a node
// that will run again is no longer in a final state. It is a no-op when the pool
// cannot read or no judge-capable key is present — with no key the rows simply
// wait. The whole sweep is bounded by poolSweepBudget, checked between landings,
// and it leaves one record of itself at the end — what it judged, what the
// budget left — for `pool status` to read.
func poolJudgeSweep(settings config.Config, profileDir, tasksPath string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time) {
	poolJudgeSweepContext(context.Background(), settings, profileDir, tasksPath, models, ask, now)
}

var poolJudgeSweepRun = poolJudgeSweepContext

func poolJudgeSweepContext(ctx context.Context, settings config.Config, profileDir, tasksPath string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time) {
	defer guard.Recover("pool/judge-sweep")
	if !config.ModelPoolAt(profileDir).CanRead() {
		return
	}
	if strings.TrimSpace(settings.APIKey) == "" {
		return
	}
	poolDir := config.ProfilePath(profileDir, "pool")
	deadline := now().Add(poolSweepBudget)
	started := now()
	judged, left := sweepPendingContext(ctx, settings, profileDir, poolDir, models, ask, now, deadline)
	if strings.TrimSpace(tasksPath) != "" {
		landed, err := session.LoadLandedForJudge(tasksPath)
		if err != nil {
			if trace.Enabled() {
				log.Printf("model pool: sweep load: %v", err)
			}
		} else {
			for i, landing := range landed {
				if ctx.Err() != nil {
					return
				}
				if !now().Before(deadline) {
					left += unjudgedLandings(poolDir, landed[i:])
					break
				}
				if alreadyJudged(poolDir, landing.ID, landing.Attempt) {
					continue
				}
				poolJudgeLandingContext(ctx, settings, profileDir, models, ask, now, "task", landing)
				judged++
			}
		}
	}
	// The sweep's one record of itself, at its end: what it judged, what its
	// budget left waiting, and whether the deadline ended it with rows left.
	// Cut is left's own word — the only way rows wait past a sweep is the
	// deadline ending it with rows behind the check.
	writeSweepLast(poolDir, sweepLast{
		At:         now(),
		Judged:     judged,
		Left:       left,
		BudgetUsed: int(now().Sub(started) / time.Second),
		Cut:        left > 0,
	})
}

// unjudgedLandings counts the resumed session's own landings the sweep did not
// reach: those behind a deadline cut, a judged marker excepted.
func unjudgedLandings(poolDir string, landed []session.TaskLanding) int {
	left := 0
	for _, landing := range landed {
		if !alreadyJudged(poolDir, landing.ID, landing.Attempt) {
			left++
		}
	}
	return left
}

// sweepPending claims the pending file with an atomic rename so concurrent doors
// keep appending to a fresh one, then judges each row it claimed under that row's
// own door. A leftover claim from a sweep a process death cut short is taken
// first. It answers what the claims held: the landings judged and the rows the
// deadline left waiting.
func sweepPending(settings config.Config, profileDir, poolDir string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time, deadline time.Time) (judged, left int) {
	return sweepPendingContext(context.Background(), settings, profileDir, poolDir, models, ask, now, deadline)
}

func sweepPendingContext(ctx context.Context, settings config.Config, profileDir, poolDir string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time, deadline time.Time) (judged, left int) {
	claim := pendingPath(poolDir) + ".sweeping"
	judged, left = sweepClaimContext(ctx, settings, profileDir, poolDir, claim, models, ask, now, deadline)
	// A CLAIM THAT IS STILL THERE WAS NOT FINISHED, and the fresh file must not be
	// renamed over it. The sweep removes a claim only when it reached every row;
	// one a cancel or the deadline cut short keeps its unjudged rows for the next
	// start, and renaming the pending file onto the same name replaced those rows
	// with the new ones and lost them for good (#1267). The pending file waits
	// where it is, and the next start takes the leftover first, as this one did.
	// Its rows are waiting too, so they are counted among the ones left.
	if _, err := os.Lstat(claim); err == nil {
		if data, err := os.ReadFile(pendingPath(poolDir)); err == nil {
			left += countUnjudged(poolDir, strings.Split(string(data), "\n"))
		}
		return judged, left
	}
	if err := os.Rename(pendingPath(poolDir), claim); err != nil {
		return judged, left
	}
	moreJudged, moreLeft := sweepClaimContext(ctx, settings, profileDir, poolDir, claim, models, ask, now, deadline)
	return judged + moreJudged, left + moreLeft
}

// sweepClaim judges the rows of one claimed batch. A torn last line (a row
// half-written when the rename landed) is skipped, not fatal; unknown future
// fields are ignored; a row already judged is skipped. The claim is removed only
// when every row was reached, so a deadline cut leaves the rest for the next
// start, where the markers keep the already-judged rows from being scored twice.
// It answers what it judged and how many unjudged rows the deadline left behind
// it — the sweep's record needs both.
func sweepClaim(settings config.Config, profileDir, poolDir, claim string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time, deadline time.Time) (judged, left int) {
	return sweepClaimContext(context.Background(), settings, profileDir, poolDir, claim, models, ask, now, deadline)
}

func sweepClaimContext(ctx context.Context, settings config.Config, profileDir, poolDir, claim string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time, deadline time.Time) (judged, left int) {
	data, err := os.ReadFile(claim)
	if err != nil {
		return 0, 0
	}
	lines := strings.Split(string(data), "\n")
	completed := true
	for i, line := range lines {
		if ctx.Err() != nil {
			completed = false
			left = countUnjudged(poolDir, lines[i:])
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !now().Before(deadline) {
			completed = false
			left = countUnjudged(poolDir, lines[i:])
			break
		}
		var row pendingLanding
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		door := row.Door
		if door == "" {
			door = "task"
		}
		if alreadyJudged(poolDir, row.Landing.ID, row.Landing.Attempt) {
			continue
		}
		poolJudgeLandingContext(ctx, settings, profileDir, models, ask, now, door, row.Landing)
		if ctx.Err() != nil {
			// Cancelled mid-judge: poolJudgeLandingContext left this landing
			// unjudged, so do not count it and stop, leaving the claim for the next
			// start rather than removing it as a completed sweep would.
			completed = false
			left = countUnjudged(poolDir, lines[i:])
			break
		}
		judged++
	}
	if completed {
		_ = os.Remove(claim)
	}
	return judged, left
}

// countUnjudged counts the rows of a claimed batch the sweep did not reach:
// the lines behind a deadline cut, parsed the way the sweep reads them — a torn
// line counted as nothing, a row already judged as not waiting.
func countUnjudged(poolDir string, lines []string) int {
	left := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row pendingLanding
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		if alreadyJudged(poolDir, row.Landing.ID, row.Landing.Attempt) {
			continue
		}
		left++
	}
	return left
}
