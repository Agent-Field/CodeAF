package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
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
// returning only conversations already recorded on the job. It writes no job.
func (b *appBackend) FactoryJobChats(ctx context.Context, p appwire.FactoryJobChatsParams) (appwire.FactoryJobChats, error) {
	host, err := b.localHostOf(p.Factory)
	if err != nil {
		return appwire.FactoryJobChats{}, err
	}
	it, err := host.floor.Get(p.Job)
	if err != nil || !factory.IsIssueJob(it) {
		return appwire.FactoryJobChats{}, appwire.Errorf(appwire.CodeNotFound, "issue job not found")
	}
	if err := factory.RefuseDropped(it); err != nil {
		return appwire.FactoryJobChats{}, appwire.Errorf(appwire.CodeNotFound, "%s", err)
	}
	out := appwire.FactoryJobChats{Factory: p.Factory, Job: p.Job, Steps: []appwire.FactoryJobStepChat{}}
	if talk := strings.TrimSpace(it.Talk); talk != "" {
		out.Agent = appAgentID(talk)
	}
	if out.Agent != "" {
		if err := b.holdJobManager(host, p.Job, out.Agent); err != nil {
			return appwire.FactoryJobChats{}, err
		}
	}
	{
		// Every step's chat: the job's own, and those of the run under each
		// agentfield step ([factory.AllPhases]).
		for _, ph := range factory.AllPhases(it) {
			chat := strings.TrimSpace(ph.Chat)
			if chat == "" {
				continue
			}
			// A STEP WHOSE CHAT CANNOT BE OPENED IS LEFT OUT, never answered as one
			// the page would then fail to draw.
			if id := appAgentID(chat); jobChatErr(b.holdJobChat(id)) == nil {
				// Execution history is evidence, not a model preference. Opening a
				// finished stage must preserve its saved Main/Fallback pair too.
				out.Steps = append(out.Steps, appwire.FactoryJobStepChat{Step: ph.Name, Agent: id, WorkspaceRoot: chatWorkspaceRoot(chat), Rounds: b.roundChats(ph.Rounds)})
			}
		}
	}
	return out, nil
}

// holdJobManager binds future opens to the job's current effort without saving
// it, and a first open to the job's model.
func (b *appBackend) holdJobManager(host *appFactoryHost, job int, id string) error {
	a, err := b.find(id)
	if err != nil {
		return err
	}
	floor := host.floor
	b.locked(func() {
		a.jobEffort = func() string {
			it, err := floor.Get(job)
			if err != nil {
				return ""
			}
			return it.Effort
		}
		a.jobModel = func() string {
			it, err := floor.Get(job)
			if err != nil {
				return ""
			}
			return jobModelOf(it, host.defaultModel)
		}
	})
	_, err = b.holdJobChat(id)
	return err
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

// jobModelOf is the one model a job's sessions run on: the job's own when a
// person chose one, else the factory's default (def), which is also what a
// step with no effort word opens on ([stageOpenModel]) and what the queue tells
// the app as its `model`. "" is none known: the launch's own stands.
func jobModelOf(it factory.Item, def func() string) string {
	if model := strings.TrimSpace(it.Model); model != "" {
		return model
	}
	if def == nil {
		return ""
	}
	return strings.TrimSpace(def())
}

// jobManagerOpenModels is the pair a job's manager is first opened on, nil for
// no model known.
//
// A JOB'S SESSIONS RUN ON THE JOB'S MODEL (the owner, 2026-11-09): the page
// names one model for a job, its steps open on it, and so does its manager.
// The manager was opened on the app's own chat default instead, so a job
// showed two models, and a model chosen before the manager existed never
// reached it ([appBackend.jobAgentModel] moves only a manager already made).
//
// IT IS ONLY WHAT THE CONVERSATION STARTS WITH. A pair saved on the
// conversation wins on every later open ([enableAppConversationModels]), so a
// model a person then picks in the manager's own composer stands. The fallback
// is the app's, as for any conversation, unless it is the same model.
func jobManagerOpenModels(model, fallback string) *session.ConversationModels {
	model, fallback = strings.TrimSpace(model), strings.TrimSpace(fallback)
	if model == "" {
		return nil
	}
	if fallback == model {
		fallback = ""
	}
	return &session.ConversationModels{Main: model, Fallback: fallback}
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
	model := jobModelOf(it, host.defaultModel)
	if model != "" {
		if it.AutomaticModels != nil {
			_ = b.SetAutomaticModels(appAgentID(it.Talk), *it.AutomaticModels)
		}
		_, _ = b.SetModel(appAgentID(it.Talk), model)
	}
}

// chatWorkspaceRoot is the folder a step's chat writes its paths against:
// for a chat relayed from another machine, that machine's folder as its
// record named it (afRelayState.OriginCwd); else the cwd the record's own
// header names; "" when neither is known. A sealed cell names none.
func chatWorkspaceRoot(file string) string {
	if raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), afRelayFile)); err == nil {
		var st afRelayState
		if json.Unmarshal(raw, &st) == nil && strings.TrimSpace(st.OriginCwd) != "" {
			return st.OriginCwd
		}
	}
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	line, _ := bufio.NewReaderSize(f, 64<<10).ReadBytes('\n')
	var h struct {
		Type string `json:"type"`
		Cwd  string `json:"cwd"`
	}
	if json.Unmarshal(bytes.TrimSpace(line), &h) != nil || h.Type != "session" || strings.TrimSpace(h.Cwd) == "" {
		return ""
	}
	return filepath.Clean(strings.TrimSpace(h.Cwd))
}
