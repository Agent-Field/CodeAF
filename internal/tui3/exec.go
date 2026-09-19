package tui3

import (
	"context"
	"path/filepath"
	"strings"
)

// Exec is the home/chat seam onto Wave 4 delegated execution. It is a TUI
// interface so this package never imports internal/workspace, wsapi, or
// wsexec. The DTOs are exported so cmd/codeaf can implement the seam without
// this package importing those, and without those importing tui3. Wiring owns
// a separate adapter from Collab: launch verbs must not grow on [Collab], or
// `var _ tui3.Collab` would demand them.
//
// NIL IS NO CHROME, NOT A BROKEN LAUNCH. A door that could not bind the
// executor leaves this nil; launch-state chrome and the pause/stop verbs are
// absent, and natural-language launch still works if session.Config.Exec is
// wired. A belt that painted a dummy completed run would be a capability
// advertised as broken.
//
// THE SNAPSHOT IS TAKEN ON THE HOME BEAT and after an exec mutation.
// View, the cursor and a mere rebuild read [app.execView], which is a memo.
// LaunchState is software-derived from bindings; it does not call a model.
type Exec interface {
	LaunchState(ctx context.Context, conversationID string) ([]ExecWork, error)
	PauseCoordination(ctx context.Context, coordinatorID string) error
	StopWork(ctx context.Context, workID string) error
}

// ExecWork is one owned run/task as the discussion and folder preview draw it.
// State is software-derived. Joined is true when launch-or-join followed
// existing work rather than starting a second run.
type ExecWork struct {
	WorkID, Title, State, Road, SourceRef string
	Joined                                bool
}

// execReading is the memo [app.readExec] writes. View reads this and never
// the seam. A failed refresh keeps the last good snapshot (P12).
type execReading struct {
	works []ExecWork
	down  bool
}

// Person-facing exec copy, quoted in the contract and the tests as these
// exact phrases. Binding machinery words are mapped, not painted.
const (
	execPauseWord       = "pause coordination"
	execStopWord        = "stop work"
	execJoinWord        = "launch-or-join"
	execSourceWord      = "source"
	execUnavailableWord = "launch state is not available"
	execCouldNotPause   = "could not pause coordination"
	execCouldNotStop    = "could not stop work"
	execNeedChatWord    = "stand on a chat · then pause coordination"
	execZeroRunsWord    = "0 runs"
	execFakeDoneWord    = "100%"
)

// readExec is the beat's launch-state reading. Nil Exec is absence: the memo
// is empty and there is no chrome. A LaunchState failure keeps the last good
// works so a binding update cannot wipe the selection, and with no prior
// snapshot it is a labelled absence rather than an empty roll-up.
func (a *app) readExec() {
	if a.exec == nil {
		a.execView = execReading{}
		a.home.folders.works = nil
		a.home.folders.execDown = false
		return
	}
	works, ok := a.collectLaunchState()
	if !ok {
		a.execView.down = true
		a.home.folders.execDown = len(a.execView.works) == 0
		a.home.folders.works = a.execView.works
		return
	}
	a.execView = execReading{works: works}
	a.home.folders.works = works
	a.home.folders.execDown = false
}

func (a *app) collectLaunchState() ([]ExecWork, bool) {
	ids := a.execWatchIDs()
	if len(ids) == 0 {
		return nil, true
	}
	var works []ExecWork
	seen := map[string]bool{}
	ok := false
	for _, id := range ids {
		got, err := a.exec.LaunchState(a.folderCtx(), id)
		if err != nil {
			continue
		}
		ok = true
		works = appendLaunchWorks(works, seen, got)
	}
	return works, ok
}

func appendLaunchWorks(works []ExecWork, seen map[string]bool, got []ExecWork) []ExecWork {
	for _, work := range got {
		key := execWorkKey(work)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		works = append(works, work)
	}
	return works
}

func execWorkKey(work ExecWork) string {
	if id := strings.TrimSpace(work.WorkID); id != "" {
		return id
	}
	source := strings.TrimSpace(work.SourceRef)
	title := strings.TrimSpace(work.Title)
	if source == "" && title == "" {
		return ""
	}
	return source + "\x00" + title
}

func (a *app) execWatchIDs() []string {
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	add(a.conversationRef())
	for _, place := range a.home.folders.members {
		add(place.RefID)
	}
	for _, place := range a.home.folders.root.Unfiled {
		add(place.RefID)
	}
	return ids
}

func execWorkBelongs(work ExecWork, conversationID string) bool {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return false
	}
	if strings.TrimSpace(work.SourceRef) == conversationID {
		return true
	}
	return strings.TrimSpace(work.WorkID) == conversationID
}

func execStateWord(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "reserved", "pending":
		return "pending"
	case "admitted", "bound", "running":
		return "running"
	case "finishing":
		return "finishing"
	case "paused":
		return "paused"
	case "stopped":
		return "stopped"
	case "completed", "done":
		return "done"
	case "failed", "incomplete":
		return "incomplete"
	case "your call":
		return "your call"
	default:
		return ""
	}
}

func conversationRefFromFile(file string) string {
	id := strings.TrimSpace(filepath.Base(homeSessionDirOf(file)))
	if id == "" || id == "." || id == string(filepath.Separator) {
		return ""
	}
	return id
}
