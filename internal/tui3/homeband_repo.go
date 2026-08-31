package tui3

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
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

// homeRepoMsg is one workspace's reading, coming BACK. It carries the workspace
// it is about because several may be in flight — a pointer sweeping down a list
// of conversations in three projects asks about three repositories — and a
// reading that landed against whichever row happened to be under the cursor when
// it arrived would be a branch name from another project.
type homeRepoMsg struct {
	workspace string
	line      string
	branch    string
}

// tookHomeRepo files that answer.
func (a *app) tookHomeRepo(msg homeRepoMsg) {
	delete(a.repoAsking, msg.workspace)
	if a.home.repos == nil {
		a.home.repos = map[string]homeRepoReading{}
	}
	a.home.repos[msg.workspace] = homeRepoReading{at: a.now(), line: msg.line, branch: msg.branch}
	a.touch()
}

// refreshRepoOf asks for the reading of ONE NAMED WORKSPACE, and it is a
// tea.Cmd rather than a syscall because A KEYSTROKE MAY NOT WAIT FOR git.
//
// This used to run `git status --porcelain=v2 --branch` INSIDE the update loop,
// on the arrow keys and on every hover that moved the card. It was already off
// the draw and already behind a five-second cache, which is why it read as
// correct; what neither of those bounds is the WALL. The command was given a
// whole second of ceiling, and a repository big enough to need it is a
// repository a person browsing home meets on their first `↓` — measured at 7.7ms
// on aforge's own worktree, and seconds on a cold cache or a network mount, with
// every key and every motion queued behind it. It is exactly the fault
// reasoninglevel.go took off the pointer's way over a connection, made locally.
//
// So the reading is ASKED FOR here and ANSWERED in [app.tookHomeRepo], and the
// card draws the last one it was given (homeband_repo.go's [drawRepoBand] reads
// the cache and never the disk). A workspace nobody has read yet simply has no
// branch clause on its place line for one frame, which is the emptiness law and
// not a blank.
//
// ONE ASK PER WORKSPACE IS IN FLIGHT AT A TIME. Without that, a pointer swept
// down twenty rows of one project would fork twenty gits at the same repository,
// all of them answering the same thing.
func (a *app) refreshRepoOf(workspace string, now time.Time) tea.Cmd {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	if cached, ok := a.home.repos[workspace]; ok && now.Sub(cached.at) < homeRepoTTL {
		return nil
	}
	if a.repoAsking == nil {
		a.repoAsking = map[string]bool{}
	}
	if a.repoAsking[workspace] {
		return nil
	}
	a.repoAsking[workspace] = true
	return func() tea.Msg {
		commandCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		raw, err := homeGitStatus(commandCtx, workspace)
		line, branch := "", ""
		if err == nil {
			line, branch = parseHomeRepo(string(raw))
		}
		return homeRepoMsg{workspace: workspace, line: line, branch: branch}
	}
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
