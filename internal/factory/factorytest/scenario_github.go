package factorytest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// FakeGitHub is one repository with one pull request, as the forge's own API
// answers it. A scenario changes the pull request's facts (its head, its base,
// whether it merges, its checks, its reviews, its comments) and the factory's
// real poller and the runner's real publish and merge read and write them
// through [factorygithub.Source]. Nothing here knows what a job is.
type FakeGitHub struct {
	factorygithub.API

	mu       sync.Mutex
	owner    string
	repo     string
	pull     *forge.Pull
	runs     map[string][]forge.CheckRun
	reviews  []forge.Review
	required int
	comments []string
	// diff stands for the pull request's diff: one file per pushed head.
	files int
	// writes is every write the factory made, in order, for a scenario to read.
	writes []string
	// headOf reads a pushed branch's head; the scenario's remote answers it.
	headOf func(branch string) string
	// down fails every read, as GitHub being unreachable does.
	down bool
	// threads are the pull request's review threads and posted every reply
	// and comment the factory made, word for word (scenario_acts.go).
	threads []reviewThread
	posted  []string
	// holdReplies makes a reply wait once GitHub has it, until its caller
	// lets go: a service that closes mid-send. replyHeld is closed when one
	// is waiting.
	holdReplies bool
	replyHeld   chan struct{}
}

// NewFakeGitHub is an empty repository: no pull request yet.
func NewFakeGitHub(owner, repo string) *FakeGitHub {
	return &FakeGitHub{owner: owner, repo: repo, runs: map[string][]forge.CheckRun{}}
}

// clone is the repository as it stands, for another scenario to go on from.
func (g *FakeGitHub) clone() *FakeGitHub {
	g.mu.Lock()
	defer g.mu.Unlock()
	c := &FakeGitHub{owner: g.owner, repo: g.repo, runs: map[string][]forge.CheckRun{}, required: g.required, files: g.files, down: g.down}
	if g.pull != nil {
		pull := *g.pull
		if pull.Mergeable != nil {
			ok := *pull.Mergeable
			pull.Mergeable = &ok
		}
		c.pull = &pull
	}
	for sha, runs := range g.runs {
		c.runs[sha] = append([]forge.CheckRun(nil), runs...)
	}
	c.reviews = append([]forge.Review(nil), g.reviews...)
	c.comments = append([]string(nil), g.comments...)
	c.writes = append([]string(nil), g.writes...)
	c.threads = append([]reviewThread(nil), g.threads...)
	c.posted = append([]string(nil), g.posted...)
	return c
}

func (g *FakeGitHub) change(f func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f()
}

// Open says whether the factory has opened the pull request.
func (g *FakeGitHub) Open() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.pull != nil && g.pull.State == "open"
}

// PullRequest is the pull request as it stands, and false before one is opened.
func (g *FakeGitHub) PullRequest() (forge.Pull, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pull == nil {
		return forge.Pull{}, false
	}
	return *g.pull, true
}

// Writes is every write the factory made on the forge, oldest first.
func (g *FakeGitHub) Writes() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.writes...)
}

// SetChecks sets the head's check runs: passed, failed and pending are how
// many of each the head carries.
func (g *FakeGitHub) SetChecks(passed, failed, pending int) {
	g.change(func() {
		if g.pull == nil {
			return
		}
		var runs []forge.CheckRun
		for i := 0; i < passed; i++ {
			runs = append(runs, forge.CheckRun{Name: fmt.Sprintf("test-%d", i), Status: "completed", Conclusion: "success"})
		}
		for i := 0; i < failed; i++ {
			runs = append(runs, forge.CheckRun{Name: fmt.Sprintf("lint-%d", i), Status: "completed", Conclusion: "failure", URL: fmt.Sprintf("https://github.com/%s/%s/actions/runs/%d", g.owner, g.repo, 100+i)})
		}
		for i := 0; i < pending; i++ {
			runs = append(runs, forge.CheckRun{Name: fmt.Sprintf("build-%d", i), Status: "in_progress"})
		}
		g.runs[g.pull.HeadSHA] = runs
	})
}

// Conflict says whether the pull request conflicts with its base.
func (g *FakeGitHub) Conflict(on bool) {
	g.change(func() {
		if g.pull != nil {
			ok := !on
			g.pull.Mergeable = &ok
		}
	})
}

// MoveBase moves the base branch's head, as a merge into it does.
func (g *FakeGitHub) MoveBase(sha string) {
	g.change(func() {
		if g.pull != nil {
			g.pull.BaseSHA = sha
		}
	})
}

// PushOutside is someone else's commit on the pull request's branch: the
// head moves and its checks start again.
func (g *FakeGitHub) PushOutside(sha string) {
	g.change(func() {
		if g.pull != nil {
			g.pull.HeadSHA = sha
			g.files++
			g.pull.Files = g.files
		}
	})
}

// Review submits a review: state is APPROVED, CHANGES_REQUESTED or COMMENTED.
func (g *FakeGitHub) Review(by, state, body string, at time.Time) {
	g.change(func() {
		g.reviews = append(g.reviews, forge.Review{User: by, State: state, Body: body, At: at, URL: fmt.Sprintf("https://github.com/%s/%s/pull/7#pullrequestreview-%d", g.owner, g.repo, len(g.reviews)+1)})
	})
}

// AddComment adds a review comment to the pull request's feedback.
func (g *FakeGitHub) AddComment(words string) {
	g.change(func() { g.comments = append(g.comments, words) })
}

// Require sets how many approving reviews the base branch asks for.
func (g *FakeGitHub) Require(n int) { g.change(func() { g.required = n }) }

// Down makes every read fail, or work again.
func (g *FakeGitHub) Down(on bool) { g.change(func() { g.down = on }) }

// Ready marks the pull request ready for review, as a person does on GitHub.
func (g *FakeGitHub) Ready() {
	g.change(func() {
		if g.pull != nil {
			g.pull.Draft = false
		}
	})
}

// ClosePull closes the pull request without merging it.
func (g *FakeGitHub) ClosePull() {
	g.change(func() {
		if g.pull != nil {
			g.pull.State = "closed"
		}
	})
}

// MergeOutside merges the pull request, as a person does on GitHub.
func (g *FakeGitHub) MergeOutside(by string, at time.Time) {
	g.change(func() {
		if g.pull != nil {
			g.pull.State, g.pull.Merged, g.pull.MergedBy, g.pull.MergedAt = "closed", true, by, at
		}
	})
}

// pushed is the factory's push arriving: the head moves, its checks have not
// reported, and a conflict the push resolved is gone.
func (g *FakeGitHub) pushed(branch, sha string) {
	g.change(func() {
		g.writes = append(g.writes, "push "+branch+" "+sha)
		if g.pull == nil || g.pull.Head != branch {
			return
		}
		if g.pull.HeadSHA != sha {
			g.pull.HeadSHA = sha
			g.files++
			g.pull.Files = g.files
		}
		ok := true
		g.pull.Mergeable = &ok
	})
}

var errDown = errors.New("dial tcp: no route to github")

func (g *FakeGitHub) Me(context.Context) (string, error) { return "me", nil }

func (g *FakeGitHub) ListIssues(context.Context, string, string, time.Time, string) ([]forge.Issue, string, error) {
	return nil, "", nil
}

func (g *FakeGitHub) ListPulls(context.Context, string, string, string) ([]forge.Pull, string, error) {
	return nil, "", nil
}

func (g *FakeGitHub) Issue(_ context.Context, _, _ string, number int) (forge.Issue, error) {
	return forge.Issue{Number: number, State: "open", User: "me"}, nil
}

func (g *FakeGitHub) IssueComments(context.Context, string, string, int, int, int) ([]forge.Comment, error) {
	return nil, nil
}

func (g *FakeGitHub) Permission(context.Context, string, string, string) (string, error) {
	return "admin", nil
}

func (g *FakeGitHub) Repos(context.Context) ([]forge.RepoInfo, error) { return nil, nil }

func (g *FakeGitHub) Pull(_ context.Context, _, _ string, number int) (forge.Pull, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.down {
		return forge.Pull{}, errDown
	}
	if g.pull == nil || g.pull.Number != number {
		return forge.Pull{}, errors.New("no such pull request")
	}
	return *g.pull, nil
}

func (g *FakeGitHub) PullFiles(context.Context, string, string, int) ([]forge.FileChange, error) {
	return []forge.FileChange{{Path: "ledger.go", Additions: 2}}, nil
}

func (g *FakeGitHub) Checks(_ context.Context, _, _, sha string) (forge.CheckState, error) {
	state, _, err := g.CheckRuns(context.Background(), "", "", sha)
	return state, err
}

func (g *FakeGitHub) CheckRuns(_ context.Context, _, _, sha string) (forge.CheckState, []forge.CheckRun, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.down {
		return forge.ChecksNone, nil, errDown
	}
	runs := append([]forge.CheckRun(nil), g.runs[sha]...)
	if len(runs) == 0 {
		// A head nothing has reported on yet: its workflows are starting.
		return forge.ChecksRunning, []forge.CheckRun{{Name: "build-0", Status: "queued"}}, nil
	}
	failed, pending := false, false
	for _, r := range runs {
		switch {
		case r.Status != "completed":
			pending = true
		case r.Conclusion != "success":
			failed = true
		}
	}
	switch {
	case failed:
		return forge.ChecksFailed, runs, nil
	case pending:
		return forge.ChecksRunning, runs, nil
	}
	return forge.ChecksPassed, runs, nil
}

func (g *FakeGitHub) Reviews(context.Context, string, string, int) ([]forge.Review, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.down {
		return nil, errDown
	}
	return append([]forge.Review(nil), g.reviews...), nil
}

func (g *FakeGitHub) RequiredReviews(context.Context, string, string, string) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.required, nil
}

// PullFeedback is the pull request's review comments as one text.
func (g *FakeGitHub) PullFeedback(context.Context, string, string, int) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.down {
		return "", errDown
	}
	return strings.Join(g.comments, "\n"), nil
}

// ReadinessMaterial is the diff a final review reads: one file per head the
// pull request has carried, so it changes exactly when the head does.
func (g *FakeGitHub) ReadinessMaterial(context.Context, string, string, string, int) (string, []forge.RequiredCheck, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var b strings.Builder
	for i := 0; i < g.files; i++ {
		fmt.Fprintf(&b, "diff --git a/f%d.go b/f%d.go\n+package ledger\n", i, i)
	}
	return b.String(), nil, nil
}

func (g *FakeGitHub) MarkPullReady(_ context.Context, _, _ string, number int, head string) error {
	return g.MarkDraftPullReady(context.Background(), "", "", number, head)
}

func (g *FakeGitHub) MarkDraftPullReady(_ context.Context, _, _ string, number int, head string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pull == nil || g.pull.Number != number || g.pull.HeadSHA != head {
		return errors.New("the pull request's head moved")
	}
	g.pull.Draft = false
	g.writes = append(g.writes, "ready "+head)
	return nil
}

func (g *FakeGitHub) Comment(_ context.Context, _, _ string, number int, body string) (forge.Posted, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.writes = append(g.writes, fmt.Sprintf("comment #%d", number))
	return forge.Posted{URL: fmt.Sprintf("https://github.com/%s/%s/issues/%d#issuecomment-1", g.owner, g.repo, number), Number: number}, nil
}

func (g *FakeGitHub) AddLabels(_ context.Context, _, _ string, number int, _ []string) (forge.Posted, error) {
	return forge.Posted{Number: number}, nil
}

func (g *FakeGitHub) Close(_ context.Context, _, _ string, number int) (forge.Posted, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pull != nil && g.pull.Number == number {
		g.pull.State = "closed"
	}
	g.writes = append(g.writes, fmt.Sprintf("close #%d", number))
	return forge.Posted{Number: number}, nil
}

func (g *FakeGitHub) OpenPullRequest(_ context.Context, _, _, head string) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, branch, ok := strings.Cut(head, ":"); ok {
		head = branch
	}
	if g.pull != nil && g.pull.State == "open" && g.pull.Head == head {
		return g.pull.Number, nil
	}
	return 0, nil
}

func (g *FakeGitHub) DefaultBranch(context.Context, string, string) (string, error) {
	return "main", nil
}

func (g *FakeGitHub) CreatePullRequest(_ context.Context, _, _, head, base, title, body string, draft bool) (forge.Posted, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	sha := ""
	if g.headOf != nil {
		sha = g.headOf(head)
	}
	ok := true
	g.files = 1
	g.pull = &forge.Pull{
		Number: 7, Title: title, Body: body, User: "me", State: "open", Draft: draft,
		URL:  fmt.Sprintf("https://github.com/%s/%s/pull/7", g.owner, g.repo),
		Head: head, HeadSHA: sha, Base: base, BaseSHA: g.baseSHA(base), Mergeable: &ok, Files: 1,
	}
	g.writes = append(g.writes, "open pr "+head)
	return forge.Posted{URL: g.pull.URL, Number: 7}, nil
}

func (g *FakeGitHub) baseSHA(base string) string {
	if g.headOf == nil {
		return ""
	}
	return g.headOf(base)
}

func (g *FakeGitHub) UpdatePullRequest(_ context.Context, _, _ string, number int, title, body string) (forge.Posted, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pull == nil || g.pull.Number != number {
		return forge.Posted{}, errors.New("no such pull request")
	}
	g.pull.Title, g.pull.Body = title, body
	g.writes = append(g.writes, fmt.Sprintf("update pr #%d", number))
	return forge.Posted{URL: g.pull.URL, Number: number}, nil
}

func (g *FakeGitHub) MergeMethods(context.Context, string, string) ([]string, error) {
	return []string{"squash"}, nil
}

func (g *FakeGitHub) MergePullRequest(_ context.Context, _, _ string, number int, sha, _ string) (forge.Merged, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pull == nil || g.pull.Number != number {
		return forge.Merged{}, errors.New("no such pull request")
	}
	if g.pull.HeadSHA != sha {
		return forge.Merged{}, forge.ErrHeadMoved
	}
	if g.pull.Mergeable != nil && !*g.pull.Mergeable {
		return forge.Merged{}, forge.ErrNotMergeable
	}
	g.pull.State, g.pull.Merged, g.pull.MergedBy, g.pull.MergedAt = "closed", true, "me", time.Now().UTC()
	g.writes = append(g.writes, "merge "+sha)
	return forge.Merged{SHA: sha}, nil
}
