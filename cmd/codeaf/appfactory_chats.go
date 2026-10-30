package main

import (
	"context"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── A JOB'S CONVERSATIONS, ON THE JOB'S OWN PAGE ───────────────────────────
//
// The app shows a factory job's conversations where the job is: the job's
// agent (the item's own conversation, [talkMaker]), the latest chat of each
// step that ran, and the chat of each of that step's finished rounds. They are read from the factory's own record of the job on the
// floor this engine serves, never guessed from a team's names, and each is held
// open the way a crew's members are, so the page draws it at once. A running
// step is lent, as it is to any window (app_lend.go).
//
// Reading chats never sets their models. A job's model supplies an initial
// default; each saved conversation's Main/Fallback choices remain authoritative.

var _ appwire.FactoryJobChatter = (*appBackend)(nil)

// FactoryJobChats answers one job's conversations on this machine's factory,
// making the job's agent when it has none yet. It starts no job and asks no
// model.
func (b *appBackend) FactoryJobChats(ctx context.Context, p appwire.FactoryJobChatsParams) (appwire.FactoryJobChats, error) {
	host, err := b.localHostOf(p.Factory)
	if err != nil {
		return appwire.FactoryJobChats{}, err
	}
	if host.seam.Talk == nil {
		return appwire.FactoryJobChats{}, appwire.Errorf(appwire.CodeEngine, "this factory cannot make a job's conversation here")
	}
	item, err := host.floor.Get(p.Job)
	if err != nil || !factory.IsIssueJob(item) {
		return appwire.FactoryJobChats{}, appwire.Errorf(appwire.CodeNotFound, "issue job not found")
	}
	talk, err := host.seam.Talk(ctx, p.Job)
	if err != nil {
		return appwire.FactoryJobChats{}, appwire.Errorf(appwire.CodeNotFound, "the job's conversation could not be opened: %s", err.Error())
	}
	it, err := host.floor.Get(p.Job)
	if err != nil {
		return appwire.FactoryJobChats{}, appwire.Errorf(appwire.CodeNotFound, "there is no job %d in this factory", p.Job)
	}
	out := appwire.FactoryJobChats{Factory: p.Factory, Job: p.Job, Agent: appAgentID(talk), Steps: []appwire.FactoryJobStepChat{}}
	held, err := b.holdJobChat(out.Agent)
	if err != nil {
		return appwire.FactoryJobChats{}, err
	}
	// The effort is set through an open, so a closed job's agent keeps its own.
	if held && it.Effort != "" {
		_, _ = b.SetEffort(out.Agent, it.Effort)
	}
	if it.Stream != nil {
		for _, ph := range it.Stream.Phases {
			chat := strings.TrimSpace(ph.Chat)
			if chat == "" {
				continue
			}
			// A STEP WHOSE CHAT CANNOT BE OPENED IS LEFT OUT, never answered as one
			// the page would then fail to draw.
			if id := appAgentID(chat); jobChatErr(b.holdJobChat(id)) == nil {
				// Execution history is evidence, not a model preference. Opening a
				// finished stage must preserve its saved Main/Fallback pair too.
				out.Steps = append(out.Steps, appwire.FactoryJobStepChat{Step: ph.Name, Agent: id, Rounds: b.roundChats(ph.Rounds)})
			}
		}
	}
	return out, nil
}

// roundChats is each finished round's chat that can be opened, held open as
// the step's own is. The same rule leaves a round out as a step: one whose chat
// cannot be opened is never answered.
func (b *appBackend) roundChats(rounds []factory.RoundLine) []appwire.FactoryJobRoundChat {
	var out []appwire.FactoryJobRoundChat
	for _, r := range rounds {
		chat := strings.TrimSpace(r.Chat)
		if chat == "" {
			continue
		}
		if id := appAgentID(chat); jobChatErr(b.holdJobChat(id)) == nil {
			out = append(out, appwire.FactoryJobRoundChat{Round: r.Round, Agent: id})
		}
	}
	return out
}

// holdJobChat holds one of a job's conversations open, and tells the app when
// it had to open it. A CHAT THE PERSON CLOSED IS NAMED BUT LEFT CLOSED: viewing
// the job's page is not an open, and an open takes it out of Closed
// ([appBackend.keepOpen]); held says which. The app is told it as it stands,
// closed, since a job's chats are never listed and the page draws it from that.
func (b *appBackend) holdJobChat(id string) (held bool, err error) {
	a, err := b.find(id)
	if err != nil {
		return false, err
	}
	if a == nil {
		return false, appwire.Errorf(appwire.CodeNotFound, "there is no conversation with id %s", id)
	}
	b.locked(func() { a.view.FactoryOwned = true })
	view, opened, err := b.keepOpen(a)
	if errors.Is(err, errAppKeptClosed) {
		b.sayKeptClosed(a)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if opened {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	return true, nil
}

// sayKeptClosed tells the app of an agent left in Closed or the archive, as
// [appBackend.sayAgent] does after a close: in a process that never closed it
// the view does not know it was archived, so the mark says.
func (b *appBackend) sayKeptClosed(a *appAgent) {
	var view appwire.Agent
	b.locked(func() { view = a.view })
	archived := view.State == appwire.StateArchived
	_ = b.state.read(func(f *appStateFile) {
		_, marked := f.Archived[view.ID]
		archived = archived || marked
	})
	b.tidyMarks(&view, archived)
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
}

// jobChatErr is holdJobChat's error alone, for a caller that names a chat
// whether or not it is held.
func jobChatErr(_ bool, err error) error { return err }

// localHostOf is the host of this machine's factory when id names it and this
// engine serves it, or why not.
func (b *appBackend) localHostOf(id string) (*appFactoryHost, error) {
	l := &b.local
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.closed:
		return nil, appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	case !l.running || l.host == nil || id != l.id:
		return nil, appwire.Errorf(appwire.CodeNotFound, "this factory's chats are on the machine that runs its jobs")
	}
	return l.host, nil
}

// jobAgentModel puts a job's agent on the job's model after a person chose it:
// the job's own, or the factory's default when they went back to it. It
// touches only an agent this factory made and only on this machine.
func (b *appBackend) jobAgentModel(factoryID string, job int) {
	host, err := b.localHostOf(factoryID)
	if err != nil {
		return
	}
	it, err := host.floor.Get(job)
	if err != nil || strings.TrimSpace(it.Talk) == "" {
		return
	}
	model := strings.TrimSpace(it.Model)
	if model == "" {
		model = host.defaultModel()
	}
	if model != "" {
		if it.AutomaticModels != nil {
			_ = b.SetAutomaticModels(appAgentID(it.Talk), *it.AutomaticModels)
		}
		_, _ = b.SetModel(appAgentID(it.Talk), model)
	}
}
