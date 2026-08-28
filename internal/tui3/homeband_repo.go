package tui3

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const homeRepoTTL = 5 * time.Second

type homeRepoReading struct {
	at   time.Time
	line string
	// branch is the head this workspace is on, kept beside the whole clause list
	// rather than parsed back out of it. The composer layer states where a task
	// will run as `in ~/aforge-v2, on master` (SCREEN 2e) and wants that one word
	// without the dirty count beside it — and reading it back off `line` would be
	// a second parser for a string this file just built.
	branch string
}

var homeGitStatus = func(ctx context.Context, workspace string) ([]byte, error) {
	return exec.CommandContext(ctx, "git", "-C", workspace, "status", "--porcelain=v2", "--branch").Output()
}

func init() {
	registerHomeBand(homeBand{name: "repo", order: bandOrderRepo, draw: drawRepoBand})
}

func drawRepoBand(a *app, ctx bandContext) []string {
	workspace := strings.TrimSpace(ctx.subject.row.Workspace)
	if workspace == "" {
		return nil
	}
	cached, ok := a.home.repos[workspace]
	if !ok || cached.line == "" {
		return nil
	}
	return bandClauses(ctx.width, 0, ctx.pal.dim, strings.Split(cached.line, " · ")...)
}

// refreshHomeRepo takes the one bounded reading when a card arrives — under the
// cursor, or under the pointer, because the card follows whichever of them is
// pointing at a row (home.go's [homeView.previewLine]). Drawing then remains
// pure no matter how often the terminal repaints, and a reading is taken once
// per workspace per [homeRepoTTL] however the arrival happened.
func (a *app) refreshHomeRepo(now time.Time) {
	subject, ok := a.homeSubject()
	if !ok || subject.kind != bandKindSession {
		return
	}
	a.refreshRepoOf(strings.TrimSpace(subject.row.Workspace), now)
}

// refreshRepoOf is that reading for ONE NAMED WORKSPACE, which is what the
// composer layer needs: it opens on a keystroke and moves its destination on a
// keystroke, and the workspace it lands on may be one no card has ever drawn.
//
// IT IS STILL NEVER CALLED FROM A DRAW. Both callers are keystrokes — a card
// arriving under the cursor, and the layer opening or cycling — which is the
// whole of what ARCHITECTURE.md's fourth law asks: `open` and `tick` may read
// the disk and `body` may not.
func (a *app) refreshRepoOf(workspace string, now time.Time) {
	if workspace == "" {
		return
	}
	if cached, ok := a.home.repos[workspace]; ok && now.Sub(cached.at) < homeRepoTTL {
		return
	}
	if a.home.repos == nil {
		a.home.repos = map[string]homeRepoReading{}
	}
	commandCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	raw, err := homeGitStatus(commandCtx, workspace)
	line, branch := "", ""
	if err == nil {
		line, branch = parseHomeRepo(string(raw))
	}
	a.home.repos[workspace] = homeRepoReading{at: now, line: line, branch: branch}
}

// repoBranchOf is the head one workspace is on, as the last reading found it,
// and "" for a workspace nobody has read or one that is not a repository at all.
// It reads the cache and never the disk, so a body may call it.
func (a *app) repoBranchOf(workspace string) string {
	return a.home.repos[strings.TrimSpace(workspace)].branch
}

// parseHomeRepo answers the whole clause list AND the branch on its own, for
// [homeRepoReading.branch]'s stated reason.
func parseHomeRepo(raw string) (string, string) {
	var branch string
	dirty, ahead, behind := 0, 0, 0
	for _, line := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			branch = strings.TrimSpace(strings.TrimPrefix(line, "# branch.head "))
			if branch == "(detached)" {
				branch = ""
			}
		case strings.HasPrefix(line, "# branch.ab "):
			for _, part := range strings.Fields(line) {
				if strings.HasPrefix(part, "+") {
					ahead, _ = strconv.Atoi(strings.TrimPrefix(part, "+"))
				} else if strings.HasPrefix(part, "-") {
					behind, _ = strconv.Atoi(strings.TrimPrefix(part, "-"))
				}
			}
		case line != "" && line[0] != '#':
			dirty++
		}
	}
	var parts []string
	if branch != "" {
		parts = append(parts, branch)
	}
	if dirty > 0 {
		// ONE FILE IS NOT ONE FILES. The count is a real number on a line a
		// person reads, and the commonest reading of all is a repository with a
		// single file changed — the moment somebody is most likely to be looking
		// at this band at all.
		parts = append(parts, strconv.Itoa(dirty)+plural(" file", dirty)+" dirty")
	}
	if ahead > 0 {
		parts = append(parts, "ahead "+strconv.Itoa(ahead))
	}
	if behind > 0 {
		parts = append(parts, "behind "+strconv.Itoa(behind))
	}
	return strings.Join(parts, " · "), branch
}
