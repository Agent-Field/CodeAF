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
	workspace := strings.TrimSpace(subject.row.Workspace)
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
	line := ""
	if err == nil {
		line = parseHomeRepo(string(raw))
	}
	a.home.repos[workspace] = homeRepoReading{at: now, line: line}
}

func parseHomeRepo(raw string) string {
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
		parts = append(parts, strconv.Itoa(dirty)+" files dirty")
	}
	if ahead > 0 {
		parts = append(parts, "ahead "+strconv.Itoa(ahead))
	}
	if behind > 0 {
		parts = append(parts, "behind "+strconv.Itoa(behind))
	}
	return strings.Join(parts, " · ")
}
