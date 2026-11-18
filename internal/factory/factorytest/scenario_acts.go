package factorytest

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// ── PUBLIC ACTS IN A SCENARIO ───────────────────────────────────────────────
//
// A scenario that carries out public acts signs its person in to GitHub
// ([Scenario.SignIn]) and reads review threads the way the real client does
// ([Scenario.TrackThreads]). The acts are then sent by the service's own
// sender to the scenario's [FakeGitHub], which keeps each as a write and the
// words of every reply and comment.

// reviewThread is one review thread on the fake pull request: its first
// comment, who wrote it, and whether it is resolved.
type reviewThread struct {
	node        string
	comment     int64
	by          string
	association string
	body        string
	resolved    bool
}

// threadedGitHub is the fake forge with its review threads read as the real
// client reads them: each with its ids, its author and their standing. A
// scenario that never adds a thread keeps the plain forge, whose feedback is
// one text.
type threadedGitHub struct{ *FakeGitHub }

// scenarioToken is the sign-in a signed-in scenario's acts are sent with.
const scenarioToken = "scenario-person-token"

// SignIn signs the scenario's person in to GitHub on the service, so public
// acts are carried out. The service is opened again with the sign-in, as the
// app is when its person connects GitHub.
func (s *Scenario) SignIn() {
	s.T.Helper()
	s.configure = func(c *serve.Config) {
		c.Mode = api.FactorySolo
		c.GitHubToken = func(context.Context) string { return scenarioToken }
		c.ActsClient = func(token string) factorygithub.API {
			if token != scenarioToken {
				s.T.Errorf("a public act was sent with the token %q", token)
			}
			return threadedGitHub{s.GitHub}
		}
		// Nothing of a scenario reaches the network: every door that would
		// call GitHub itself is refused its transport.
		c.GitHubHTTPClient = &http.Client{Transport: noNetwork{}}
	}
	s.RestartService()
}

type noNetwork struct{}

func (noNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("a scenario has no network")
}

// TrackThreads makes the factory read the pull request's review threads one
// by one, as it does from GitHub itself. It is called before the first event.
func (s *Scenario) TrackThreads() {
	s.source = factorygithub.New(threadedGitHub{s.GitHub}, []string{scenarioOwner + "/" + scenarioRepo}, s.clock.Now)
}

// Act proposes public acts on the job through the service's own door, as by:
// the job's manager ([factory.ActByMainAgent]) or its person.
func (s *Scenario) Act(by string, acts ...api.ActAsk) Pressed {
	return s.Post(api.ActVerb, api.ActsRequest{Acts: acts, By: by, Why: "scenario"})
}

// ActsSent waits until the service's sender has nothing in hand for the job
// and answers it. The service sends on a request or on its timer; the
// scenario stands in for both.
func (s *Scenario) ActsSent() factory.Item {
	s.T.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		s.Tick()
		it := s.Item()
		_, due := factory.NextAct(it, s.clock.Now())
		running := false
		for _, a := range it.Acts {
			running = running || a.State == factory.ActRunning
		}
		if !due && !running {
			return it
		}
		if time.Now().After(deadline) {
			s.T.Fatalf("the job's acts never came to rest: %+v\n%s", it.Acts, s.Story())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// AddThread is a reviewer opening a review thread on the pull request's
// code. association is GitHub's word for their standing (OWNER, MEMBER,
// COLLABORATOR, CONTRIBUTOR, NONE). It answers the name the job stores the
// thread under, which a reply names.
func (g *FakeGitHub) AddThread(by, association, body string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	id := int64(501 + len(g.threads))
	g.threads = append(g.threads, reviewThread{node: fmt.Sprintf("PRRT_%d", id), comment: id, by: by, association: association, body: body})
	return fmt.Sprintf("inline:%d", id)
}

// Posted is every reply and comment the factory posted, word for word,
// oldest first.
func (g *FakeGitHub) Posted() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.posted...)
}

// Resolved says whether the review thread stored as id is resolved.
func (g *FakeGitHub) Resolved(id string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, t := range g.threads {
		if fmt.Sprintf("inline:%d", t.comment) == id {
			return t.resolved
		}
	}
	return false
}

// HoldReplies makes the next reply reach GitHub and then wait for its caller
// to let go, as a reply does when the service closes mid-send. The channel
// it answers is closed when a reply is waiting.
func (g *FakeGitHub) HoldReplies(on bool) <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.holdReplies = on
	if on {
		g.replyHeld = make(chan struct{})
	}
	return g.replyHeld
}

// PullFeedbackState is the pull request's review threads as items, and as
// the text the factory hands its manager and delegates. A reply the factory
// posted begins with the publication marker and is never feedback.
func (g threadedGitHub) PullFeedbackState(context.Context, string, string, int) (forge.ReviewFeedback, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.down {
		return forge.ReviewFeedback{}, errDown
	}
	var out forge.ReviewFeedback
	var b strings.Builder
	for _, t := range g.threads {
		id := fmt.Sprintf("inline:%d", t.comment)
		out.Items = append(out.Items, forge.FeedbackItem{ID: id, Revision: fmt.Sprintf("%x", sha256.Sum256([]byte(t.body))), Resolved: t.resolved,
			Thread: t.node, CommentID: t.comment, ReplyTo: t.comment, By: t.by, Association: t.association})
		if t.resolved {
			continue
		}
		if forge.Untrusted(t.association) {
			fmt.Fprintf(&b, "[%s]\n", forge.UntrustedNote)
		}
		fmt.Fprintf(&b, "Review comment %d (thread %s) by @%s ledger.go:1: %s\n", t.comment, id, t.by, t.body)
	}
	for i, words := range g.comments {
		id := fmt.Sprintf("conversation:%d", 601+i)
		out.Items = append(out.Items, forge.FeedbackItem{ID: id, Revision: fmt.Sprintf("%x", sha256.Sum256([]byte(words))), CommentID: int64(601 + i), By: "reviewer", Association: "COLLABORATOR"})
		fmt.Fprintf(&b, "PR conversation comment %d (thread %s) by @reviewer: %s\n", 601+i, id, words)
	}
	out.Text = b.String()
	return out, nil
}

func (g *FakeGitHub) EditPullRequest(_ context.Context, _, _ string, number int, edit forge.PullEdit) (forge.Pull, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pull == nil || g.pull.Number != number {
		return forge.Pull{}, errors.New("no such pull request")
	}
	line := fmt.Sprintf("edit pr #%d", number)
	if edit.Title != nil {
		g.pull.Title, line = *edit.Title, line+" title"
	}
	if edit.Body != nil {
		g.pull.Body, line = *edit.Body, line+" body"
	}
	if edit.Base != nil {
		if g.baseSHA(*edit.Base) == "" {
			return forge.Pull{}, &forge.APIError{StatusCode: http.StatusUnprocessableEntity, Body: `{"message":"Validation Failed","errors":[{"resource":"PullRequest","field":"base","code":"invalid"}]}`}
		}
		g.pull.Base, g.pull.BaseSHA, line = *edit.Base, g.baseSHA(*edit.Base), line+" base="+*edit.Base
	}
	g.writes = append(g.writes, line)
	return *g.pull, nil
}

func (g *FakeGitHub) ReplyToReviewComment(ctx context.Context, _, _ string, _ int, comment int64, body string) (forge.Posted, error) {
	g.mu.Lock()
	found := false
	for _, t := range g.threads {
		found = found || t.comment == comment
	}
	if !found {
		g.mu.Unlock()
		return forge.Posted{}, &forge.APIError{StatusCode: http.StatusNotFound, Body: `{"message":"Not Found"}`}
	}
	g.writes = append(g.writes, fmt.Sprintf("reply inline:%d", comment))
	g.posted = append(g.posted, body)
	hold, held := g.holdReplies, g.replyHeld
	g.holdReplies = false
	g.mu.Unlock()
	if hold {
		close(held)
		<-ctx.Done()
		return forge.Posted{}, ctx.Err()
	}
	return forge.Posted{URL: fmt.Sprintf("https://github.com/%s/%s/pull/7#discussion_r%d", g.owner, g.repo, comment+1000)}, nil
}

func (g *FakeGitHub) CommentOnce(_ context.Context, _, _ string, number int, body string) (forge.Posted, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.writes = append(g.writes, fmt.Sprintf("comment #%d", number))
	g.posted = append(g.posted, body)
	return forge.Posted{URL: fmt.Sprintf("https://github.com/%s/%s/pull/%d#issuecomment-%d", g.owner, g.repo, number, len(g.posted)), Number: number}, nil
}

func (g *FakeGitHub) ResolveReviewThread(_ context.Context, _, _, thread string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := range g.threads {
		if g.threads[i].node == thread {
			g.threads[i].resolved = true
			g.writes = append(g.writes, fmt.Sprintf("resolve inline:%d", g.threads[i].comment))
			return nil
		}
	}
	return &forge.ThreadRefusal{Gone: true, Message: "GitHub refused to resolve the thread: Could not resolve to a node"}
}

func (g *FakeGitHub) RequestReviewers(_ context.Context, _, _ string, _ int, reviewers []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.writes = append(g.writes, "rerequest "+strings.Join(reviewers, ","))
	return nil
}

// RerunFailedChecks starts every failed check on sha again: each reads as
// under way until the scenario says how it ended.
func (g *FakeGitHub) RerunFailedChecks(_ context.Context, _, _, sha string) (forge.Rerun, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out forge.Rerun
	runs := append([]forge.CheckRun(nil), g.runs[sha]...)
	for i, r := range runs {
		if r.Status == "completed" && r.Conclusion != "success" {
			runs[i].Status, runs[i].Conclusion = "in_progress", ""
			out.Ran = append(out.Ran, r.Name)
		}
	}
	g.runs[sha] = runs
	g.writes = append(g.writes, "rerun checks "+strings.Join(out.Ran, ","))
	return out, nil
}
