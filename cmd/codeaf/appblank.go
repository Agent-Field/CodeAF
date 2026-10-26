package main

// ── codeaf app: BLANK CONVERSATIONS ─────────────────────────────────────────
//
// A BLANK CONVERSATION IS ONE NOBODY HAS SAID ANYTHING IN: neither the person
// nor a crew's brief. agents.create with no words makes one, and its folder is
// on disk before anybody speaks, because the create answers with the agent's
// lasting id at once and that id is the folder's name: a crew adds a member by
// its journal's path before the member's brief reaches it, so the folder
// cannot wait for the first message. What can be true instead is that a blank
// conversation is neither kept nor listed:
//
//   - agents.list, and search.all which reads it, never list one, and
//     agents.create and the open behind it do not announce one. The person's
//     first words, or the name a manager's start gives a member, are what
//     bring an agent to a window.
//   - At quit the app takes away each blank conversation it made, judged by
//     the rule the terminal reaps an empty launch with
//     ([v3EmptyUnassignedSession]): nothing said in its journal, no saved
//     tasks, no work, worktrees or deliverables. A crew naming it, or a window
//     holding it, keeps it.
//
// A CREW MEMBER A MANAGER STARTED IS NOT BLANK. Its brief is its first message
// (S2 names an agent by "the first words of the first message"), and removing
// a member never deletes the agent, so it stays in its crew and in the
// list. The person never spoke in it, so it has no last_active_at, and it goes
// by the word its crew calls it, its brief's first words, when its folder's
// row has no name: a conversation does not name itself from a crew's note.

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// saidLocked reports whether this process knows that somebody has said
// something in a's conversation: the person's words sent, or held for a retry
// after a start that failed, a turn at work or a question out, or a crew's
// brief on its way to a member a manager started. Callers hold b.mu.
func (a *appAgent) saidLocked() bool {
	if a.users > 0 || a.failed != "" || a.briefed {
		return true
	}
	if a.view.State == appwire.StateWorking || a.view.State == appwire.StateNeedsYou {
		return true
	}
	live := a.live
	return live != nil && (live.running || len(live.asks) > 0)
}

// appSpokenIn reports whether a conversation's journal holds anything said in
// it: the person's words, or a crew's brief, which the journal keeps as the
// same kind of line. A journal that cannot be read is not called blank, for
// the reason [v3EmptySession] gives: a failed parse is not silence.
func appSpokenIn(transcript string) bool {
	if strings.TrimSpace(transcript) == "" {
		return false
	}
	spoken, sure := session.SpokeIn(transcript)
	return spoken || !sure
}

// memberNames is the word each crew calls a member by, keyed by agent id: the
// first words of its brief for a member a manager started, the agent's own
// name for one that was added. Every crew is read, open or finished, and a
// former member is named too, because removing a member never deletes it; a
// crew it is in now outranks one it left.
func (b *appBackend) memberNames() map[string]string {
	f, err := teamstore.Snapshot(b.crewProfile())
	if err != nil || f == nil {
		return nil
	}
	names := map[string]string{}
	for _, t := range f.Teams {
		for _, m := range t.FormerMembers {
			if word := strings.TrimSpace(m.Word); word != "" {
				names[appMemberAgent(m)] = word
			}
		}
	}
	for _, t := range f.Teams {
		for _, m := range t.Members {
			if word := strings.TrimSpace(m.Word); word != "" {
				names[appMemberAgent(m)] = word
			}
		}
	}
	return names
}

// nameUntitled gives each listed agent with no name of its own the word its
// crew calls it by, and keeps that name on the agent's record so every later
// agent.changed says it too. The crews are read only when a row needs a name.
func (b *appBackend) nameUntitled(agents []appwire.Agent) {
	var names map[string]string
	read := false
	for i := range agents {
		if strings.TrimSpace(agents[i].Title) != "" {
			continue
		}
		if !read {
			names, read = b.memberNames(), true
		}
		name := names[agents[i].ID]
		if name == "" {
			continue
		}
		agents[i].Title = name
		id := agents[i].ID
		b.locked(func() {
			if a := b.agents[id]; a != nil && strings.TrimSpace(a.view.Title) == "" {
				a.view.Title = name
			}
		})
	}
}

// reapBlank takes away a conversation this process made, once it has been let
// go, when nobody ever said anything in it, and forgets the agent; it answers
// whether it did. The judgement is the one the terminal reaps an empty launch
// by, made again under the crews' writer lock, so a crew cannot take the
// conversation in while it goes; a folder that is not there is left alone.
func (b *appBackend) reapBlank(a *appAgent) bool {
	a.gate.Lock()
	defer a.gate.Unlock()
	var id, transcript string
	mine := false
	b.locked(func() {
		id, transcript = a.view.ID, a.transcript
		mine = a.minted && a.live == nil && !a.opening
	})
	if !mine || strings.TrimSpace(transcript) == "" {
		return false
	}
	transcript = filepath.Clean(transcript)
	dir := session.DirOf(transcript)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return false
	}
	gone := false
	_ = teamstore.Inspect(b.crewProfile(), func(f *teamstore.File) error {
		if f.ReferencedConversations()[teamstore.ConversationIdentity(transcript)] || session.InUse(transcript) || !v3EmptyUnassignedSession(dir) {
			return nil
		}
		gone = os.RemoveAll(dir) == nil
		return nil
	})
	if !gone {
		return false
	}
	b.locked(func() {
		if b.agents[id] == a {
			delete(b.agents, id)
		}
	})
	b.forgetMarks(id)
	return true
}

// forgetMarks drops what the app's own file kept about an agent that is gone:
// its budget, a raise of it, and when it was closed or put away. The file is
// written only when it named the agent.
func (b *appBackend) forgetMarks(id string) {
	named := false
	_ = b.state.read(func(f *appStateFile) {
		_, budget := f.Budgets[id]
		_, raise := f.Raises["agent:"+id]
		_, closed := f.Closed[id]
		_, archived := f.Archived[id]
		named = budget || raise || closed || archived
	})
	if !named {
		return
	}
	_ = b.state.write(func(f *appStateFile) error {
		delete(f.Budgets, id)
		delete(f.Raises, "agent:"+id)
		delete(f.Closed, id)
		delete(f.Archived, id)
		return nil
	})
}
