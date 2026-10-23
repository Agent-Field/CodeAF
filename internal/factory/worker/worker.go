// Package worker is the factory's execution half: a loop that attaches to a
// factory, pulls a job, runs it with this machine's own Runner, and reports
// what the runner wrote back over /v1/workers (ARCHITECTURE §4).
//
// THE RUNNER DOES NOT KNOW IT IS REMOTE. It runs over a mirror store on this
// machine holding the jobs this worker leased; every write it makes there is
// read back as a report ([run.Diff]) and sent on the lease. Its state machine
// is the one internal/factory/run has always had; only who owns the job (a
// lease) and where its writes land (a report) changed.
package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	factorythread "github.com/Agent-Field/codeaf/internal/factory/thread"
)

// Options is one machine's worker against one factory.
type Options struct {
	// Dial answers the worker door for slot, with the credential cached for
	// it, into the factory this machine uses now. It is asked again after a
	// failure, so a factory that moved is followed.
	Dial func(slot int) *client.WorkerClient
	// Keep caches the credential attach answered for slot.
	Keep func(slot int, w *client.WorkerClient)
	// Attach names the machine, its person and its consent to unassigned work.
	Attach api.WorkerAttach
	// Runner runs the jobs over Mirror.
	Runner *run.Runner
	Mirror *store.Store
	// LocalLease keeps this machine's receipt of a pulled or renewed lease.
	// An empty lease removes the receipt before the mirror is put away.
	LocalLease func(base string, lease api.WorkerLease)
	// Reply submits words to the manager on this machine. A failed delivery
	// stays pending; the next heartbeat offers it again.
	Reply func(context.Context, factory.Item, string) error
	// Slots is how many jobs run at once until the owner's saved capacity is
	// known; each slot is its own worker. It is clamped to 1..api.MaxRunnerSlots.
	Slots int
	// Desired reads the capacity the owner saved, with a credential this
	// worker already holds. ok false means unknown (not attached yet, an older
	// factory, the factory unreachable) and the capacity stays as it is. Nil
	// keeps Slots fixed for the life of the worker.
	Desired func(ctx context.Context) (slots int, ok bool)
	// CapacityTick paces the Desired reads; tests inject it. Nil means a
	// ticker every CapacityEvery.
	CapacityTick <-chan time.Time
	// CapacityEvery is that ticker's period; zero is [CapacityEveryDefault].
	CapacityEvery time.Duration
	// ReportEvery and BeatEvery pace the reports and the heartbeats (the
	// heartbeat is also how a person's verbs arrive); zero is 500ms and
	// [lease.HeartbeatEvery]. Sixty seconds of silence is offline.
	ReportEvery, BeatEvery time.Duration
	// DeliveryRoot keeps unsent per-lease result artifacts outside disposable
	// mirrors. Empty uses a private child of Mirror; app hosts supply their home.
	DeliveryRoot string
	// LocalFactory permits a retained loopback origin to change its ephemeral
	// port across restart. Only the app host owning this private store sets it.
	LocalFactory bool
	// Now makes bounded delivery backoff deterministic in isolated checks.
	Now func() time.Time

	cap *capacity
}

// CapacityEveryDefault is how often the owner's saved capacity is re-read.
// A lowering or a raise is therefore followed within this long, and the read
// is one small request per period, however many slots are running.
const CapacityEveryDefault = 5 * time.Second

// capacity is how many slots may run now, shared by the supervisor and the
// slot loops. Lowering never touches a running job: a slot above the limit
// retires only between jobs, after its last pull returns.
type capacity struct {
	mu      sync.Mutex
	limit   int
	running map[int]bool
	wake    chan struct{}
}

func clampSlots(n int) int {
	if n <= 0 {
		n = api.DefaultRunnerSlots
	}
	return min(n, api.MaxRunnerSlots)
}

// set moves the limit and wakes the supervisor to start slots a raise freed.
func (c *capacity) set(n int) {
	n = max(1, min(n, api.MaxRunnerSlots))
	c.mu.Lock()
	changed := c.limit != n
	c.limit = n
	c.mu.Unlock()
	if changed {
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
}

// retire stops slot when it is above the limit. The check and the mark are
// one step, so a raise that lands meanwhile keeps the slot instead of
// stranding it until the next tick.
func (c *capacity) retire(slot int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if slot < c.limit {
		return false
	}
	delete(c.running, slot)
	return true
}

// Run works until ctx ends.
func Run(ctx context.Context, opts Options) {
	if opts.ReportEvery <= 0 {
		opts.ReportEvery = 500 * time.Millisecond
	}
	if opts.BeatEvery <= 0 {
		opts.BeatEvery = lease.HeartbeatEvery
	}
	opts.Slots = clampSlots(opts.Slots)
	opts.Attach.Slots = opts.Slots
	// Only a worker that reads the saved number may say it follows changes.
	opts.Attach.LiveCapacity = opts.Desired != nil
	opts.cap = &capacity{limit: opts.Slots, running: map[int]bool{}, wake: make(chan struct{}, 1)}
	tick := opts.CapacityTick
	if opts.Desired != nil && tick == nil {
		every := opts.CapacityEvery
		if every <= 0 {
			every = CapacityEveryDefault
		}
		t := time.NewTicker(every)
		defer t.Stop()
		tick = t.C
	}
	var slots sync.WaitGroup
	defer slots.Wait()
	for {
		// Start every slot below the limit that is not running. Slots are
		// started lazily so an idle surplus slot never attaches a credential.
		opts.cap.mu.Lock()
		for i := 0; i < opts.cap.limit; i++ {
			if opts.cap.running[i] {
				continue
			}
			opts.cap.running[i] = true
			slots.Add(1)
			go func(slot int) {
				defer slots.Done()
				opts.slot(ctx, slot)
			}(i)
		}
		opts.cap.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-opts.cap.wake:
		case <-tick:
			if n, ok := opts.Desired(ctx); ok && n > 0 {
				opts.cap.set(n)
			}
		}
	}
}

// slot pulls and runs one job at a time. Failures back off and dial again.
func (o Options) slot(ctx context.Context, slot int) {
	wait := time.Duration(0)
	for pause(ctx, wait) {
		if o.cap.retire(slot) {
			return
		}
		w := o.Dial(slot)
		if w == nil {
			wait = backoff(wait)
			continue
		}
		if w.Credential() == "" {
			attached, err := w.Attach(ctx, o.Attach)
			if err != nil {
				wait = backoff(wait)
				continue
			}
			// THE OWNER'S SAVED NUMBER WINS over this process's own default
			// or environment pin, so a restart resumes at what was chosen.
			// Only an elastic worker listens; an older factory says nothing.
			if o.Desired != nil && attached.Slots > 0 {
				o.cap.set(attached.Slots)
			}
			if o.Keep != nil {
				o.Keep(slot, w)
			}
		}
		o.recoverRetained(ctx, w)
		began := time.Now()
		pulled, ok, err := w.Pull(ctx)
		switch {
		case errors.Is(err, client.ErrNotAttached):
			o.forget(slot, w)
			wait = 0
		case err != nil:
			wait = backoff(wait)
		case !ok && time.Since(began) < 200*time.Millisecond:
			// A pull that came back at once (this worker still holds a job
			// in another process) must not spin.
			wait = time.Second
		case !ok:
			wait = 0
		default:
			wait = 0
			o.work(ctx, w, pulled)
		}
		// A surplus slot leaves between jobs, never during one, and does not
		// sit out its backoff first.
		if o.cap.retire(slot) {
			return
		}
	}
}

func (o Options) forget(slot int, w *client.WorkerClient) {
	if o.Keep != nil {
		o.Keep(slot, w.Worker(""))
	}
}

func pause(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func backoff(d time.Duration) time.Duration {
	return min(max(2*d, time.Second), 30*time.Second)
}

// job is one leased job on this machine.
type job struct {
	o     Options
	w     *client.WorkerClient
	lease string
	id    int
	sent  factory.Item // the mirror as last reported

	mu           sync.Mutex
	replying     bool
	replyCancel  context.CancelFunc
	accepted     map[string]bool
	acknowledged map[string]bool
	thread       []factory.Message
	pending      *pendingDelivery
	blocked      string
	control      *client.PreparedWorkerReport
	controlTries int
	controlSent  bool
}

// work runs one pulled job to its end, or until the lease is gone.
func (o Options) work(ctx context.Context, w *client.WorkerClient, pulled api.WorkerPull) {
	it := api.ToItem(pulled.Job)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	j := &job{o: o, w: w, lease: pulled.Lease.ID, id: it.ID, accepted: map[string]bool{}, acknowledged: map[string]bool{}}
	defer j.clearMirror()
	defer j.keepLocal(api.WorkerLease{Job: it.ID})
	pulledTalk, pulledNotes := it.Talk, it.Notes
	start := stand(&it, pulled.Signals)
	if err := o.Mirror.Save(it); err != nil {
		j.refuse(ctx, err)
		return
	}
	j.keepLocal(pulled.Lease)
	if pulled.Lease.ThreadOnly {
		j.sent = it
		j.signals(ctx, pulled.Signals)
		j.converse(ctx)
		return
	}
	if err := start(o.Runner, it.ID); err != nil {
		j.refuse(ctx, err)
		return
	}
	// THE BASELINE IS THE JOB AS STARTED, less what the factory never held:
	// the manager conversation and the brief the start wrote are the run's
	// and go in the first report.
	j.sent, _ = o.Mirror.Get(it.ID)
	j.sent.Talk, j.sent.Notes = pulledTalk, pulledNotes
	j.signals(ctx, pulled.Signals)
	report := time.NewTicker(o.ReportEvery)
	defer report.Stop()
	beat := time.NewTicker(o.BeatEvery)
	defer beat.Stop()
	for {
		if !o.Runner.Running(it.ID) && !j.replyingNow() && j.pending == nil && j.blocked == "" {
			if j.flushFinal(ctx) {
				if cur, err := o.Mirror.Get(it.ID); err == nil {
					_ = w.Done(ctx, j.lease, final(cur))
				}
				return
			}
		}
		select {
		case <-ctx.Done():
			j.drop()
			return
		case <-report.C:
			if !j.flush(ctx) {
				j.drop()
				return
			}
		case <-beat.C:
			if !j.beat(ctx) {
				j.drop()
				return
			}
		}
	}
}

// stand puts the pulled job where the runner's own doors pick it up, and
// answers the door: a publish carries a person's approval out, a recheck
// re-runs a landed job's checks, a job whose run
// has not begun is launched, and one that had (a pause, a request for
// changes, a worker that went away) goes on at the step it stopped in.
func stand(it *factory.Item, signals []api.WorkerSignal) func(*run.Runner, int) error {
	for _, s := range signals {
		switch s.Verb {
		case lease.SignalRecheck:
			it.State = factory.StateLanded
			return (*run.Runner).Reverify
		case lease.SignalPublish:
			// A PERSON APPROVED THE PROOF: this machine publishes it, citing
			// the trailer the factory read from its record.
			it.State = factory.StateLanded
			trailer := s.Words
			return func(r *run.Runner, id int) error { return r.Publish(id, trailer) }
		case lease.SignalMerge:
			// A MERGE THE FACTORY'S GATE PASSED: this machine reads the pull
			// request again, runs the same gate and merges only that commit.
			it.State = factory.StateShipped
			commit := s.Words
			return func(r *run.Runner, id int) error { return r.Merge(id, commit) }
		}
	}
	if begun(it.Stream) {
		it.State = factory.StateRunning
		it.Stream.Paused = true
		return func(r *run.Runner, id int) error { return r.Hold(id, false) }
	}
	it.State = factory.StateNew
	return (*run.Runner).Launch
}

// begun is whether the run has started. A step with a round has run, even when
// a send-back from an approve step stood every step pending again: launching
// that job fresh would run the steps before it a second time.
func begun(s *factory.Stream) bool {
	if s == nil {
		return false
	}
	for _, ph := range s.Phases {
		if ph.State != factory.PhasePending || ph.Round > 0 {
			return true
		}
	}
	return false
}

// flush retains one immutable per-lease request until the factory confirms it.
// Deterministic rejection parks execution and preserves the rejected baseline;
// transport failures do not turn newly incurred cost into a larger replay.
func (j *job) flush(ctx context.Context) bool {
	if j.blocked != "" {
		if j.recoverBlocked(ctx, false) {
			return false
		}
		return j.reportBlocked(ctx)
	}
	if j.pending != nil {
		return j.sendPending(ctx)
	}
	cur, err := j.o.Mirror.Get(j.id)
	if err != nil {
		return true
	}
	report, ok := run.Diff(j.sent, cur)
	var messages []factory.Message
	for _, m := range factorythread.Read(cur.Talk) {
		if m.From == "manager" {
			messages = append(messages, m)
		}
	}
	known := map[string]bool{}
	for _, m := range j.thread {
		known[m.ID] = true
	}
	for _, m := range messages {
		if !known[m.ID] {
			report.Thread = append(report.Thread, m)
			if len(report.Thread) == 32 {
				break
			}
		}
	}
	ok = ok || len(report.Thread) > 0
	j.mu.Lock()
	for id := range j.accepted {
		if !j.acknowledged[id] {
			report.Delivered = append(report.Delivered, id)
		}
	}
	j.mu.Unlock()
	ok = ok || len(report.Delivered) > 0
	if !ok {
		return true
	}
	pending, err := j.prepareDelivery(cur, report)
	if err != nil {
		j.blockDelivery("Results could not be retained locally; work stopped before sending them.")
		return j.reportBlocked(ctx)
	}
	j.pending = pending
	return j.sendPending(ctx)
}

// flushFinal sends every bounded batch before giving the lease back, even
// when the runner finished before the first periodic report.
func (j *job) flushFinal(ctx context.Context) bool {
	for range (factory.ThreadMost + 31) / 32 {
		if !j.flush(ctx) || j.pending != nil || j.blocked != "" {
			return false
		}
	}
	return true
}

// beat renews the lease and carries every verb a person asked of the job
// through the runner's own door.
func (j *job) beat(ctx context.Context) bool {
	beatCtx, cancel := context.WithTimeout(ctx, deliveryRequestTimeout)
	defer cancel()
	hb, err := j.w.Heartbeat(beatCtx, j.lease)
	if gone(err) {
		return false
	}
	if err == nil {
		j.keepLocal(hb.Lease)
	}
	return j.signals(ctx, hb.Signals)
}

// signals uses the runner for step verbs and the manager for replies. Stop
// answers false so the worker does not report the job back as backlog.
func (j *job) signals(ctx context.Context, signals []api.WorkerSignal) bool {
	r := j.o.Runner
	for _, s := range signals {
		// A generic Resume or manager reply cannot bypass reconciliation of a
		// rejected result. Stop remains available and unsent replies stay pending.
		if j.blocked != "" && s.Verb == lease.SignalReconcile {
			if j.recoverBlocked(ctx, true) {
				return false
			}
			continue
		}
		if j.blocked != "" && s.Verb != lease.SignalStop {
			continue
		}
		switch s.Verb {
		case lease.SignalReply:
			j.reply(ctx, s)
		case lease.SignalBudget:
			if s.Budget <= 0 {
				continue
			}
			if err := j.o.Mirror.Update(j.id, func(it *factory.Item) error { it.Cap = s.Budget; return nil }); err != nil {
				return false
			}
			if it, err := j.o.Mirror.Get(j.id); s.Yes && err == nil && it.QKind == "cap" && s.Budget > factory.JobSpent(it) && factory.Automatic(it) && !it.Publishing {
				_ = r.Answer(j.id, true, factory.ChoiceContinue)
			}
		case lease.SignalHold:
			_ = r.Hold(j.id, s.Yes)
		case lease.SignalAnswer:
			if s.ID != "" {
				it, err := j.o.Mirror.Get(j.id)
				if err != nil || factory.PendingQuestionID(it) != s.ID {
					continue
				}
			}
			_ = r.Answer(j.id, s.Yes, s.Words)
		case lease.SignalSteer:
			_ = r.Steer(j.id, s.Words)
		case lease.SignalModel:
			_ = r.SetModelSelection(j.id, s.Words, s.Automatic)
		case lease.SignalAutonomy:
			if err := j.o.Mirror.Update(j.id, func(it *factory.Item) error { it.Autonomy = s.Words; return nil }); err != nil {
				return false
			}
		case lease.SignalStop:
			// Drop asked this machine to stop. Stop keeps the branch.
			_ = r.Stop(j.id)
			return false
		}
	}
	return true
}

// reply serializes manager turns while reporting and renewing continue.
func (j *job) reply(ctx context.Context, s api.WorkerSignal) {
	j.mu.Lock()
	if j.replying || j.accepted[s.ID] || j.o.Reply == nil {
		j.mu.Unlock()
		return
	}
	j.replying = true
	ctx, cancel := context.WithCancel(ctx)
	j.replyCancel = cancel
	j.mu.Unlock()
	go func() {
		defer cancel()
		it, err := j.o.Mirror.Get(j.id)
		if err == nil {
			err = j.o.Reply(ctx, it, s.Words)
		}
		j.mu.Lock()
		if err == nil {
			j.accepted[s.ID] = true
		}
		j.replying = false
		j.replyCancel = nil
		j.mu.Unlock()
	}()
}

func (j *job) replyingNow() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.replying
}

// converse holds only the manager; done leaves all job steps and state alone.
func (j *job) converse(ctx context.Context) {
	ticker := time.NewTicker(j.o.BeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !j.flush(ctx) || !j.beat(ctx) {
				return
			}
			j.mu.Lock()
			finished := !j.replying && len(j.accepted) > 0
			j.mu.Unlock()
			if finished && j.flushFinal(ctx) {
				_ = j.w.Done(ctx, j.lease, api.WorkerDone{})
				return
			}
		}
	}
}

// StopWait is how long a worker waits for its runner to stop a job the
// factory took back before it lets the job go. A drop on this machine waits
// at least this long, after the worker's next heartbeat, before it deletes
// the job's folder.
const StopWait = 5 * time.Second

// drop lets the job go here: the factory stopped it, or this process is
// ending and the lease will expire into the queue for another worker.
func (j *job) drop() {
	j.keepLocal(api.WorkerLease{Job: j.id})
	_ = j.o.Runner.Stop(j.id)
	for deadline := time.Now().Add(StopWait); j.o.Runner.Running(j.id) && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
	}
}

// keepLocal records only leases the factory actually granted or renewed.
func (j *job) keepLocal(held api.WorkerLease) {
	if j.o.LocalLease != nil {
		j.o.LocalLease(j.w.Base(), held)
	}
}

// refuse says why this worker could not run the job, and gives it back.
func (j *job) refuse(ctx context.Context, why error) {
	line := factory.LogLine{At: time.Now(), Tone: "fail", Text: "this machine could not run it: " + why.Error()}
	_ = j.w.Report(ctx, j.lease, api.FromRunReport(factory.RunReport{Log: []factory.LogLine{line}}))
	_ = j.w.Done(ctx, j.lease, api.WorkerDone{State: api.StateBacklog})
}

// final is the state the job ended in, as done says it. Landing is decided
// again on the server from the proof it holds; a job shipped with a PR or
// comment says done, which the server takes only beside a recorded approval.
func final(it factory.Item) api.WorkerDone {
	switch it.State {
	case factory.StateShipped:
		if it.PullRequest != nil || it.CommentURL != "" {
			return api.WorkerDone{State: api.StateDone}
		}
		return api.WorkerDone{}
	case factory.StateLanded:
		return api.WorkerDone{}
	case factory.StateDismissed:
		return api.WorkerDone{State: api.StateDropped}
	case factory.StateNeedsYou:
		if it.Question != "" {
			return api.WorkerDone{State: api.StateNeedsYou}
		}
	}
	return api.WorkerDone{State: api.StateBacklog}
}

func gone(err error) bool {
	return errors.Is(err, client.ErrLeaseGone) || errors.Is(err, client.ErrNotAttached)
}
