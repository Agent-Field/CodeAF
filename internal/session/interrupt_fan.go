package session

import (
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// ONE ESC, ONE DECISION.
//
// Measured (F13/F17): one Escape mid-think, then a redirect, fired six model
// calls in the same second — two mastermind replans, two identical title
// calls, a 57-message handoff re-read, and a reflex. The handlers that answer
// an interrupt (the leftover race, the next turn's route, the mark reader, the
// brief writer, the namer) each decided independently what had changed.
//
// SO THEY SHARE ONE GENERATION. Interrupt mints it. The person's next words
// are what changed. Each generation may spend one planner pass and one title
// call; a second claim is silence. A redirect does not re-read the whole
// conversation to write a brief — the new words are the brief.

type interruptFan struct {
	mu      sync.Mutex
	live    bool
	changed string
	planned bool
	titled  bool
}

func (f *interruptFan) begin() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.live = true
	f.changed = ""
	f.planned = false
	f.titled = false
}

func (f *interruptFan) note(words string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.live || f.changed != "" {
		return
	}
	f.changed = strings.TrimSpace(words)
}

func (f *interruptFan) finishTurn() {
	f.mu.Lock()
	defer f.mu.Unlock()
	// THE FIRST PERSON-TYPED TURN AFTER ESC IS THE REDIRECT. Once it ends,
	// later turns plan and name as they always have. An interrupted turn's
	// own cleanup sees no words yet and leaves the generation standing.
	if f.changed == "" {
		return
	}
	f.live = false
	f.changed = ""
	f.planned = false
	f.titled = false
}

func (f *interruptFan) redirecting() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live && f.changed != ""
}

func (f *interruptFan) allow(role roles.Role) error {
	kind := interruptRoleKind(role)
	if kind == 0 {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.live {
		return nil
	}
	switch kind {
	case interruptPlanner:
		if f.planned {
			return errInterruptQuiet
		}
		f.planned = true
	case interruptTitle:
		if f.titled {
			return errInterruptQuiet
		}
		f.titled = true
	}
	return nil
}

const (
	interruptPlanner = 1
	interruptTitle   = 2
)

// interruptRoleKind says which of an Esc generation's two allowances a role
// spends, and 0 for a role that spends neither.
//
// THE SESSION'S OWN NAMER IS NOT ON THIS LIST, and that is the one exception
// here worth stating. Everything gated above is FOREGROUND WORK bought by the
// turn the person just stopped — a planner, a mark reader, the two words a task
// is called — and the allowance exists so that a leftover and a redirect racing
// inside one generation cannot each buy one.
//
// A session naming itself is none of that. Since #653 it is started when the
// person's first message is accepted, it runs on the SESSION'S lifetime rather
// than any turn's, and its dedup is [Agent.titleTried] — which is strictly
// stronger than a generation's allowance: one naming per session for the life
// of the process, against one per Esc. Leaving it here cost the thing the
// allowance was never about — a transient failure whose retry landed inside a
// live generation was answered with [errInterruptQuiet] and read as "the model
// said no", and the session stayed unnamed; and a task named first in the same
// generation took the slot before the namer's first attempt ever reached it.
//
// [roles.RoleTaskName] keeps the allowance. It is a name bought by the work a
// turn started, on that turn's clock, and it is exactly the case F13/F17 were
// written about.
func interruptRoleKind(role roles.Role) int {
	switch role {
	case roles.RoleMarkReader, roles.RoleHandoff, roles.RolePlanner, roles.RoleRouterConfirm:
		return interruptPlanner
	case roles.RoleTaskName:
		return interruptTitle
	}
	return 0
}

// errInterruptQuiet is what a second planner or title in the same Esc
// generation returns. Callers already treat any error as silence.
var errInterruptQuiet = errStr("session: this stop already spent that call")
