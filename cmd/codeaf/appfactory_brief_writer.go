package main

// ── codeaf app: THE WRITER OF THIS MACHINE'S FACTORY'S BRIEFS ──────────────
//
// The engine's half of appwire.FactoryJobBriefWriter, and the writer that
// runs on its own. A write is three steps, each the factory's or the
// model's, never this file's judgment: the factory's POST brief/begin (which
// decides whether to write, marks the brief generating and hands back the
// prompt and the job as the writer reads it), ONE errand on the job's agent
// under the brief role, and the factory's POST brief/keep (which checks the
// answer against the job and keeps what passes with its already-booked cost).
//
// TRIGGERS are the runner's progress news for a job on this machine (a step
// done, failed or stopped is a step result, and a step waiting on its person
// is an ask), the store's committed lifecycle changes (proof approval and
// publication or end), and a person's step or model edits through this engine. A GitHub comment read is not a trigger yet: nothing in the engine
// says when the manager has read one. Each job's news is taken as one after factory.BriefDebounce,
// the strongest trigger of the burst kept, so a burst of results is one
// version. A person's Retry is factory.job_brief_write.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

func init() { roles.Register(roles.RoleBrief, roles.TierLow, "a factory job's brief") }

var _ appwire.FactoryJobBriefWriter = (*appBackend)(nil)

// briefPurpose labels the writer's errand on the spend ledger.
const briefPurpose = "factory_brief"

// briefMaxTokens bounds the writer's answer: four blocks of short claims.
// MEASURED, NOT GUESSED: ten live briefs on deepseek/deepseek-v4.1-flash with
// the bound at 4096 wrote 1189 to 2860 tokens (median 2036, p95 about 2830),
// and at the old 2000 about half were cut, which read as invalid JSON or as
// an empty answer the adapter asked again for. The p95 and a quarter's margin.
const briefMaxTokens = 3600

func (b *appBackend) FactoryJobBriefWrite(ctx context.Context, p appwire.FactoryJobBriefWriteParams) (appwire.FactoryJobBriefWritten, error) {
	if _, err := b.localHostOf(p.Factory); err != nil {
		return appwire.FactoryJobBriefWritten{}, err
	}
	return b.writeBrief(ctx, p.Factory, p.Job, factory.BriefTriggerRetry)
}

// writeBrief is one write of one job's brief, for one trigger.
func (b *appBackend) writeBrief(ctx context.Context, factoryID string, job int, trigger string) (appwire.FactoryJobBriefWritten, error) {
	h := b.factoryHub()
	raw, err := h.post(ctx, factoryID, jobPath(job, "brief/begin"), api.BriefBeginRequest{Trigger: trigger})
	if err != nil {
		if routeMissing(err) {
			return appwire.FactoryJobBriefWritten{}, factoryTooOld()
		}
		return appwire.FactoryJobBriefWritten{}, err
	}
	var begin api.BriefBegin
	if err := json.Unmarshal(raw, &begin); err != nil {
		return appwire.FactoryJobBriefWritten{}, appwire.Errorf(appwire.CodeEngine, "the factory's answer to a brief is not one this app can read")
	}
	if begin.Skip != "" {
		current, err := b.FactoryJobBrief(ctx, appwire.FactoryJobBriefParams{Factory: factoryID, Job: job})
		return appwire.FactoryJobBriefWritten{Skip: begin.Skip, Drops: []factory.BriefDrop{}, Current: current}, err
	}
	answer := b.briefErrand(ctx, factoryID, job, begin)
	keep := api.BriefKeepRequest{Hash: begin.Hash, Model: answer.Model, CostUSD: answer.Usage.CostUSD, Draft: answer.Text, Failure: answer.Reason}
	raw, err = h.post(ctx, factoryID, jobPath(job, "brief/keep"), keep)
	if err != nil {
		return appwire.FactoryJobBriefWritten{}, err
	}
	var kept api.BriefKept
	if err := json.Unmarshal(raw, &kept); err != nil {
		return appwire.FactoryJobBriefWritten{}, appwire.Errorf(appwire.CodeEngine, "the factory's answer to a brief is not one this app can read")
	}
	logBriefDrops(job, kept)
	return appwire.FactoryJobBriefWritten{
		Kept: kept.Kept, Drops: orDrops(kept.Drops), Model: answer.Model, CostUSD: answer.Usage.CostUSD,
		Current: appwire.FactoryJobBrief{
			Factory: factoryID, Job: job, Status: kept.Status, Failure: kept.Failure, FailureKind: kept.FailureKind, FailedAfter: kept.FailedAfter,
			Brief: kept.Brief, VerdictBehind: kept.VerdictBehind, ChangedSinceSeen: orKeys(kept.ChangedSinceSeen),
		},
	}, nil
}

// briefErrand asks the job's agent for the brief. Every way of not
// answering is a Reason, which the factory keeps as the write's failure.
func (b *appBackend) briefErrand(ctx context.Context, factoryID string, job int, begin api.BriefBegin) session.ErrandAnswer {
	host, err := b.localHostOf(factoryID)
	if err != nil || host.seam.Talk == nil {
		return session.ErrandAnswer{Reason: appwire.ErrandNotOpen}
	}
	current, err := host.floor.Get(job)
	if err != nil || factory.StoppedByPerson(current) {
		return session.ErrandAnswer{Reason: factory.PersonStopReason}
	}
	// Writing a brief needs a manager; reading its chats no longer creates one.
	if _, err := host.seam.Talk(ctx, job); err != nil {
		return session.ErrandAnswer{Reason: appwire.ErrandNotOpen}
	}
	chats, err := b.FactoryJobChats(ctx, appwire.FactoryJobChatsParams{Factory: factoryID, Job: job})
	if err != nil {
		return session.ErrandAnswer{Reason: appwire.ErrandNotOpen}
	}
	ask, err := session.ErrandAsk{
		Purpose: briefPurpose, Role: string(roles.RoleBrief), System: begin.System,
		Messages: []session.ErrandMessage{{Role: "user", Text: begin.Input}},
		JSON:     true, MaxTokens: briefMaxTokens, Deadline: appwire.BriefWriterDeadline,
	}.Normalize()
	if err != nil {
		return session.ErrandAnswer{Reason: session.ErrandModelFailed}
	}
	answer, err := b.Errand(ctx, chats.Agent, ask)
	if err != nil && answer.Reason == "" {
		// A refused ask or route is the writer's own fault; its words go to
		// the log beside the reason the brief keeps.
		fmt.Fprintf(errandLog, "codeaf app: job %d brief errand refused: %v\n", job, err)
		answer.Reason = session.ErrandModelFailed
	}
	if answer.Reason != "" {
		answer.Text = ""
	}
	return answer
}

// logBriefDrops says on the engine's log what validation dropped, so a
// prompt that keeps failing its check is seen.
func logBriefDrops(job int, kept api.BriefKept) {
	for _, d := range kept.Drops {
		fmt.Fprintf(os.Stderr, "codeaf app: job %d brief rev %d dropped %s: %s\n", job, kept.Brief.Rev, d.Part, d.Reason)
	}
	if !kept.Kept && kept.Failure != "" {
		fmt.Fprintf(os.Stderr, "codeaf app: job %d brief not written: %s\n", job, kept.Failure)
	}
}

func orDrops(d []factory.BriefDrop) []factory.BriefDrop {
	if d == nil {
		return []factory.BriefDrop{}
	}
	return d
}

func orKeys(k []string) []string {
	if k == nil {
		return []string{}
	}
	return k
}

// ── TRIGGERS ────────────────────────────────────────────────────────────────

// briefTriggerOf is the trigger a progress line is, "" for none.
var briefTriggerOf = map[factory.ProgressEvent]string{
	factory.ProgressDone:    factory.BriefTriggerStep,
	factory.ProgressFailed:  factory.BriefTriggerStep,
	factory.ProgressStopped: factory.BriefTriggerStep,
	factory.ProgressWaiting: factory.BriefTriggerAsk,
}

// strongerTrigger keeps the trigger of a burst the cost guard would not hold
// back over one it would.
func strongerTrigger(had, next string) string {
	if had == "" || factory.BriefForced(next) {
		return next
	}
	return had
}

// briefDebounce takes each job's triggers as one after wait.
type briefDebounce struct {
	wait    time.Duration
	fire    func(job int, trigger string)
	mu      sync.Mutex
	closing bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	pending map[int]string
	timers  map[int]*time.Timer
}

func newBriefDebounce(wait time.Duration, fire func(job int, trigger string)) *briefDebounce {
	ctx, cancel := context.WithCancel(context.Background())
	return &briefDebounce{
		wait: wait, fire: fire, ctx: ctx, cancel: cancel,
		pending: map[int]string{}, timers: map[int]*time.Timer{},
	}
}

// note is one trigger for a job: the job's wait starts again.
func (d *briefDebounce) note(job int, trigger string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closing {
		return
	}
	d.pending[job] = strongerTrigger(d.pending[job], trigger)
	if t := d.timers[job]; t != nil {
		t.Stop()
	}
	d.timers[job] = time.AfterFunc(d.wait, func() { d.take(job) })
}

func (d *briefDebounce) take(job int) {
	trigger := d.claim(job)
	if trigger == "" {
		return
	}
	defer d.wg.Done()
	d.fire(job, trigger)
}

// claim takes a job's waiting trigger off the debounce, so the write fires
// outside the lock.
func (d *briefDebounce) claim(job int) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closing {
		delete(d.pending, job)
		delete(d.timers, job)
		return ""
	}
	trigger := d.pending[job]
	delete(d.pending, job)
	delete(d.timers, job)
	if trigger != "" {
		d.wg.Add(1)
	}
	return trigger
}

func (d *briefDebounce) context() context.Context {
	if d == nil {
		return context.Background()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ctx != nil {
		return d.ctx
	}
	return context.Background()
}

// briefStopWait bounds how long briefDebounce.stop waits for an in-flight write
// to finish after cancelling its context.
var briefStopWait = poolCloseBudget

// stop drops every trigger still waiting, cancels any write in flight, and
// waits up to [briefStopWait] for it to finish.
func (d *briefDebounce) stop() {
	if d == nil {
		return
	}
	if cancel := d.beginStop(); cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	guard.Go("app/brief-stop", func() {
		defer close(done)
		d.wg.Wait()
	})
	timer := time.NewTimer(briefStopWait)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
	d.rearm()
}

// beginStop refuses new writes, drops every waiting trigger and hands back the
// cancel of the writes in flight.
func (d *briefDebounce) beginStop() context.CancelFunc {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closing = true
	for job, t := range d.timers {
		t.Stop()
		delete(d.timers, job)
		delete(d.pending, job)
	}
	return d.cancel
}

// rearm gives the debounce a fresh context once a stop has waited, so a factory
// started again writes briefs again.
func (d *briefDebounce) rearm() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ctx, d.cancel = context.WithCancel(context.Background())
	d.closing = false
}

// briefNews turns a progress line of a job on this machine's factory into a
// trigger of its brief.
func (b *appBackend) briefNews(news session.ProgressNews) {
	if news.Step == nil || b.briefs == nil || b.isClosing() {
		return
	}
	trigger := briefTriggerOf[news.Step.Event]
	if trigger == "" {
		return
	}
	if job := b.localJobOfTalk(news.Path); job > 0 {
		b.briefs.note(job, trigger)
	}
}

// localJobOfTalk is the job of this machine's factory whose conversation is
// path, 0 for none.
func (b *appBackend) localJobOfTalk(path string) int {
	host, _, _ := b.localNow()
	if host == nil || path == "" {
		return 0
	}
	items, err := host.floor.List()
	if err != nil {
		return 0
	}
	for _, it := range items {
		if it.Talk == path {
			return it.ID
		}
	}
	return 0
}

// writeBriefLater is a debounced write, on the factory this engine serves.
func (b *appBackend) writeBriefLater(job int, trigger string) {
	_, id, running := b.localNow()
	if !running || b.isClosing() {
		return
	}
	ctx, cancel := context.WithTimeout(b.briefs.context(), appwire.BriefWriteBudget)
	defer cancel()
	written, err := b.writeBrief(ctx, id, job, trigger)
	if err != nil {
		if ctx.Err() == nil && !b.isClosing() {
			fmt.Fprintf(os.Stderr, "codeaf app: job %d brief not written: %v\n", job, err)
		}
		return
	}
	// A forced transition still matters when an earlier model call holds the
	// writer. Keep it in the debounce until that call lets the next one begin.
	if written.Skip == factory.BriefSkipWriting && factory.BriefForced(trigger) && b.briefs != nil && !b.isClosing() {
		b.briefs.note(job, trigger)
	}
}

// appBriefDebounce is how long the engine waits on a job's news before it
// writes the job's brief.
const appBriefDebounce = factory.BriefDebounce

// localNow is this machine's factory as it stands: its host, its id and
// whether it is running.
func (b *appBackend) localNow() (*appFactoryHost, string, bool) {
	l := &b.local
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.host, l.id, l.running
}

// briefEdited is a person's change to a job's steps or model, a trigger of
// the brief when the job is this machine's factory's.
func (b *appBackend) briefEdited(factoryID string, job int) {
	if _, id, running := b.localNow(); running && id == factoryID && b.briefs != nil && !b.isClosing() {
		b.briefs.note(job, factory.BriefTriggerEdit)
	}
}

// briefJobChanged hears lifecycle facts at the common committed-write seam,
// separately from step progress so landing and publish lines keep their shape.
func (b *appBackend) briefJobChanged(before, after factory.Item) {
	if b.briefs == nil || b.isClosing() {
		return
	}
	if end := factory.BriefEndOf(after); end != "" && end != factory.BriefEndOf(before) {
		// A job that ended with nothing done on it has no outcome to write up,
		// and a brief is a model call.
		if !factory.JobUnworked(after) {
			b.briefs.note(after.ID, factory.BriefTriggerEnd)
		}
		return
	}
	if p := after.PublishStatus; p != nil && !reflect.DeepEqual(p, before.PublishStatus) && (p.State == "opened" || p.State == "failed") {
		b.briefs.note(after.ID, factory.BriefTriggerEnd)
		return
	}
	if factory.BriefProofApprovalWaiting(after) && !factory.BriefProofApprovalWaiting(before) {
		b.briefs.note(after.ID, factory.BriefTriggerAsk)
	}
}
