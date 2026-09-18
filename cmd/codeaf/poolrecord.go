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
	"github.com/Agent-Field/codeaf/internal/pool/grade"
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
func poolJudgeHook(settings config.Config, profileDir, workspace string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time, door string) func(session.TaskLanding) {
	if !config.ModelPoolAt(profileDir).CanRead() {
		return nil
	}
	return func(landing session.TaskLanding) {
		defer guard.Recover("pool/judge")
		poolJudgeLanding(settings, profileDir, models, ask, now, door, workspace, landing)
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
func poolJudgeLanding(settings config.Config, profileDir string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time, door, workspace string, landing session.TaskLanding) {
	pool := config.ModelPoolAt(profileDir)
	poolDir := config.ProfilePath(profileDir, "pool")
	seats := map[judge.Role]string{
		judge.RoleWorker: landing.Worker,
	}
	if landing.High != "" {
		seats[judge.RoleHigh] = landing.High
	}
	// The seats are said in the crew's own order — the worker, then the high
	// seat when the run held one — so the record reads the way the crew ran.
	held := []string{landing.Worker}
	if landing.High != "" {
		held = append(held, landing.High)
	}
	day := now().UTC().Format("2006-01-02")
	size := poolSize(landing.Tokens)

	// THE GRADE COMES FIRST AND ASKS NO MODEL. It is the harness's own
	// reading of what the task left behind — the tree formats, builds, vets
	// and passes the tests of the packages the task touched — and it is the
	// one observation the picker learns from; the judge below is a model's
	// opinion, kept as its own evidence. A workspace the grader cannot read
	// (none given, gone from disk, no module at its root) grades nothing, and
	// nothing is recorded for it rather than a grade that means nothing.
	graded, gradedOK := poolGradeLanding(poolDir, workspace, now, landing)

	candidates := judge.Candidates(models(), seats, judge.DefaultFloor)
	var judgeID string
	var scores []judge.Score
	if len(candidates) == 0 {
		if trace.Enabled() {
			log.Printf("model pool: no judge to ask about task %d", landing.ID)
		}
		writeJudgeLast(poolDir, judgeLast{At: now(), Task: landing.ID, Seats: held, Reason: errNoJudgeCandidate})
	} else {
		if len(candidates) > judgeTries {
			candidates = candidates[:judgeTries]
		}
		judgeID, scores = poolAskJudges(poolDir, candidates, seats, held, ask, now, landing)
	}
	// Nothing to record — no grade and no score — is a terminal outcome all
	// the same: the run is marked so the restart sweep never judges it again.
	// The live hook writes the mark but does not read it, so a resettle still
	// re-judges; only the sweep reads it. A sheet the process could not load
	// or save is the one outcome that is NOT marked, because it is not the
	// run's outcome but the box's, and the sweep should try again.
	if !gradedOK && len(scores) == 0 {
		markJudged(poolDir, landing.ID, landing.Attempt)
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
	if gradedOK {
		if err := recorder.RecordGrade(seats, graded.Source, graded.Pass, door, size, day); err != nil && trace.Enabled() {
			log.Printf("model pool: record grade: %v", err)
		}
	}
	if len(scores) > 0 {
		if err := recorder.Record(scores, judgeID, door, size, day); err != nil && trace.Enabled() {
			log.Printf("model pool: record: %v", err)
		}
	}
	if err := record.SaveSheet(record.OwnSheetPath(poolDir), sheet); err != nil {
		if trace.Enabled() {
			log.Printf("model pool: own sheet: %v", err)
		}
		return
	}
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

// poolAskJudges puts the landing through the candidates in order until one
// scores a seat, and leaves the judge-last record either way. It answers the
// judge that scored and its scores, or an empty id and no scores when none
// did.
func poolAskJudges(poolDir string, candidates []string, seats map[judge.Role]string, held []string, ask func(model string) judge.Ask, now func() time.Time, landing session.TaskLanding) (string, []judge.Score) {
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
		return "", nil
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
	return judgeID, scores
}

// gradeLast is the small record the hook leaves after every landing it
// graded, beside judge-last.json, for the one reading that would otherwise
// need --debug: whether the tree passed, how deep the grade went, and where
// it stopped when it did not.
type gradeLast struct {
	At       time.Time `json:"at"`
	Task     uint64    `json:"task"`
	Source   string    `json:"source"`
	Pass     bool      `json:"pass"`
	Stage    string    `json:"stage,omitempty"`
	Detail   string    `json:"detail,omitempty"`
	Packages []string  `json:"packages,omitempty"`
	Seconds  float64   `json:"seconds"`
	// Grader is [grade.Version], so a record left by an older grader can be
	// told from one left by this build.
	Grader int `json:"grader"`
}

// gradeLastName is the file the grade record is kept as, under the pool
// directory.
const gradeLastName = "grade-last.json"

// poolGradeLanding runs the model-free grade over the workspace the task
// changed and leaves its record. ok is false when there is no workspace to
// read — none given, or a path no longer on disk, which is what a landing the
// restart sweep replays may carry — or when it is not one the grader can
// read, and nothing is left behind then: an absent capability, not a failed
// one. The paths graded are the ones the landing says it wrote, resolved
// inside the workspace by the grader itself.
func poolGradeLanding(poolDir, workspace string, now func() time.Time, landing session.TaskLanding) (grade.Result, bool) {
	if workspace == "" {
		return grade.Result{}, false
	}
	if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
		return grade.Result{}, false
	}
	result, ok := grade.Grade(context.Background(), workspace, landing.Wrote, grade.Options{})
	if !ok {
		return grade.Result{}, false
	}
	last := gradeLast{At: now(), Task: landing.ID, Source: result.Source, Pass: result.Pass, Stage: result.Stage, Detail: result.Detail, Packages: result.Packages, Seconds: result.Elapsed.Seconds(), Grader: result.Version}
	if data, err := json.MarshalIndent(last, "", "  "); err == nil {
		if err := os.MkdirAll(poolDir, 0o700); err == nil {
			if err := os.WriteFile(filepath.Join(poolDir, gradeLastName), append(data, '\n'), 0o600); err != nil && trace.Enabled() {
				log.Printf("model pool: grade record: %v", err)
			}
		}
	}
	if trace.Enabled() {
		log.Printf("model pool: task %d graded %v by %s at %s", landing.ID, result.Pass, result.Source, result.Stage)
	}
	return result, true
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
// for the restart sweep to judge, under the door it ran on. The optional fields
// are forward room for a later grader (an acceptable/source verdict, a role->model
// map, a lease propensity); this build writes only Door and Landing, and the sweep
// ignores fields it does not know rather than refusing a row.
type pendingLanding struct {
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
// each other's rows.
func writePendingLanding(profileDir, door string, landing session.TaskLanding) error {
	poolDir := config.ProfilePath(profileDir, "pool")
	if err := os.MkdirAll(poolDir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(pendingLanding{Door: door, Landing: landing})
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

// poolJudgeSweep judges, at chat start, every landed run that never was: the
// headless doors' pending rows, and the resumed session's own final-state nodes
// that a process death left unjudged. It runs on its own goroutine, so the
// session's checkpoint already reflects whatever recovery made of it and a node
// that will run again is no longer in a final state. It is a no-op when the pool
// cannot read or no judge-capable key is present — with no key the rows simply
// wait. The whole sweep is bounded by poolSweepBudget, checked between landings.
func poolJudgeSweep(settings config.Config, profileDir, tasksPath string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time) {
	defer guard.Recover("pool/judge-sweep")
	if !config.ModelPoolAt(profileDir).CanRead() {
		return
	}
	if strings.TrimSpace(settings.APIKey) == "" {
		return
	}
	poolDir := config.ProfilePath(profileDir, "pool")
	deadline := now().Add(poolSweepBudget)
	sweepPending(settings, profileDir, poolDir, models, ask, now, deadline)
	if strings.TrimSpace(tasksPath) == "" {
		return
	}
	landed, err := session.LoadLandedForJudge(tasksPath)
	if err != nil {
		if trace.Enabled() {
			log.Printf("model pool: sweep load: %v", err)
		}
		return
	}
	for _, landing := range landed {
		if !now().Before(deadline) {
			return
		}
		if alreadyJudged(poolDir, landing.ID, landing.Attempt) {
			continue
		}
		// The restart sweep has no workspace to hand over — the tree the run
		// changed may be gone — so the grade is skipped and the judge reads.
		poolJudgeLanding(settings, profileDir, models, ask, now, "task", "", landing)
	}
}

// sweepPending claims the pending file with an atomic rename so concurrent doors
// keep appending to a fresh one, then judges each row it claimed under that row's
// own door. A leftover claim from a sweep a process death cut short is taken
// first.
func sweepPending(settings config.Config, profileDir, poolDir string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time, deadline time.Time) {
	claim := pendingPath(poolDir) + ".sweeping"
	sweepClaim(settings, profileDir, poolDir, claim, models, ask, now, deadline)
	if err := os.Rename(pendingPath(poolDir), claim); err != nil {
		return
	}
	sweepClaim(settings, profileDir, poolDir, claim, models, ask, now, deadline)
}

// sweepClaim judges the rows of one claimed batch. A torn last line (a row
// half-written when the rename landed) is skipped, not fatal; unknown future
// fields are ignored; a row already judged is skipped. The claim is removed only
// when every row was reached, so a deadline cut leaves the rest for the next
// start, where the markers keep the already-judged rows from being scored twice.
func sweepClaim(settings config.Config, profileDir, poolDir, claim string, models func() []catalog.Model, ask func(model string) judge.Ask, now func() time.Time, deadline time.Time) {
	data, err := os.ReadFile(claim)
	if err != nil {
		return
	}
	completed := true
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !now().Before(deadline) {
			completed = false
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
		// A swept row's workspace may be gone by now, so no path is passed and
		// the grade is skipped; the judge still reads the landing it was given.
		poolJudgeLanding(settings, profileDir, models, ask, now, door, "", row.Landing)
	}
	if completed {
		_ = os.Remove(claim)
	}
}
