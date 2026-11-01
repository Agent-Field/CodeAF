package main

// ── codeaf app: THE WRITER OF THIS MACHINE'S FACTORY'S BRIEFS ──────────────
//
// The engine's half of appwire.FactoryJobBriefWriter, and the writer that
// runs on its own. A write is three steps, each the factory's or the
// model's, never this file's judgment: the factory's POST brief/begin (which
// decides whether to write, marks the brief generating and hands back the
// prompt and the job as the writer reads it), ONE errand on the job's agent
// under the brief role, and the factory's POST brief/keep (which checks the
// answer against the job, keeps what passes and books the cost).
//
// TRIGGERS are the runner's progress news for a job on this machine (a step
// done, failed or stopped is a step result, and a step waiting on its person
// is an ask) and a person's change to a job's steps or model through this
// engine. A GitHub comment read is not a trigger yet: nothing in the engine
// says when the manager has read one. Each job's news is taken as one after factory.BriefDebounce,
// the strongest trigger of the burst kept, so a burst of results is one
// version. A person's Retry is factory.job_brief_write.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

func init() { roles.Register(roles.RoleBrief, roles.TierLow, "a factory job's brief") }

var _ appwire.FactoryJobBriefWriter = (*appBackend)(nil)

// briefPurpose labels the writer's errand on the spend ledger.
const briefPurpose = "factory_brief"

// briefMaxTokens bounds the writer's answer: four blocks of short claims.
const briefMaxTokens = 2000

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
			Factory: factoryID, Job: job, Status: kept.Status, Failure: kept.Failure, FailedAfter: kept.FailedAfter,
			Brief: kept.Brief, ChangedSinceSeen: orKeys(kept.ChangedSinceSeen),
		},
	}, nil
}

// briefErrand asks the job's agent for the brief. Every way of not
// answering is a Reason, which the factory keeps as the write's failure.
func (b *appBackend) briefErrand(ctx context.Context, factoryID string, job int, begin api.BriefBegin) session.ErrandAnswer {
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
	if had == "" || briefForcedTrigger(next) {
		return next
	}
	return had
}

func briefForcedTrigger(t string) bool {
	return t == factory.BriefTriggerAsk || t == factory.BriefTriggerEnd || t == factory.BriefTriggerRetry
}

// briefDebounce takes each job's triggers as one after wait.
type briefDebounce struct {
	wait    time.Duration
	fire    func(job int, trigger string)
	mu      sync.Mutex
	pending map[int]string
	timers  map[int]*time.Timer
}

func newBriefDebounce(wait time.Duration, fire func(job int, trigger string)) *briefDebounce {
	return &briefDebounce{wait: wait, fire: fire, pending: map[int]string{}, timers: map[int]*time.Timer{}}
}

// note is one trigger for a job: the job's wait starts again.
func (d *briefDebounce) note(job int, trigger string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pending[job] = strongerTrigger(d.pending[job], trigger)
	if t := d.timers[job]; t != nil {
		t.Stop()
	}
	d.timers[job] = time.AfterFunc(d.wait, func() { d.take(job) })
}

func (d *briefDebounce) take(job int) {
	d.mu.Lock()
	trigger := d.pending[job]
	delete(d.pending, job)
	delete(d.timers, job)
	d.mu.Unlock()
	if trigger != "" {
		d.fire(job, trigger)
	}
}

// stop drops every trigger still waiting.
func (d *briefDebounce) stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for job, t := range d.timers {
		t.Stop()
		delete(d.timers, job)
		delete(d.pending, job)
	}
}

// briefNews turns a progress line of a job on this machine's factory into a
// trigger of its brief.
func (b *appBackend) briefNews(news session.ProgressNews) {
	if news.Step == nil || b.briefs == nil {
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
	if !running {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), appwire.BriefWriteBudget)
	defer cancel()
	if _, err := b.writeBrief(ctx, id, job, trigger); err != nil {
		fmt.Fprintf(os.Stderr, "codeaf app: job %d brief not written: %v\n", job, err)
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
	if _, id, running := b.localNow(); running && id == factoryID && b.briefs != nil {
		b.briefs.note(job, factory.BriefTriggerEdit)
	}
}
