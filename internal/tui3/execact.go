package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// folderExecVerbs is the optional pause/stop/revoke strip. NIL IS ABSENT: a
// surface without Exec never offers these keys, so Wave 1 membership verbs stay
// exactly `n f e i` / `n f m w x` and Wave 3 mark/coordinate stay `k` / `c`.
// `p` is pause coordination; `s` is stop work; `v` is revoke grant. They must
// not share a chord. Closing a view does not pause, stop, or revoke; tab-close
// `stop work` is the stop action.
func (a *app) folderExecVerbs(line homeLine) []verb {
	if a.exec == nil {
		return nil
	}
	verbs := []verb{{
		key:  'p',
		word: execPauseWord,
		do:   func() tea.Cmd { return a.pauseCoordination() },
	}}
	if line.kind == homeSession {
		if workID := a.execWorkIDOf(line); workID != "" {
			verbs = append(verbs, verb{
				key:  's',
				word: execStopWord,
				do:   func() tea.Cmd { return a.stopExecWork(workID) },
			})
		}
		if grantID := a.execGrantIDOf(line); grantID != "" {
			verbs = append(verbs, verb{
				key:  'v',
				word: execRevokeWord,
				do:   func() tea.Cmd { return a.revokeExecGrant(grantID) },
			})
		}
	}
	return verbs
}

func (a *app) execWorkIDOf(line homeLine) string {
	ref := a.placementRefOf(line)
	for _, work := range a.execView.works {
		if !execWorkBelongs(work, ref) {
			continue
		}
		if id := strings.TrimSpace(work.WorkID); id != "" {
			return id
		}
	}
	return ""
}

func (a *app) execGrantIDOf(line homeLine) string {
	ref := a.placementRefOf(line)
	for _, work := range a.execView.works {
		if !execWorkBelongs(work, ref) {
			continue
		}
		if id := strings.TrimSpace(work.GrantID); id != "" {
			return id
		}
	}
	return ""
}

// pauseCoordination is `p`: stop new deliver/invite/launch from this
// existing chat. It does not stop work already running. An empty conversation
// id is a refusal, never a dummy success.
func (a *app) pauseCoordination() tea.Cmd {
	if a.exec == nil {
		return nil
	}
	id := strings.TrimSpace(a.conversationRef())
	if id == "" {
		a.folderNote(execNeedChatWord)
		return nil
	}
	if err := a.exec.PauseCoordination(a.folderCtx(), id); err != nil {
		a.folderNote(execCouldNotPause)
		return nil
	}
	a.refreshFolderMemo()
	return nil
}

// stopExecWork is `s` on a member that already has authorized work. History
// remains. This is not pause coordination.
func (a *app) stopExecWork(workID string) tea.Cmd {
	if a.exec == nil {
		return nil
	}
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return nil
	}
	if err := a.exec.StopWork(a.folderCtx(), workID); err != nil {
		a.folderNote(execCouldNotStop)
		return nil
	}
	a.refreshFolderMemo()
	return nil
}

// revokeExecGrant is `v`: withdraw authority so the next launch/steer/stop
// is refused. Already-bound work stays until an explicit stop work. This is
// not pause coordination and not stop work.
func (a *app) revokeExecGrant(grantID string) tea.Cmd {
	if a.exec == nil {
		return nil
	}
	grantID = strings.TrimSpace(grantID)
	if grantID == "" {
		return nil
	}
	if err := a.exec.RevokeGrant(a.folderCtx(), grantID); err != nil {
		a.folderNote(execCouldNotRevoke)
		return nil
	}
	a.refreshFolderMemo()
	return nil
}

// stopAuthorizedWork is the tab-close `stop work` spelling of this action,
// not pause. Keep running and closing a view never land here.
func (a *app) stopAuthorizedWork(tab chatTab) {
	if a.exec == nil {
		return
	}
	id := a.conversationRef()
	if tab.key != a.frontTabKey() {
		id = conversationRefFromFile(tab.file)
	}
	for _, work := range a.execView.works {
		if !execWorkBelongs(work, id) {
			continue
		}
		workID := strings.TrimSpace(work.WorkID)
		if workID == "" {
			continue
		}
		if err := a.exec.StopWork(a.folderCtx(), workID); err != nil {
			a.note(execCouldNotStop)
		}
	}
}
