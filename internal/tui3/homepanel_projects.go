package tui3

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
)

// projectsPanel is `projects`: every folder this machine has conversations in,
// this window's own first and then the most recently spoken in — its path, how
// many chats are in it and how many are running, and where its repository
// stands. Clicking one selects the next message’s project ([app.pinTargetProject]).
//
// IT IS NEVER EMPTY, so it has no whisper: the folder this window was launched
// in is always one of its rows.
type projectsPanel struct{ homePanelBase }

// homeProjectPad is the widest the path column is padded to, so the counts
// beside the paths stand in one column without a long path pushing them off the
// row.
const homeProjectPad = 24

// homeProjectRow selects the draft's project when clicked. It is not a keyboard
// stop, so arrow navigation stays in the field and Option+P cycles projects.
// Its number stays outside the shared iota block to avoid shifting other rows.
const homeProjectRow homeRowKind = 244

func (projectsPanel) rows(in *homeGridInput) homePanelRows {
	ordered := projectsOrdered(in)
	shown := min(in.cap(panelProjects), len(ordered))
	pad := 0
	for _, project := range ordered[:shown] {
		pad = max(pad, min(homeProjectPad, len([]rune(projectWord(project, in.tilde)))))
	}
	var lines []homeLine
	for _, project := range ordered[:shown] {
		cell := &homeCell{panel: panelProjects, title: projectWord(project, in.tilde), pad: pad, path: true,
			note: projectCounts(project), right: projectRepo(project, in.repos)}
		lines = append(lines, homeLine{kind: homeProjectRow, project: project.Name, dir: project.Dir, proj: project, cell: cell})
	}
	return homePanelRows{lines: lines, more: len(ordered) - shown}
}

// projectsOrdered is the panel's projects: THIS WINDOW'S FOLDER FIRST, and then
// every other one with a folder, in the world's order.
//
// THE LAUNCH FOLDER IS ALWAYS A ROW (DESIGN §4), with no chats in it or with
// no bucket at all — a window opened a moment ago in a folder nobody has spoken
// in is still standing somewhere, and `projects` over nothing was the owner's
// first binary. A project that recorded no folder is left off: its row would be
// a count under no name, and enter on it has nowhere to start a conversation.
func projectsOrdered(in *homeGridInput) []session.Project {
	launch := strings.TrimSpace(in.launch)
	var own *session.Project
	out := make([]session.Project, 0, len(in.world.Projects)+1)
	for _, project := range in.world.Projects {
		path := strings.TrimSpace(project.Path)
		switch {
		case path == "":
		case own == nil && (project.Dir == in.bucket || path == launch):
			mine := project
			own = &mine
		default:
			out = append(out, project)
		}
	}
	if own == nil && launch != "" {
		own = &session.Project{Path: launch, Name: standBareName(launch)}
	}
	if own == nil {
		return out
	}
	return append([]session.Project{*own}, out...)
}

// projectWord is what a project is called on its row: its folder, with the
// person's home written `~/` — and the home directory itself `~`, because in
// this cell it is a PATH and not a name (the rule that retired `~` as a project
// name is about a tag standing alone on a chat row, homepanel_recent.go). A
// project that never recorded a folder draws nothing here, because the bucket
// it lives in is an address and not a place.
func projectWord(project session.Project, tilde string) string {
	return tildePath(strings.TrimSpace(project.Path), tilde)
}

// projectCounts is `12 chats · 1 running`, each half only when it is not zero.
func projectCounts(project session.Project) string {
	chats := 0
	for _, row := range project.Sessions {
		if !row.Archived {
			chats++
		}
	}
	var parts []string
	if chats > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", chats, switcherPlural(chats, "chat", "chats")))
	}
	if running := project.Running(); running > 0 {
		parts = append(parts, fmt.Sprintf("%d running", running))
	}
	return strings.Join(parts, rowSep)
}

// projectRepo is where the project's repository stands — `master, 2 dirty` — out
// of the last `git status` the beat took for it (homeband_repo.go), with its
// own clauses joined by commas because it is one fact about one checkout.
func projectRepo(project session.Project, repos map[string]homeRepoReading) string {
	path := strings.TrimSpace(project.Path)
	if path == "" {
		return ""
	}
	return strings.Join(strings.Split(repos[path].line, rowSep), ", ")
}
