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
	"os"
	"reflect"
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
	// Slots is how many jobs run at once; each slot is its own worker.
	Slots int
	// ReportEvery and BeatEvery pace the reports and the heartbeats (the
	// heartbeat is also how a person's verbs arrive); zero is 500ms and
	// [lease.HeartbeatEvery]. Three missed beats is offline.
	ReportEvery, BeatEvery time.Duration
}

// Run works until ctx ends.
func Run(ctx context.Context, opts Options) {
	if opts.ReportEvery <= 0 {
		opts.ReportEvery = 500 * time.Millisecond
	}
	if opts.BeatEvery <= 0 {
		opts.BeatEvery = lease.HeartbeatEvery
	}
	slots := max(opts.Slots, 1)
	done := make(chan struct{}, slots)
	for i := 0; i < slots; i++ {
		go func(slot int) {
			defer func() { done <- struct{}{} }()
			opts.slot(ctx, slot)
		}(i)
	}
	for i := 0; i < slots; i++ {
		<-done
	}
}

// slot pulls and runs one job at a time. Failures back off and dial again.
func (o Options) slot(ctx context.Context, slot int) {
	wait := time.Duration(0)
	for pause(ctx, wait) {
		w := o.Dial(slot)
		if w == nil {
			wait = backoff(wait)
			continue
		}
		if w.Credential() == "" {
			if _, err := w.Attach(ctx, o.Attach); err != nil {
				wait = backoff(wait)
				continue
			}
			if o.Keep != nil {
				o.Keep(slot, w)
			}
		}
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
	shaping      bool
	replying     bool
	accepted     map[string]bool
	acknowledged map[string]bool
	thread       []factory.Message
}

// work runs one pulled job to its end, or until the lease is gone.
func (o Options) work(ctx context.Context, w *client.WorkerClient, pulled api.WorkerPull) {
	it := api.ToItem(pulled.Job)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	j := &job{o: o, w: w, lease: pulled.Lease.ID, id: it.ID, accepted: map[string]bool{}, acknowledged: map[string]bool{}}
	defer os.Remove(o.Mirror.ItemPath(it.ID))
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
	for o.Runner.Running(it.ID) || j.replyingNow() {
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
	if j.flushFinal(ctx) {
		cur, err := o.Mirror.Get(it.ID)
		if err == nil {
			_ = w.Done(ctx, j.lease, final(cur))
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

func begun(s *factory.Stream) bool {
	if s == nil {
		return false
	}
	for _, ph := range s.Phases {
		if ph.State != factory.PhasePending {
			return true
		}
	}
	return false
}

// flush reports what the runner wrote since the last report. It answers
// false when the lease is gone. A report the factory refused as wrong is not
// sent again; one that did not arrive is.
func (j *job) flush(ctx context.Context) bool {
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
	if !reflect.DeepEqual(messages, j.thread) {
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
	}
	j.mu.Lock()
	for id := range j.accepted {
		if !j.acknowledged[id] {
			report.Delivered = append(report.Delivered, id)
		}
	}
	j.mu.Unlock()
	if len(report.Delivered) > 0 {
		ok = true
	}
	if !ok {
		return true
	}
	err = j.w.Report(ctx, j.lease, api.FromRunReport(report))
	if gone(err) {
		return false
	}
	if err == nil {
		j.mu.Lock()
		for _, id := range report.Delivered {
			j.acknowledged[id] = true
		}
		j.mu.Unlock()
	}
	if err == nil || !transient(err) {
		j.sent = cur
		j.thread = append(j.thread, report.Thread...)
		if len(j.thread) > factory.ThreadMost {
			j.thread = j.thread[len(j.thread)-factory.ThreadMost:]
		}
	}
	return true
}

// flushFinal sends every bounded batch before giving the lease back, even
// when the runner finished before the first periodic report.
func (j *job) flushFinal(ctx context.Context) bool {
	for range (factory.ThreadMost + 31) / 32 {
		if !j.flush(ctx) {
			return false
		}
	}
	return true
}

// beat renews the lease and carries every verb a person asked of the job
// through the runner's own door.
func (j *job) beat(ctx context.Context) bool {
	hb, err := j.w.Heartbeat(ctx, j.lease)
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
		switch s.Verb {
		case lease.SignalReply:
			j.reply(ctx, s)
		case lease.SignalHold:
			_ = r.Hold(j.id, s.Yes)
		case lease.SignalAnswer:
			_ = r.Answer(j.id, s.Yes, s.Words)
		case lease.SignalSteer:
			_ = r.Steer(j.id, s.Words)
		case lease.SignalShape:
			j.shape(ctx)
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
	j.mu.Unlock()
	go func() {
		it, err := j.o.Mirror.Get(j.id)
		if err == nil {
			err = j.o.Reply(ctx, it, s.Words)
		}
		j.mu.Lock()
		if err == nil {
			j.accepted[s.ID] = true
		}
		j.replying = false
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

// shape gives the manager one shaping turn on this job. A turn already out
// is left to finish: the second signal asks nothing more. The turn can take
// as long as a model call, so it does not hold the heartbeat.
func (j *job) shape(ctx context.Context) {
	j.mu.Lock()
	if j.shaping {
		j.mu.Unlock()
		return
	}
	j.shaping = true
	j.mu.Unlock()
	go func() {
		defer func() {
			j.mu.Lock()
			j.shaping = false
			j.mu.Unlock()
		}()
		_, _ = j.o.Runner.ShapeAsked(ctx, j.id)
	}()
}

// drop lets the job go here: the factory stopped it, or this process is
// ending and the lease will expire into the queue for another worker.
func (j *job) drop() {
	j.keepLocal(api.WorkerLease{Job: j.id})
	_ = j.o.Runner.Stop(j.id)
	for deadline := time.Now().Add(5 * time.Second); j.o.Runner.Running(j.id) && time.Now().Before(deadline); {
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

// transient is a report that never reached the factory, as opposed to one it
// read and refused.
func transient(err error) bool {
	var refused client.Refusal
	return !errors.As(err, &refused)
}
