package main

// ── codeaf app: DISMISS, THE INBOX'S FIFTH VERB ────────────────────────────
//
// The owner's decision of 2026-11-16: a person may dismiss an Inbox item. The
// item moves to Done with a receipt ("Dismissed · you · 14:05"), its question
// closes with nothing decided, and its agent is told it was dismissed, so it
// can ask again later if it still needs to.
//
// EACH KIND CLOSES THROUGH ITS OWN DOOR, so the receipt, the Inbox and every
// other surface read a dismissal the way they read any other ending:
//
//	the model's own question   withdrawn by the conversation (session's
//	                           askdismiss.go); a call parked on it, or the
//	                           model's next step, reads that it was dismissed
//	any other question         answered with its one answer that changes
//	                           nothing (session.AnswerOption.Safe: deny, not
//	                           now, don't save …); one with no such answer is
//	                           refused, because dismissing it would decide
//	an order's run             skipped, as "Skip this one" does
//	a crew's decision          decided as dismissed, in the person's name; the
//	                           raiser is told so, as with any decision
//	a crew proposal            let go, as keeping the agent alone does
//	a budget ask               closed; the budget stays as it is
//	a kept-running process     its line closes; the process is left as it is
//	                           and this episode's line does not open again,
//	                           even after a restart (the app's state file)
//	a Library choice           refused: it is the Library guide's choice, owned
//	                           by no agent, and it stands until the guide
//	                           settles it

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appDismissedWords is what a crew's raiser reads as the person's decision on
// a packet they dismissed.
const appDismissedWords = "Dismissed by the person without a decision; raise it again later if it is still needed."

// appDismissedNote is the words a dismissed question's answer carries to its
// lane, where the lane passes words on. It says plainly that the person put the
// question away and decided nothing, so no lane reads it as a no, a change or a
// rule about later requests.
const appDismissedNote = "Dismissed from the Inbox without an answer: the person put the question away and decided nothing. This says nothing about later requests."

// appCrewDismissed is the crew asks this app dismissed, so the packet's
// closing says dismissed rather than decided.
var appCrewDismissed sync.Map

// appDismissedReceipt is the receipt a dismissed item leaves.
func appDismissedReceipt(at time.Time) string {
	return "Dismissed · you · " + at.Local().Format("15:04")
}

// DismissInbox is inbox.dismiss: it closes the item's question with nothing
// decided and moves the item to done.
func (b *appBackend) DismissInbox(id string) error {
	if strings.HasPrefix(id, "library:") {
		return appwire.Errorf(appwire.CodeBadParams, "a Library choice is made in the Library guide and cannot be dismissed; put it aside with Later")
	}
	agent, ask, ok := appInboxSplit(id)
	if !ok {
		return appwire.Errorf(appwire.CodeBadParams, "%s is not an Inbox item", id)
	}
	in := b.inbox
	if in == nil {
		return appwire.Errorf(appwire.CodeNotFound, "there is no Inbox item %s", id)
	}
	var refused error
	in.locked(func() {
		switch {
		case in.items[id] == nil || in.closing[id].ID != "":
			refused = appwire.Errorf(appwire.CodeNotFound, "the Inbox item %s is no longer waiting", id)
		case in.held[id] != nil:
			refused = appwire.Errorf(appwire.CodeBusy, "the Inbox item %s is being answered; take the answer back first", id)
		default:
			in.from[id] = "inbox"
		}
	})
	if refused != nil {
		return refused
	}
	err := b.dismissAsk(id, agent, ask)
	if err != nil {
		in.locked(func() { delete(in.from, id) })
	}
	return err
}

// dismissAsk closes one ask through the door its kind closes by.
func (b *appBackend) dismissAsk(id, agent, ask string) error {
	switch {
	case appRunAsk(ask):
		return appwire.Errorf(appwire.CodeBadParams, "choose Approve or Decline for a run approval")
	case strings.HasPrefix(agent, appwire.AppDraftInboxPrefix) && ask == appBuildInboxAsk:
		b.inbox.locked(func() {
			b.inbox.closing[id] = appwire.AskClosed{ID: ask, Option: appwire.ResolvedDismissed, At: b.now()}
		})
		b.inboxResolved(id, appwire.Entry{})
		return nil
	case strings.HasPrefix(ask, "service:"):
		return b.dismissServiceLine(id)
	case strings.HasPrefix(ask, appwire.OrderAskPrefix):
		return b.dismissOrderRun(ask)
	case strings.HasPrefix(ask, appCrewAskPrefix):
		appCrewDismissed.Store(ask, true)
		if _, err := b.CrewDecide(strings.TrimPrefix(ask, appCrewAskPrefix), "", appDismissedWords, ""); err != nil {
			appCrewDismissed.Delete(ask)
			return err
		}
		return nil
	case strings.HasPrefix(ask, appProposalAskPrefix):
		return b.dismissProposal(agent, strings.TrimPrefix(ask, appProposalAskPrefix))
	}
	return b.dismissQuestion(agent, ask)
}

// dismissOrderRun skips the run, as its own "Skip this one" does. The mark is
// set first, so the run's closing says dismissed whenever the store is read.
func (b *appBackend) dismissOrderRun(ask string) error {
	run, err := strconv.ParseInt(strings.TrimPrefix(ask, appwire.OrderAskPrefix), 10, 64)
	if err != nil {
		return appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open", ask)
	}
	store, err := b.ordersOf()
	if err != nil {
		return err
	}
	r, err := store.Run(run)
	if err != nil {
		return appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open", ask)
	}
	// Dismissed already (a second press before the next orders poll closes the
	// ask): nothing more to do, and the first press's mark must stay.
	if was, ok := appOrderAnswered.Load(run); ok && was == appwire.ResolvedDismissed {
		return nil
	}
	appOrderAnswered.Store(run, appwire.ResolvedDismissed)
	if _, err := b.OrderVerdict(r.AutomationID, run, appwire.VerdictSkip); err != nil {
		appOrderAnswered.Delete(run)
		return err
	}
	return nil
}

// dismissProposal lets a crew proposal go, as keeping the agent alone does.
func (b *appBackend) dismissProposal(agent, proposal string) error {
	run := b.crews()
	if run == nil {
		return appwire.Errorf(appwire.CodeNotFound, "the proposal %s is no longer waiting", proposal)
	}
	open := false
	run.locked(func() {
		ask, ok := run.asks[appProposalAskPrefix+proposal]
		open = ok && ask.Agent == agent
	})
	if !open {
		return appwire.Errorf(appwire.CodeNotFound, "the proposal %s is no longer waiting", proposal)
	}
	b.dropProposal(proposal, appwire.ResolvedDismissed)
	return nil
}

// dismissServiceLine closes a kept-running process's line and leaves the
// process as it is; the same episode's line does not open again, in this run
// of the app or the next ([appStateFile.ServiceDismissed]).
func (b *appBackend) dismissServiceLine(id string) error {
	svc, err := b.servicesOf()
	if err != nil {
		return err
	}
	var key string
	var line appServiceLine
	found := false
	svc.locked(func() {
		for k, have := range svc.lines {
			if have.item == id {
				key, line, found = k, have, true
				svc.dismissed[k] = have.episode
				return
			}
		}
	})
	if !found {
		return appwire.Errorf(appwire.CodeNotFound, "the Inbox item %s is no longer waiting", id)
	}
	b.saveServiceDismissed(key, line.episode)
	b.serviceLineResolved(svc, key, line, appwire.ResolvedDismissed, b.now())
	return nil
}

// saveServiceDismissed keeps a process's dismissed episode in the app's
// file, or forgets it when episode is "", so a restart does not open the line
// again. A file that cannot be written leaves the dismissal standing for this
// run of the app.
func (b *appBackend) saveServiceDismissed(key, episode string) {
	if b.state == nil {
		return
	}
	_ = b.state.write(func(f *appStateFile) error {
		if episode == "" {
			delete(f.ServiceDismissed, key)
			return nil
		}
		if f.ServiceDismissed == nil {
			f.ServiceDismissed = map[string]string{}
		}
		f.ServiceDismissed[key] = episode
		return nil
	})
}

// dismissQuestion closes one of a held conversation's asks: a budget ask, the
// model's own question, or any other question by its answer that changes
// nothing.
func (b *appBackend) dismissQuestion(agent, ask string) error {
	a, endRead, err := b.findCrewOwner(agent)
	if err != nil {
		return err
	}
	defer endRead()
	live := b.liveOf(a)
	if live == nil {
		return appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open", ask)
	}
	var money appMoneyAsk
	var q session.Question
	isMoney, open := false, false
	b.locked(func() {
		money, isMoney = live.money[ask]
		q, open = live.questions[ask]
	})
	if isMoney {
		if !b.closeMoneyAsks(a, money.scope, appwire.ResolvedDismissed, "Dismissed") {
			return appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open", ask)
		}
		return nil
	}
	if !open {
		return appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open", ask)
	}
	if q.Kind == session.QuestionAsk {
		dismisser, ok := live.conn.Conv.(interface{ DismissQuestion(string) error })
		if !ok {
			return appwire.Errorf(appwire.CodeEngine, "this conversation cannot dismiss a question")
		}
		b.locked(func() { live.answered[ask] = appAnsweredMark + appwire.ResolvedDismissed })
		if err := dismisser.DismissQuestion(q.Token()); err != nil {
			b.locked(func() { delete(live.answered, ask) })
			return appEngineError(err)
		}
		return nil
	}
	// A landing's or a conflict's cards all decide the task: its "not right"
	// is marked safe, but it refutes the task and fails what depends on it.
	if q.Kind == session.QuestionLanding || q.Kind == session.QuestionConflict {
		return appwire.Errorf(appwire.CodeBadParams, "every answer to this question decides the task, so dismissing it would decide it; answer it instead")
	}
	safe, ok := appSafeOption(q)
	if !ok {
		return appwire.Errorf(appwire.CodeBadParams, "this question has no answer that changes nothing, so dismissing it would decide it; answer it instead")
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	b.locked(func() { live.answered[ask] = appAnsweredMark + appwire.ResolvedDismissed })
	resolved := session.Answer{Kind: q.Kind, ID: q.ID, Ref: q.Ref, Ask: q.Ask, Key: safe, Picked: []string{safe}, Change: appDismissedNote, Dismissed: true}
	if err := live.conn.Conv.ResolveQuestion(resolved); err != nil {
		b.locked(func() { delete(live.answered, ask) })
		return appEngineError(err)
	}
	return nil
}

// appSafeOption is the key of a question's one answer that changes nothing,
// and false when it has none.
func appSafeOption(q session.Question) (string, bool) {
	for _, option := range q.Options {
		if option.Safe {
			return option.Key, true
		}
	}
	return "", false
}
