package session

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// A CONVERSATION THAT BELONGS TO A WORKSTREAM KEEPS ITS WORK THERE.
//
// A workstream (an Afi, or one of its chats) has its own Files: what is made in
// it lands there, where everyone in the workstream sees it. The app's Library
// is a different shelf, reached only when a person adds something to it on
// purpose. So a marked conversation is:
//
//   - never captured into the Library (knowledge.go's scan passes its sources
//     over, and the job capture does the same);
//   - without the app-wide `automation` verb ([Config.mayAutomate]): standing
//     work for a workstream is set up in the workstream itself;
//   - told both on its page ([Config.workstreamFact]).
//
// The mark is the session's own `meta.json` word, set by a client with
// `agents.set_workstream` (a desk for a Personal workstream, an organization
// server for its own); an organization's workstream agent, which carries a
// [Config.Workstream] door, is marked by that door alone.

// workstreamMarkMost bounds a workstream id as the wire states it.
const workstreamMarkMost = 128

// WorkstreamAt reads the stored workstream mark; "" is an ordinary conversation.
func WorkstreamAt(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	meta, err := LoadMeta(dir)
	if err != nil {
		return ""
	}
	return meta.Workstream
}

// SetConversationWorkstream marks an existing conversation as a workstream's,
// or clears the mark with "". A word already kept owes no write.
func SetConversationWorkstream(dir, workstream string) (changed bool, err error) {
	workstream = strings.TrimSpace(workstream)
	if len(workstream) > workstreamMarkMost || strings.IndexFunc(workstream, unicode.IsControl) >= 0 {
		return false, fmt.Errorf("workstream is an id of at most %d characters", workstreamMarkMost)
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false, fmt.Errorf("set conversation workstream: no conversation directory")
	}
	err = withMetaLock(dir, func() error {
		meta, err := LoadMeta(dir)
		if err != nil {
			return err
		}
		if meta.ID == "" {
			return fmt.Errorf("no conversation at %s", dir)
		}
		if meta.Workstream == workstream {
			return nil
		}
		meta.Workstream = workstream
		if err := saveMetaLocked(dir, meta); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, err
}

// belongsToWorkstream is the ONE reading of the mark: an organization's
// workstream door, or the stored word.
func (c Config) belongsToWorkstream() bool {
	if c.Workstream != nil {
		return true
	}
	return WorkstreamAt(c.Place.Dir) != ""
}

// workstreamFactLine is what a marked conversation's page adds.
const workstreamFactLine = "- THIS CONVERSATION BELONGS TO A WORKSTREAM. What you make for it is saved in that workstream's Files, where everyone in it sees it; the app's Library is a separate shelf that something reaches only when a person adds it there. Never say you filed or saved anything in the Library. You cannot set up work on a clock here: when the person wants something done every Monday, each morning or the like, tell them to set it up under People and rules › Keeps running — Add a standing order…"

func (c Config) workstreamFact(page string) string {
	if !c.inWorkstream {
		return page
	}
	return page + "\n" + workstreamFactLine
}

// withdrawTool takes one tool off the belt and the shelf, in fresh arrays, so a
// request walking the old snapshot is undisturbed ([Agent.retellTasksTool]'s
// way). A tool not held is nothing to do.
func (a *Agent) withdrawTool(name string) {
	a.armMu.Lock()
	defer a.armMu.Unlock()
	if i := slices.IndexFunc(a.tools, func(t bare.Tool) bool { return t.Name == name }); i >= 0 {
		tools := slices.Delete(slices.Clone(a.tools), i, i+1)
		if len(a.definitions) == len(a.tools) {
			a.definitions = slices.Delete(slices.Clone(a.definitions), i, i+1)
		}
		a.tools = tools
	}
	for group, held := range a.shelf {
		if i := slices.IndexFunc(held, func(t bare.Tool) bool { return t.Name == name }); i >= 0 {
			a.shelf[group] = slices.Delete(slices.Clone(held), i, i+1)
		}
	}
}
