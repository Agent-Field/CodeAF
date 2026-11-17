package serve

// ── PUBLIC ACTS: THE DOORS AND THE CARRYING OUT ─────────────────────────────
//
// A job's manager proposes public acts on the job's own pull request through
// POST /v1/jobs/{id}/act, in any state of the job, and the person answers the
// ones that wait for them through acts_approve and acts_decline. The rules
// (what an act may be, who may say yes, what the set reads as) are
// internal/factory's public_act.go; this file is the three doors and the one
// sender.
//
// AN ACT IS CARRIED OUT OFF THE SETTLE PASS, ONE AT A TIME, with the job's
// person's GitHub sign-in ([Handler.settleActs]). The pass only finds what is
// due and starts the sender; the sender does the network work on its own
// goroutine, so a GitHub that hangs never holds a request.
//
// A REPLY IS NEVER POSTED TWICE. The act is marked running on the job before
// anything is sent and done or failed after. One sender works at a time on a
// factory, across processes (acts.lock), so an act that sender finds running
// was cut off mid-send by a service that is gone: it is failed with words that
// say to check the pull request, and never sent again.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/filelock"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// actFor bounds the sending of one act: a read, then at most a few writes.
const actFor = 90 * time.Second

// The sentences the doors refuse with.
const (
	actsSignIn  = "Sign in to GitHub first: public acts are posted with your own sign-in."
	actsNone    = "No act is waiting for approval."
	actsChanged = "The acts waiting for approval changed since you read them. Read the job and decide again."
)

// registerActs registers the three doors.
func (h *Handler) registerActs() {
	stored := h.config.Store != nil
	h.postJob(api.ActVerb, stored, h.act)
	h.postJob(api.ActsApproveVerb, stored, h.actsApprove)
	h.postJob(api.ActsDeclineVerb, stored, h.actsDecline)
}

// actVerb says verb is one of the three, for the lists postJob keeps.
func actVerb(verb string) bool {
	return verb == api.ActVerb || verb == api.ActsApproveVerb || verb == api.ActsDeclineVerb
}

// refuseAct answers an act the rules refuse: 422 for an act that is wrong in
// itself, 409 for a job that takes none now.
func (h *Handler) refuseAct(w http.ResponseWriter, it factory.Item, err error) {
	var refusal *factory.ActRefusal
	if errors.As(err, &refusal) && refusal.Invalid {
		h.refuse(w, http.StatusUnprocessableEntity, refusal.Words, "bad_input", "", nil, factory.JobState(it))
		return
	}
	h.refuse(w, http.StatusConflict, err.Error(), "act_refused", "", nil, factory.JobState(it))
}

// act is POST /v1/jobs/{id}/act: one proposal of one or more public acts.
// NOTHING IS KEPT OR SENT UNLESS EVERY ACT IN IT IS TAKEN: a proposal with
// one act the job refuses is refused whole, naming that act.
func (h *Handler) act(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body api.ActsRequest
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, api.ActVerb, body.Expect.Guard()) || !h.owns(w, r, it) {
		return
	}
	by := strings.TrimSpace(body.By)
	switch {
	case by != factory.ActByMainAgent && by != factory.ActByYou:
		h.fail(w, http.StatusUnprocessableEntity, `by must be "main_agent" or "you"`)
		return
	case len(body.Acts) == 0:
		h.fail(w, http.StatusUnprocessableEntity, "Name at least one act.")
		return
	case len(body.Acts) > factory.ActsAtOnce:
		h.fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("At most %d acts at once.", factory.ActsAtOnce))
		return
	}
	if err := factory.ActsRefusal(it); err != nil {
		h.refuseAct(w, it, err)
		return
	}
	if err := factory.ActsBudgetRefusal(it); err != nil {
		h.refuseAct(w, it, err)
		return
	}
	if token, _ := h.personGitHubToken(r); token == "" {
		h.fail(w, http.StatusForbidden, actsSignIn)
		return
	}
	// Every act is checked before any is carried out or kept.
	results := make([]api.ActResult, len(body.Acts))
	var keep []factory.PublicAct
	var kept []int
	press, merge := false, false
	for i, ask := range body.Acts {
		kind := strings.TrimSpace(ask.Kind)
		results[i].Kind = kind
		switch kind {
		case factory.ActKindPush, factory.ActKindOpenPR:
			road, words, err := factory.PushRoad(it, by)
			if err != nil {
				h.refuseAct(w, it, nthAct(len(body.Acts), i, kind, err))
				return
			}
			if road == factory.RoadPress {
				press = true
				results[i].State, results[i].Words = api.ActPressed, "The push is approved and goes out when its Undo window closes."
			} else {
				results[i].State, results[i].Words = api.ActWaitingOnYou, words
			}
		case factory.ActKindMerge:
			merge = true
		default:
			clean, err := factory.CleanAct(it, factory.PublicAct{Kind: kind, Title: ask.Title, Body: ask.Body, Base: ask.Base, Thread: ask.Thread, Text: ask.Text, Reviewers: ask.Reviewers})
			if err != nil {
				h.refuseAct(w, it, nthAct(len(body.Acts), i, kind, err))
				return
			}
			keep, kept = append(keep, clean), append(kept, i)
		}
	}
	now := h.clock().UTC()
	if press {
		if status, words := h.pressPublish(r, it, body.Why); status >= 300 {
			h.refuse(w, status, firstWords(words, "The push approval was refused."), "act_refused", "", nil, factory.JobState(it))
			return
		}
		if by == factory.ActByMainAgent {
			h.noteAct(it.ID, now, "said", "Auto: the main agent approved pushing the checked work.")
		}
	}
	if len(keep) > 0 {
		var stored []factory.PublicAct
		err := h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			// The job is read again under its lock: it may have ended, or lost
			// the thread an act names, since the proposal was checked.
			if err := factory.ActsRefusal(*cur); err != nil {
				return err
			}
			again := make([]factory.PublicAct, len(keep))
			for i, a := range keep {
				clean, err := factory.CleanAct(*cur, a)
				if err != nil {
					return nthAct(len(body.Acts), kept[i], a.Kind, err)
				}
				again[i] = clean
			}
			before := len(factory.ProposedActs(*cur))
			var err error
			if stored, err = factory.AddActs(cur, again, by, body.Why, now); err != nil {
				return err
			}
			if waiting := factory.ProposedActs(*cur); len(waiting) > before {
				logAct(cur, now, "ask", "Waiting for your approval: "+factory.ActsSummary(waiting)+".")
			}
			return nil
		})
		var refusal *factory.ActRefusal
		if errors.As(err, &refusal) {
			h.refuseAct(w, it, err)
			return
		}
		if h.failed(w, err) {
			return
		}
		for i, a := range stored {
			results[kept[i]].ID, results[kept[i]].State, results[kept[i]].Words = a.ID, a.State, actStateWords(a)
		}
	}
	if merge {
		// MERGE IS NEVER CARRIED OUT HERE. The job offers it to its person
		// again, on the merge card, and the merge stays their press.
		state, words := api.ActOffered, "Merging is the person's. The merge card is shown to them again."
		if err := h.config.Store.Update(it.ID, func(cur *factory.Item) error { return factory.OfferMerge(cur, now) }); err != nil {
			state, words = api.ActNotOffered, "Merging is the person's, and "+err.Error()+"."
		}
		for i := range results {
			if results[i].Kind == factory.ActKindMerge {
				results[i].State, results[i].Words = state, words
			}
		}
	}
	h.scheduleJobTimer()
	h.startActs()
	h.answerActs(w, it.ID, results)
}

// nthAct names which act of a proposal of several was refused.
func nthAct(of, i int, kind string, err error) error {
	var refusal *factory.ActRefusal
	if of <= 1 || !errors.As(err, &refusal) {
		return err
	}
	return &factory.ActRefusal{Words: fmt.Sprintf("Act %d (%s): %s Nothing was kept or sent.", i+1, kind, refusal.Words), Invalid: refusal.Invalid}
}

// actStateWords says where a stored act stands, in a line for its proposer.
func actStateWords(a factory.PublicAct) string {
	switch a.State {
	case factory.ActProposed:
		return "Waiting for the person's approval."
	case factory.ActApproved:
		if a.AfterPush != "" {
			return "Approved. It is carried out once the push has landed."
		}
		return "Approved. It is being carried out."
	case factory.ActRunning:
		return "It is being sent now."
	case factory.ActDone:
		return firstWords(a.Result, "Already done.")
	}
	return a.State
}

// pressPublish presses the job's waiting publish approval through the approve
// door itself, with every check that door makes, and answers its status and,
// for a refusal, its words.
func (h *Handler) pressPublish(r *http.Request, it factory.Item, words string) (int, string) {
	raw, _ := json.Marshal(api.Approve{Commit: it.HeadCommit, Words: words})
	inner := r.Clone(r.Context())
	inner.Body = io.NopCloser(bytes.NewReader(raw))
	inner.ContentLength = int64(len(raw))
	w := &replayWriter{header: make(http.Header)}
	h.approve(w, inner, it)
	if w.status() < 300 {
		return w.status(), ""
	}
	var refusal refusalBody
	_ = json.Unmarshal(w.body.Bytes(), &refusal)
	return w.status(), refusal.Error
}

// actsApprove is POST /v1/jobs/{id}/acts_approve: the person's one yes to
// every act that waits.
func (h *Handler) actsApprove(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body api.ActsApproveRequest
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, api.ActsApproveVerb, body.Expect.Guard()) || !h.owns(w, r, it) {
		return
	}
	if err := factory.ActsRefusal(it); err != nil {
		h.refuseAct(w, it, err)
		return
	}
	if len(factory.ProposedActs(it)) == 0 {
		h.refuse(w, http.StatusConflict, actsNone, "conflict", "", nil, factory.JobState(it))
		return
	}
	if token, _ := h.personGitHubToken(r); token == "" {
		h.fail(w, http.StatusForbidden, actsSignIn)
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if who = factory.NormalizeHandle(who); who == "" {
		h.fail(w, http.StatusUnprocessableEntity, "Sign in to GitHub before approving these.")
		return
	}
	now := h.clock().UTC()
	var approved []factory.PublicAct
	err = h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		if err := factory.ActsRefusal(*cur); err != nil {
			return err
		}
		approved = factory.ProposedActs(*cur)
		if len(approved) == 0 {
			return &factory.ActRefusal{Words: actsNone}
		}
		if len(body.IDs) > 0 && !sameActs(approved, body.IDs) {
			return &factory.ActRefusal{Words: actsChanged}
		}
		factory.ApproveActs(cur, who, false, now)
		logAct(cur, now, "said", "@"+who+" approved: "+factory.ActsSummary(approved)+".")
		return nil
	})
	var refusal *factory.ActRefusal
	if errors.As(err, &refusal) {
		h.refuseAct(w, it, err)
		return
	}
	if h.failed(w, err) {
		return
	}
	if h.config.Record != nil {
		_, _ = h.config.Record.Append(r.Context(), record.Event{Kind: record.ActsApproved, Job: it.ID, Handle: who, Detail: factory.ActsSummary(approved)})
	}
	h.scheduleJobTimer()
	h.startActs()
	h.answerActs(w, it.ID, actResults(h, it.ID, approved))
}

// actsDecline is POST /v1/jobs/{id}/acts_decline: the person's one no. The
// acts are never sent, and their words are kept for the job's manager.
func (h *Handler) actsDecline(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body api.ActsDeclineRequest
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, api.ActsDeclineVerb, body.Expect.Guard()) || !h.owns(w, r, it) {
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	who = factory.NormalizeHandle(who)
	now := h.clock().UTC()
	words := strings.TrimSpace(body.Words)
	var declined []factory.PublicAct
	err = h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		declined = factory.ProposedActs(*cur)
		if len(declined) == 0 {
			return &factory.ActRefusal{Words: actsNone}
		}
		factory.DeclineActs(cur, words, now)
		line := "Declined: " + factory.ActsSummary(declined) + "."
		if who != "" {
			line = "@" + who + " declined: " + factory.ActsSummary(declined) + "."
		}
		if words != "" {
			line += " " + words
		}
		logAct(cur, now, "said", line)
		return nil
	})
	var refusal *factory.ActRefusal
	if errors.As(err, &refusal) {
		h.refuseAct(w, it, err)
		return
	}
	if h.failed(w, err) {
		return
	}
	if h.config.Record != nil {
		_, _ = h.config.Record.Append(r.Context(), record.Event{Kind: record.ActsDeclined, Job: it.ID, Handle: who, Detail: strings.TrimSpace(factory.ActsSummary(declined) + " · " + words)})
	}
	h.answerActs(w, it.ID, actResults(h, it.ID, declined))
}

// sameActs says ids names exactly the acts given.
func sameActs(acts []factory.PublicAct, ids []string) bool {
	if len(acts) != len(ids) {
		return false
	}
	for _, a := range acts {
		if !slices.Contains(ids, a.ID) {
			return false
		}
	}
	return true
}

// actResults is where each of acts stands on the job now.
func actResults(h *Handler, id int, acts []factory.PublicAct) []api.ActResult {
	cur, err := h.config.Store.Get(id)
	out := make([]api.ActResult, 0, len(acts))
	for _, a := range acts {
		if err == nil {
			for _, now := range cur.Acts {
				if now.ID == a.ID {
					a = now
					break
				}
			}
		}
		words := actStateWords(a)
		if a.State == factory.ActDeclined {
			words = "Declined. It will not be sent."
		}
		out = append(out, api.ActResult{ID: a.ID, Kind: a.Kind, State: a.State, Words: words})
	}
	return out
}

// answerActs answers a door with the job's acts and wait as they now stand.
func (h *Handler) answerActs(w http.ResponseWriter, id int, results []api.ActResult) {
	out := api.ActsAnswer{Factory: h.factory, Job: id, Results: results}
	if cur, err := h.config.Store.Get(id); err == nil {
		out.Acts, out.Wait = cur.Acts, cur.Wait
		out.Waiting = factory.ActsSummary(factory.ProposedActs(cur))
	}
	h.write(w, http.StatusOK, out)
}

// logAct writes one line about the job's acts on its log and its activity.
func logAct(it *factory.Item, now time.Time, tone, text string) {
	it.Note(now, text)
	if it.Stream != nil {
		it.Stream.Log = append(it.Stream.Log, factory.LogLine{At: now, Tone: tone, Text: text})
	}
}

// noteAct is [logAct] as a write of its own.
func (h *Handler) noteAct(id int, now time.Time, tone, text string) {
	_ = h.config.Store.Update(id, func(cur *factory.Item) error {
		logAct(cur, now, tone, text)
		return nil
	})
}

// approveActsWithPush is the publish approval's own yes reaching the acts that
// wait beside it: ONE APPROVAL COVERS THE PUSH AND THE SET. The approve door
// calls it in the write that records the approval; the acts then follow that
// push, and run once it has landed.
func approveActsWithPush(cur *factory.Item, who string, now time.Time) {
	waiting := factory.ProposedActs(*cur)
	if len(waiting) == 0 {
		return
	}
	factory.ApproveActs(cur, factory.NormalizeHandle(who), true, now)
	logAct(cur, now, "said", "Approved with the push: "+factory.ActsSummary(waiting)+". Carried out once the push has landed.")
}

// ── THE ONE SENDER ──────────────────────────────────────────────────────────

// actRun is the sender's state on one handler: whether it is working, and the
// context that ends its call when the service closes.
type actRun struct {
	mu     sync.Mutex
	busy   bool
	ctx    context.Context
	cancel context.CancelFunc
}

// stop ends the act being sent. Close calls it before it waits for the sender.
func (a *actRun) stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

// errNoAct ends a write that found nothing to do, before anything is written.
var errNoAct = errors.New("no act is due")

// settleActs is the settle pass's look at public acts on one listing of the
// floor: each job's acts are put right against the job as it stands
// ([factory.SettleActs]), and the sender is started when any is due. It
// answers the soonest moment an act comes due by the clock alone, zero for
// none, so a floor that is otherwise idle is looked at again then.
func (h *Handler) settleActs(items []factory.Item) (next time.Time) {
	if h.config.Store == nil {
		return next
	}
	now := h.clock().UTC()
	due := false
	for _, it := range items {
		if len(it.Acts) == 0 {
			continue
		}
		probe := it
		if factory.SettleActs(&probe, now) {
			_ = h.config.Store.Update(it.ID, func(cur *factory.Item) error {
				if !settleActsLogged(cur, now) {
					return errNoAct
				}
				return nil
			})
		}
		at := factory.ActsDueAt(probe, now)
		if at.IsZero() {
			continue
		}
		if !at.After(now) {
			due = true
		}
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	if due {
		h.startActs()
	}
	return next
}

// settleActsLogged is [factory.SettleActs] with a line on the job's log for
// each act it failed.
func settleActsLogged(cur *factory.Item, now time.Time) bool {
	before := map[string]string{}
	for _, a := range cur.Acts {
		before[a.ID] = a.State
	}
	if !factory.SettleActs(cur, now) {
		return false
	}
	for _, a := range cur.Acts {
		if a.State == factory.ActFailed && before[a.ID] != factory.ActFailed {
			logAct(cur, now, "fail", "Not sent: "+factory.ActWords(a)+". "+a.Error)
		}
	}
	return true
}

// startActs starts the sender unless it is working or the service is closing.
func (h *Handler) startActs() {
	if h.config.Store == nil {
		return
	}
	h.jobsMu.Lock()
	if h.jobsClosed {
		h.jobsMu.Unlock()
		return
	}
	a := &h.acts
	a.mu.Lock()
	if a.busy {
		a.mu.Unlock()
		h.jobsMu.Unlock()
		return
	}
	a.busy = true
	if a.ctx == nil {
		a.ctx, a.cancel = context.WithCancel(context.Background())
	}
	ctx := a.ctx
	a.mu.Unlock()
	h.jobsBusy.Add(1)
	h.jobsMu.Unlock()
	go func() {
		defer h.jobsBusy.Done()
		h.carryOutActs(ctx)
		a.mu.Lock()
		a.busy = false
		a.mu.Unlock()
		// An act approved while this pass was ending, and the next try of one
		// GitHub did not answer, are the timer's.
		h.scheduleJobTimer()
	}()
}

// lockActs takes the factory's one sender lock without waiting. ok is false
// when another sender holds it, here or in another process on this store.
func (h *Handler) lockActs() (unlock func(), ok bool) {
	f, err := os.OpenFile(filepath.Join(h.config.Store.Root(), "acts.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false
	}
	if err := filelock.Lock(f, true, true); err != nil {
		f.Close()
		return nil, false
	}
	return func() { _ = filelock.Unlock(f); _ = f.Close() }, true
}

// carryOutActs sends every act that is due, one at a time, job by job, until
// none is.
func (h *Handler) carryOutActs(ctx context.Context) {
	unlock, ok := h.lockActs()
	if !ok {
		return
	}
	defer unlock()
	// The bound is against a fault that would otherwise spin: every act is
	// begun at most once per try, and tries are few.
	for pass := 0; pass < 500 && ctx.Err() == nil; pass++ {
		items, err := h.config.Store.ListShared()
		if err != nil {
			return
		}
		worked := false
		for _, it := range items {
			if len(it.Acts) == 0 || ctx.Err() != nil {
				continue
			}
			if h.carryOutNext(ctx, it.ID) {
				worked = true
			}
		}
		if !worked {
			return
		}
	}
}

// carryOutNext sends the job's next due act and answers whether it began one.
// The caller holds the sender lock.
func (h *Handler) carryOutNext(ctx context.Context, id int) bool {
	now := h.clock().UTC()
	var it factory.Item
	var act factory.PublicAct
	begun := false
	err := h.config.Store.Update(id, func(cur *factory.Item) error {
		// THIS IS THE ONE SENDER, so an act that reads running was cut off: by
		// a service that quit or died mid-send. It is failed, never resent.
		cut := factory.CutOffActs(cur, now)
		for _, a := range cut {
			logAct(cur, now, "fail", "Not confirmed: "+factory.ActWords(a)+". "+factory.ActCutOff)
		}
		settled := settleActsLogged(cur, now)
		next, ok := factory.NextAct(*cur, now)
		if ok && factory.BeginAct(cur, next.ID, now) {
			act, it, begun = next, *cur, true
			return nil
		}
		if len(cut) == 0 && !settled {
			return errNoAct
		}
		return nil
	})
	if err != nil || !begun {
		return false
	}
	done, err := h.sendAct(ctx, it, act)
	now = h.clock().UTC()
	var failure *factorygithub.ActFailure
	posted := false
	_ = h.config.Store.Update(id, func(cur *factory.Item) error {
		switch {
		case err == nil:
			if !factory.FinishAct(cur, act.ID, done.URL, done.Words, now) {
				return errNoAct
			}
			if pr := cur.PullRequest; done.Change != nil && pr != nil && it.PullRequest != nil && pr.Number == it.PullRequest.Number {
				cp := *pr
				done.Change(&cp)
				cur.PullRequest = &cp
			}
			line := done.Words
			if done.URL != "" && act.Kind != factory.ActKindEditPR && act.Kind != factory.ActKindChangeBase {
				line += " " + done.URL
			}
			logAct(cur, now, "ok", line)
			posted = true
		case errors.As(err, &failure) && failure.How == factorygithub.ActNotSent:
			was, failed := factory.RetryAct(cur, act.ID, failure.Words, now)
			switch {
			case !was:
				return errNoAct
			case failed:
				logAct(cur, now, "fail", fmt.Sprintf("Not sent: %s. %s It was tried %d times.", factory.ActWords(act), failure.Words, factory.ActTriesMost))
			case act.Tries == 0:
				logAct(cur, now, "said", "Not sent yet: "+factory.ActWords(act)+". "+failure.Words+" It will be tried again.")
			}
		default:
			words := "It could not be sent."
			lead := "Not sent: "
			if errors.As(err, &failure) {
				words = failure.Words
				if failure.How == factorygithub.ActUnknown {
					lead = "Not confirmed: "
				}
			}
			if !factory.FailAct(cur, act.ID, words, now) {
				return errNoAct
			}
			logAct(cur, now, "fail", lead+factory.ActWords(act)+". "+words)
		}
		return nil
	})
	if posted {
		h.recordAct(it, act, done)
	}
	return true
}

// sendAct carries one act out on GitHub with the job's person's sign-in.
func (h *Handler) sendAct(ctx context.Context, it factory.Item, act factory.PublicAct) (factorygithub.ActDone, error) {
	// A repository switched off since the yes has nothing sent to it.
	if sw, err := h.repoSwitch(); err == nil && sw.OffJob(it) {
		return factorygithub.ActDone{}, &factorygithub.ActFailure{How: factorygithub.ActRefused, Words: "This repository was switched off, so nothing is sent to it."}
	}
	token := h.actsToken(ctx, it)
	if token == "" {
		return factorygithub.ActDone{}, &factorygithub.ActFailure{How: factorygithub.ActNotSent, Words: "The job's person is not signed in to GitHub."}
	}
	ctx, cancel := context.WithTimeout(ctx, actFor)
	defer cancel()
	var client factorygithub.API
	if h.config.ActsClient != nil {
		client = h.config.ActsClient(token)
	} else {
		httpClient := h.githubClient()
		client = forge.NewClient(token).WithHTTPClient(&httpClient)
	}
	return factorygithub.New(client, []string{factory.RepoOf(it)}, h.clock).Act(ctx, it, act)
}

// actsToken is the GitHub sign-in the job's acts are carried out with: its
// person's own, kept while they have a live session here. A solo host is its
// one person, and uses its profile's sign-in; a team never falls back to the
// host's.
func (h *Handler) actsToken(ctx context.Context, it factory.Item) string {
	if person := it.Person(); person != "" && h.viewer != nil {
		if token := h.viewer.token(person); token != "" {
			return token
		}
	}
	if h.mode() == api.FactorySolo && h.config.GitHubToken != nil {
		return h.config.GitHubToken(ctx)
	}
	return ""
}

// recordAct writes the act that was carried out into the factory's record.
func (h *Handler) recordAct(it factory.Item, act factory.PublicAct, done factorygithub.ActDone) {
	if h.config.Record == nil {
		return
	}
	event := record.Event{Kind: record.PRActed, Job: it.ID, Handle: it.Person(), Verb: actRecordVerbs[act.Kind], Detail: strings.TrimSpace(done.Words + " " + done.URL)}
	if act.Kind == factory.ActKindMarkReady {
		event.Kind, event.Verb = record.PRReady, ""
	}
	if act.ApprovedBy == factory.ActByMainAgent {
		// Auto: the job's manager said yes, and the person's sign-in posted it.
		event.Actor = record.Actor{Kind: "agent", Name: "main agent"}
		event.Detail += " · Auto mode"
	}
	_, _ = h.config.Record.Append(context.Background(), event)
}

// actRecordVerbs says each kind of act as the record's Activity verb.
var actRecordVerbs = map[string]string{
	factory.ActKindEditPR:          "edited pull request",
	factory.ActKindChangeBase:      "changed base",
	factory.ActKindReply:           "replied",
	factory.ActKindComment:         "commented",
	factory.ActKindResolveThread:   "resolved review thread",
	factory.ActKindRerequestReview: "asked for review",
	factory.ActKindRerunChecks:     "reran checks",
}
