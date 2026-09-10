package tui3

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// projectsPanel is `projects`: every folder this machine has conversations in,
// this window's own first and then the most recently spoken in — its path, how
// many chats are in it and how many are running, and where its repository
// stands. Enter on one starts a fresh conversation there ([app.homeProjectEnter]).
//
// IT IS NEVER EMPTY, so it has no whisper: the folder this window was launched
// in is always one of its rows.
type projectsPanel struct{ homePanelBase }

// homeProjectRows is how many projects the panel draws before its fold, and
// homeProjectPad the widest the path column is padded to, so the counts beside
// the paths stand in one column without a long path pushing them off the row.
const (
	homeProjectRows = 5
	homeProjectPad  = 24
)

// homeProjectRow is ONE PROJECT ON THE PROJECTS PANEL, and a cursor stop whose
// door is a fresh conversation in that folder. It is numbered beside the
// switcher's own kinds (place_home.go) for their reason: the iota block in
// home.go is edited by other lanes.
const homeProjectRow homeRowKind = 244

func (projectsPanel) rows(in *homeGridInput) homePanelRows {
	ordered := make([]session.Project, 0, len(in.world.Projects))
	for _, project := range in.world.Projects {
		if project.Dir == in.bucket {
			ordered = append([]session.Project{project}, ordered...)
			continue
		}
		ordered = append(ordered, project)
	}
	shown := min(homeProjectRows, len(ordered))
	pad := 0
	for _, project := range ordered[:shown] {
		pad = max(pad, min(homeProjectPad, len([]rune(projectWord(project, in.tilde)))))
	}
	var lines []homeLine
	for _, project := range ordered[:shown] {
		cell := &homeCell{panel: panelProjects, title: projectWord(project, in.tilde), pad: pad,
			note: projectCounts(project), right: projectRepo(project, in.repos)}
		lines = append(lines, homeLine{kind: homeProjectRow, project: project.Name, dir: project.Dir, proj: project, cell: cell})
	}
	return homePanelRows{lines: lines, more: len(ordered) - shown}
}

// projectWord is what a project is called on its row: its folder, with the
// person's home written `~/`. THE HOME DIRECTORY ITSELF IS NOT A NAME — a row
// for the conversations launched in `~` draws nothing in that cell rather than
// a lone tilde — and a project that never recorded a folder draws nothing there
// either, because the bucket it lives in is an address and not a place.
func projectWord(project session.Project, tilde string) string {
	path := strings.TrimSpace(project.Path)
	if path == "" || project.Name == "~" {
		return ""
	}
	if word := tildePath(path, tilde); word != "~" {
		return word
	}
	return ""
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
