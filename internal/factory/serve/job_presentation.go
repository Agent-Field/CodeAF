package serve

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// hostGitHubToken is the host's token answer for one request. A queue presents
// every job, and each presentation asks the same question; resolving it inside
// that loop starts one gh process per stored item.
type hostGitHubToken struct{ has bool }

type hostGitHubTokenKey struct{}

// rememberHostToken resolves the solo host token at most once and returns the
// request that carries the answer. Later presentations of the same request
// read it instead of asking again.
func (h *Handler) rememberHostToken(r *http.Request) *http.Request {
	if _, ok := r.Context().Value(hostGitHubTokenKey{}).(hostGitHubToken); ok {
		return r
	}
	has := false
	if h.mode() == api.FactorySolo && h.config.GitHubToken != nil {
		session, _ := h.credential(r)
		if session.githubToken == "" {
			has = h.config.GitHubToken(r.Context()) != ""
		}
	}
	return r.WithContext(context.WithValue(r.Context(), hostGitHubTokenKey{}, hostGitHubToken{has}))
}

// presentJob is the job as a READER sees it: the worker's view below with the
// runner-local conversation paths withheld. Every queue, job and mutation
// reply goes through here; only the worker pull skips the withholding.
func (h *Handler) presentJob(r *http.Request, j *api.Job) {
	h.presentWorkerJob(r, j)
	withholdChats(j)
	withholdTalk(j)
}

// withholdTalk drops the manager conversation's session FILE PATH from the
// job's desk, for the same reason withholdChats drops a step's: it names a
// directory on the runner's machine and means nothing on any other, the
// owner's included. The worker pull keeps it. The desk is copied before the
// write so the shared payload is never mutated; every other desk fact,
// the thread among them, stays.
func withholdTalk(j *api.Job) {
	if j.Desk == nil || j.Desk.Talk == "" {
		return
	}
	d := *j.Desk
	d.Talk = ""
	j.Desk = &d
}

// withholdChats drops each step's conversation id, and each of its finished
// rounds'. A chat stage's id is the session FILE PATH on the machine that ran
// it (stageConversation.ID), so it names a runner's home directory to whoever
// reads the job, and a path on one machine means nothing on another. ONLY THE
// WORKER holding the lease needs it, to resume the same conversation after a
// pause, and it receives the job from pulled, which does not call this. The
// app on the machine that ran the job opens a step's chat, and a round's, by
// factory.job_chats, which answers agent ids. The slices are cloned so nothing
// is written through into a shared backing array.
func withholdChats(j *api.Job) {
	for i := range j.Steps {
		if !stepNamesChat(j.Steps[i]) {
			continue
		}
		steps := make([]api.Step, len(j.Steps))
		copy(steps, j.Steps)
		for k := i; k < len(steps); k++ {
			steps[k].Chat = ""
			if len(steps[k].Rounds) == 0 {
				continue
			}
			rounds := make([]api.RoundLine, len(steps[k].Rounds))
			copy(rounds, steps[k].Rounds)
			for r := range rounds {
				rounds[r].Chat = ""
			}
			steps[k].Rounds = rounds
		}
		j.Steps = steps
		return
	}
}

// stepNamesChat says a step carries a conversation id: its own, or a round's.
func stepNamesChat(s api.Step) bool {
	if s.Chat != "" {
		return true
	}
	for _, r := range s.Rounds {
		if r.Chat != "" {
			return true
		}
	}
	return false
}

// presentWorkerJob runs after solo permissions and presence have narrowed the
// job. A button appears only when its engine door can handle the current facts.
func (h *Handler) presentWorkerJob(r *http.Request, j *api.Job) {
	api.PresentJob(j)
	h.redactMachines(r, j)
	active := j.State == api.StateRunning || j.State == api.StateQueued || j.State == api.StateNeedsYou
	owner := j.Can.Stop || j.Can.Run || j.Can.RunAgain || j.Can.CancelJob
	if h.mode() == api.FactorySolo {
		owner = true
	}
	step, stoppable, rerunnable := false, false, false
	if j.Step != nil {
		for _, s := range j.Steps {
			if s.Name == j.Step.Name && s.InRun {
				step = true
				stopped := j.Failure != nil && j.Failure.Step == s.Name && j.Failure.Next == factory.FailureStopped
				stoppable = (s.State == api.StepRunning || s.State == api.StepWaiting) && !stopped
				rerunnable = s.State != api.StepPending || stopped
				break
			}
		}
	}
	publishing := j.Publishing != nil && (j.Publishing.State == "queued" || j.Publishing.State == "pushing")
	mutable := j.State != api.StateDone && !publishing
	j.Controls.StopStep = owner && active && stoppable && mutable && h.workers != nil && h.workers.desk != nil
	j.Controls.RerunStep = owner && step && rerunnable && mutable && h.workers != nil && h.workers.desk != nil
	j.Controls.EditSteps = owner && mutable && !j.Ready && h.seam.Edit != nil
	j.Controls.ChangeAssignee = j.Can.ChangeAssignee && h.config.Store != nil
	// The plan's target and the caller's right to answer are the job's own
	// facts (api.CanFor); whether this factory takes the revision is its own.
	j.Can.RejectPlan = j.Can.RejectPlan && h.servesPlanRevision()
	local := j.Desk != nil && j.Desk.Origin != string(factory.OriginForge)
	session, _ := h.credential(r)
	hasToken := session.githubToken != ""
	if !hasToken {
		if answer, ok := r.Context().Value(hostGitHubTokenKey{}).(hostGitHubToken); ok {
			hasToken = answer.has
		} else if h.mode() == api.FactorySolo && h.config.GitHubToken != nil {
			hasToken = h.config.GitHubToken(r.Context()) != ""
		}
	}
	j.Controls.Sync = owner && ((local && hasToken && h.config.Store != nil) || (!local && h.seam.Refresh != nil))
}

// redactMachines removes every machine name and heartbeat that is not the
// caller's own from a job (FS0/FS15 privacy: another person's machine appears
// nowhere, the operator's view included). Who holds the job, and since when,
// stays; only the machine is withheld. A solo factory has one person, so all
// of it is theirs.
func (h *Handler) redactMachines(r *http.Request, j *api.Job) {
	if h.mode() == api.FactorySolo {
		return
	}
	caller := h.machineViewer(r)
	mine := func(handle string) bool { return caller != "" && factory.NormalizeHandle(handle) == caller }
	// Capacity belongs to the job owner's computers, just like their slots and
	// heartbeat. Shared job state remains visible without exposing those counts.
	if j.StatePresentation.Kind == "waiting_for_capacity" && !mine(j.AssignedTo) {
		j.StatePresentation.Detail = "Waiting for an available execution slot."
	}
	// A live hold names its machine from the lease and the worker's own handle:
	// that is wire provenance. The job's assignee proves nothing about a stored
	// machine id (a reassigned job keeps the first worker's id), so every
	// historical name below is shown only on proof, and withheld otherwise.
	live := ""
	if j.HeldBy != nil {
		if mine(j.HeldBy.Handle) {
			live = j.HeldBy.Machine
		} else {
			held := *j.HeldBy
			held.Machine, held.HeartbeatAt = "", time.Time{}
			j.HeldBy = &held
		}
	}
	proven := func(machine string) bool {
		return machine != "" && (machine == live || h.callersOnlyMachine(caller, machine))
	}
	if !proven(j.Runner) {
		j.Runner = ""
	}
	if j.Failure != nil && (!proven(j.Failure.Machine) || !proven(j.Failure.Runner)) {
		f := *j.Failure
		if !proven(f.Runner) {
			f.Runner = ""
		}
		if !proven(f.Machine) {
			f.Machine = ""
		}
		j.Failure = &f
	}
	// Each step carries its own copy of the failure (StampFailure writes both),
	// and it names the same machine; it is withheld by the same proof. The
	// slice is cloned so nothing is written through into a shared backing array.
	if len(j.Steps) > 0 {
		steps := make([]api.Step, len(j.Steps))
		copy(steps, j.Steps)
		for i := range steps {
			if f := steps[i].Failure; f != nil && (!proven(f.Machine) || !proven(f.Runner)) {
				c := *f
				if !proven(c.Runner) {
					c.Runner = ""
				}
				if !proven(c.Machine) {
					c.Machine = ""
				}
				steps[i].Failure = &c
			}
		}
		j.Steps = steps
	}
	if j.Desk != nil && j.Desk.WorktreeMachine != "" && !proven(j.Desk.WorktreeMachine) {
		d := *j.Desk
		d.WorktreeMachine = ""
		j.Desk = &d
	}
	if p := j.Publishing; p != nil && p.Code == "machine_offline" && (j.Desk == nil || j.Desk.WorktreeMachine == "") {
		c := *p
		c.Detail = "Publishing waits for its machine"
		j.Publishing = &c
	}
}

// callersOnlyMachine is true when the caller has a registered machine with
// this id and nobody else does. A bare id says nothing about whose it was, so
// an id two people both registered (every laptop called "mbp") is never named.
func (h *Handler) callersOnlyMachine(caller, machine string) bool {
	if caller == "" || h.workers == nil {
		return false
	}
	mineSeen := false
	h.workers.mu.RLock()
	defer h.workers.mu.RUnlock()
	for _, row := range h.workers.credentials {
		name := strings.TrimSpace(row.Worker.Machine)
		if name == "" {
			name = row.Name
		}
		if name != machine {
			continue
		}
		if factory.NormalizeHandle(row.Worker.Handle) != caller {
			return false
		}
		mineSeen = true
	}
	return mineSeen
}
