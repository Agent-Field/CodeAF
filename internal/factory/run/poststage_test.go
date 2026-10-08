package run

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

type fakeSource struct {
	mu  sync.Mutex
	got []factory.Action
	err error
}

func (f *fakeSource) Name() string { return "fake" }
func (f *fakeSource) Read(context.Context, string) ([]factory.Item, string, error) {
	return nil, "", nil
}
func (f *fakeSource) Write(_ context.Context, a factory.Action) (factory.Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, a)
	if f.err != nil {
		return factory.Receipt{}, f.err
	}
	return factory.Receipt{URL: "https://x/1"}, nil
}

func runPost(t *testing.T, src factory.Source, pol []string, it factory.Item, ask string, prior []factory.StageResult) (factory.StageResult, error, []string) {
	t.Helper()
	var logs []string
	ex := NewPostExecutor(func(string) factory.Source { return src }, func(string) []string { return pol })
	res, err := ex.Run(context.Background(), Job{Item: it, Stage: factory.Stage{Name: "post", Kind: factory.StagePost, Ask: ask}, Prior: prior,
		Log: func(l string) { logs = append(logs, l) }})
	return res, err, logs
}

func item() factory.Item {
	return factory.Item{ID: 3, Num: 12, Repo: "r", Title: "Fix the Parser!", Tier: factory.TierOwner,
		Proof:  []factory.Claim{{Text: "parses", OK: true}, {Text: "fast", OK: false}},
		Stages: []factory.Stage{{Name: "test", Kind: factory.StageCheck}, {Name: "post", Kind: factory.StagePost}}}
}

func TestVerbs(t *testing.T) {
	f := &fakeSource{}
	res, err, logs := runPost(t, f, nil, item(), "comment: hello there", nil)
	if err != nil || !res.Done || res.Claims[0].Evidence != "https://x/1" || res.Claims[0].Medium != "transcript" {
		t.Fatalf("%+v %v", res, err)
	}
	if f.got[0].Verb != "comment" || f.got[0].Body != "hello there" {
		t.Fatal(f.got[0])
	}
	if len(logs) != 1 || logs[0] != "posted comment · https://x/1" {
		t.Fatal(logs)
	}
	runPost(t, f, nil, item(), "label bug, p1", nil)
	if l := f.got[1].Labels; len(l) != 2 || l[0] != "bug" || l[1] != "p1" {
		t.Fatal(l)
	}
	runPost(t, f, nil, item(), "pr draft", nil)
	p := f.got[2]
	if !p.Draft || p.Branch != "factory/12-fix-the-parser" || !strings.Contains(p.Body, "✓ parses\n✕ fast") || !strings.HasSuffix(p.Body, "by codeaf's factory floor") {
		t.Fatalf("%+v", p)
	}
	runPost(t, f, nil, item(), "close", nil)
	if f.got[3].Verb != "close" {
		t.Fatal(f.got[3])
	}
}

func TestCommentBodies(t *testing.T) {
	f := &fakeSource{}
	runPost(t, f, nil, item(), "comment", []factory.StageResult{{Output: strings.Repeat("x", 3000)}})
	if len(f.got[0].Body) != 2000 {
		t.Fatal(len(f.got[0].Body))
	}
	runPost(t, f, nil, item(), "comment", nil)
	if f.got[1].Body != "✓ parses\n✕ fast" {
		t.Fatal(f.got[1].Body)
	}
}

func TestBranchFromStreamLog(t *testing.T) {
	it := item()
	it.Stream = &factory.Stream{Log: []factory.LogLine{{Text: "branch: factory/mine"}}}
	f := &fakeSource{}
	runPost(t, f, nil, it, "pr", nil)
	if f.got[0].Branch != "factory/mine" || f.got[0].Draft {
		t.Fatal(f.got[0])
	}
}

func TestUnknownVerbAndNilSource(t *testing.T) {
	f := &fakeSource{}
	_, err, _ := runPost(t, f, nil, item(), "shout", nil)
	if err == nil || err.Error() != "a post stage says comment, label, pr or close" || len(f.got) != 0 {
		t.Fatal(err)
	}
	ex := NewPostExecutor(func(string) factory.Source { return nil }, nil)
	_, err = ex.Run(context.Background(), Job{Item: item(), Stage: factory.Stage{Ask: "close"}})
	if err == nil || err.Error() != "#12 is not on a source codeaf can post to" {
		t.Fatal(err)
	}
}

func TestNoRetryOnError(t *testing.T) {
	f := &fakeSource{err: errors.New("boom")}
	_, err, _ := runPost(t, f, nil, item(), "comment hi", nil)
	if err == nil || len(f.got) != 1 {
		t.Fatal(err, len(f.got))
	}
}

func TestCancelled(t *testing.T) {
	f := &fakeSource{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ex := NewPostExecutor(func(string) factory.Source { return f }, nil)
	if _, err := ex.Run(ctx, Job{Item: item(), Stage: factory.Stage{Ask: "close"}}); err == nil || len(f.got) != 0 {
		t.Fatal(err)
	}
}

func TestPolicyRefusalResult(t *testing.T) {
	f := &fakeSource{}
	res, err, _ := runPost(t, f, []string{"Never close an issue."}, item(), "close", nil)
	if err != nil || res.Done || res.Output != "post refused: Never close an issue." || len(f.got) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	c := res.Claims[0]
	if c.Text != "close refused by policy" || c.OK || c.Medium != "policy" || c.Evidence != "Never close an issue." {
		t.Fatal(c)
	}
}

func TestPolicyShapes(t *testing.T) {
	green := []factory.StageResult{{Done: true, Exit: 0, Claims: []factory.Claim{{Medium: "test", OK: true}}}}
	redExit := []factory.StageResult{{Exit: 1}}
	redClaim := []factory.StageResult{{Claims: []factory.Claim{{Medium: "test", OK: false}}}}
	owner := item()
	stranger := item()
	stranger.Tier = factory.TierStranger
	cases := []struct {
		line   string
		it     factory.Item
		verb   string
		prior  []factory.StageResult
		allows bool
	}{
		{"Tests pass before anything posts.", owner, "comment", green, true},
		{"tests pass before anything posts", owner, "comment", redExit, false},
		{"TESTS PASS BEFORE ANYTHING POSTS", owner, "label", redClaim, false},
		{"nothing posts to a stranger", stranger, "comment", green, false},
		{"Nothing posts to a stranger.", owner, "comment", green, true},
		{"nothing posts to a stranger", stranger, "label", green, true},
		{"No pull request without a green check", owner, "pr", nil, false},
		{"No pull request without a green check", owner, "pr", redExit, false},
		{"No pull request without a green check", owner, "pr", green, true},
		{"No pull request without a green check", owner, "comment", nil, true},
		{"never close an issue", owner, "close", green, false},
		{"never close an issue", owner, "pr", green, true},
		{"a stranger's PR never runs write", stranger, "comment", nil, true},
		{"something the model reads", owner, "close", redExit, true},
	}
	for _, c := range cases {
		ok, why := PolicyAllows([]string{c.line}, c.it, factory.Action{Verb: c.verb}, c.prior)
		if ok != c.allows || (!ok && why != c.line) {
			t.Errorf("%q %s: ok=%v why=%q", c.line, c.verb, ok, why)
		}
	}
}

func TestPolicyAllowsStage(t *testing.T) {
	pol := []string{"A stranger's PR never runs write."}
	st := item()
	st.Tier = factory.TierStranger
	if ok, why := PolicyAllowsStage(pol, st, factory.Stage{Name: "write"}); ok || why != pol[0] {
		t.Fatal(ok, why)
	}
	if ok, _ := PolicyAllowsStage(pol, st, factory.Stage{Name: "test"}); !ok {
		t.Fatal("test refused")
	}
	if ok, _ := PolicyAllowsStage(pol, item(), factory.Stage{Name: "write"}); !ok {
		t.Fatal("owner refused")
	}
	if ok, _ := PolicyAllowsStage(nil, st, factory.Stage{Name: "write"}); !ok {
		t.Fatal("no policy refused")
	}
}

func TestUnknownPolicy(t *testing.T) {
	got := UnknownPolicy([]string{"never close an issue", "keep it kind", "", "Tests pass before anything posts."})
	if len(got) != 1 || got[0] != "keep it kind" {
		t.Fatal(got)
	}
}
