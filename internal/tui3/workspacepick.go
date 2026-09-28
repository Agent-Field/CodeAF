package tui3

import tea "charm.land/bubbletea/v2"

// setWorkspace is shared by a typed path and the workspace picker.
func (a *app) setWorkspace(rest string) tea.Cmd {
	if a.anchorWorkspace == nil {
		a.note("this conversation already has a workspace")
		return nil
	}
	resolved, err := a.anchorWorkspace(rest)
	if err != nil {
		a.note("could not set the workspace: " + err.Error())
		return nil
	}
	a.workspace = resolved
	a.owned = false
	a.place = placeShown(resolved, false, a.host)
	// THE BRANCH IS ASKED FOR, NOT WAITED ON. This used to call the probe
	// straight — two `git` processes under one four-hundred-millisecond
	// ceiling, run on the update loop, so a person who typed `/workspace`
	// into a large repository watched the whole surface stop for up to four
	// tenths of a second before their own keystroke was drawn. It takes the
	// road every other reading of the repository takes ([app.probeGit],
	// armed at `open` and at every turn end): the command runs off the loop
	// and the branch arrives as a gitMsg, which is exactly the same nothing
	// the legend draws until a probe answers.
	a.branch, a.branchDirty = "", false
	a.anchorWorkspace = nil
	a.note("workspace · " + a.hostedPath(resolved))
	a.touch()
	return a.probeGit()
}

func (a *app) openWorkspacePick() tea.Cmd {
	if a.anchorWorkspace == nil {
		a.note("this conversation already has a workspace")
		return nil
	}
	if a.hosted() {
		a.note(folderRemoteWord)
		return nil
	}
	cmd := a.openContextPick("", false)
	if a.folder.open {
		a.folder.forWorkspace = true
		a.folder.held = nil
	}
	return cmd
}
