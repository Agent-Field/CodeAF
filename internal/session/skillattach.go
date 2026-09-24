// The skills a person hands this conversation by hand.
//
// The shelf is chosen for the model two ways, and they answer different
// questions. RETRIEVAL asks "which of these look like the work in front of
// us?", which is a guess and is allowed to be wrong; ATTACHMENT is a person
// saying "use this one", which is not a guess and may never be scored away.
// So an attachment is held here by name, rendered ahead of anything retrieval
// found, and never subject to the catalog's window (skillcatalog.go).
//
// NAMES AND NOT FACTS. Resolution happens where the skills are rendered,
// against the shelf as it stands at that moment, which is what lets a person
// attach a skill they are about to install and lets a skill deleted from the
// shelf simply stop being carried.
package session

import (
	"errors"
	"strings"

	store "github.com/Agent-Field/codeaf/internal/store"
)

// AttachSkills puts skill names in front of this conversation, in the order
// given, and returns the set as it now stands. A name already attached keeps
// its original position rather than moving to the end: the order is the
// conflict rule the workers read (plan.ComposeSkills — earlier wins), so
// re-attaching what is already there must not quietly re-rank it.
//
// Blank names are dropped. Nothing here reads the shelf, so an unknown name is
// kept as written.
func (a *Agent) AttachSkills(names ...string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || containsSkillName(a.attachedSkills, name) {
			continue
		}
		a.attachedSkills = append(a.attachedSkills, name)
	}
	return append([]string(nil), a.attachedSkills...)
}

// DetachSkill takes one name back off, and says whether it was there. Taking
// off a name nobody attached is not an error: a surface offering a list of
// checkboxes has no way to know the list changed under it.
func (a *Agent) DetachSkill(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for index, attached := range a.attachedSkills {
		if strings.EqualFold(attached, name) {
			a.attachedSkills = append(a.attachedSkills[:index], a.attachedSkills[index+1:]...)
			return true
		}
	}
	return false
}

// AttachedSkills is the set as it stands, in attachment order. The slice is a
// copy, because the caller is a surface drawing a list while a turn may be
// running.
func (a *Agent) AttachedSkills() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.attachedSkills) == 0 {
		return nil
	}
	return append([]string(nil), a.attachedSkills...)
}

// ClearAttachedSkills takes every name back off and returns how many were on.
func (a *Agent) ClearAttachedSkills() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	count := len(a.attachedSkills)
	a.attachedSkills = nil
	return count
}

// containsSkillName is the one comparison every door above uses. A shelf name
// is a folder name, and a person typing one into a picker's search box should
// not be told it is a different skill because they typed it in capitals.
func containsSkillName(names []string, name string) bool {
	for _, held := range names {
		if strings.EqualFold(held, name) {
			return true
		}
	}
	return false
}

// ErrNoSkillShelf is the answer [Agent.SkillFacts] gives a conversation that
// has no shelf store at all, which is different from a shelf with nothing on
// it: a surface lists the skill folders it finds either way, and only this
// answer makes it say on each row that choosing one does nothing.
var ErrNoSkillShelf = errors.New("this conversation has no skill shelf")

// SkillFacts is the shelf as THIS SESSION reads it — the same store the
// catalog, the skills a message carries and `use_skill` read
// ([Config.skillShelf]) — for a surface that lists it. It is the session's
// answer and not the surface's because which store the shelf lives in is the
// door's choice, and a picker that read some store of its own would be a
// second answer to "which skills can this conversation use".
func (a *Agent) SkillFacts(status string, limit int) ([]store.Fact, error) {
	shelf := a.config.skillShelf()
	if shelf == nil {
		return nil, ErrNoSkillShelf
	}
	return shelf.SkillFacts(status, limit)
}
