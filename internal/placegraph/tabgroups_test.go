package placegraph

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func gitRepo(t *testing.T, origin string) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if origin != "" {
		add := exec.Command("git", "remote", "add", "origin", origin)
		add.Dir = dir
		if out, err := add.CombinedOutput(); err != nil {
			t.Fatalf("git remote: %v %s", err, out)
		}
	}
	return dir
}

func openGate(perDay, everyMinutes int) *TopicGate {
	when := time.Unix(1_700_000_000, 0).UTC()
	return &TopicGate{
		Now: func() time.Time { return when },
		Policy: func() RecommendPolicy {
			p := DefaultRecommendPolicy()
			p.ClusterCallsPerDay = perDay
			p.OrganizeEveryMinutes = everyMinutes
			return p
		},
	}
}

type countingAsk struct {
	mu    sync.Mutex
	calls int
	raw   string
	err   error
	last  ModelRequest
}

func (c *countingAsk) ask(ctx context.Context, req ModelRequest) (string, error) {
	c.mu.Lock()
	c.calls++
	c.last = req
	c.mu.Unlock()
	if c.err != nil {
		return "", c.err
	}
	return c.raw, nil
}

func (c *countingAsk) n() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestCanonicalRepoIsTheGitRootAndNotTheHomeDirectory(t *testing.T) {
	origin := "https://example.com/acme/widgets.git"
	root := gitRepo(t, origin)
	sub := filepath.Join(root, "internal", "parse")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	other := gitRepo(t, origin)
	plain := t.TempDir()
	home := gitRepo(t, "https://example.com/acme/home.git")

	key, name, ok := CanonicalRepo(sub, "/somewhere/else")
	if !ok || name != filepath.Base(root) {
		t.Fatalf("subdir identity = %q %q ok=%v", key, name, ok)
	}
	otherKey, _, otherOK := CanonicalRepo(other, "/somewhere/else")
	if !otherOK || otherKey != key {
		t.Fatalf("two clones of one origin: %q and %q", key, otherKey)
	}
	if _, _, ok := CanonicalRepo(plain, "/somewhere/else"); ok {
		t.Fatal("a folder that is not a git repository was given a repo identity")
	}
	if _, _, ok := CanonicalRepo(home, home); ok {
		t.Fatal("the home directory was treated as a repository")
	}
	if _, _, ok := CanonicalRepo("  ", home); ok {
		t.Fatal("an empty workspace was treated as a repository")
	}
}

func TestThreeChatsInOneRepoOfferAndUnrelatedHomeChatsStayLoose(t *testing.T) {
	repo := gitRepo(t, "https://example.com/acme/widgets.git")
	if err := os.MkdirAll(filepath.Join(repo, "cmd"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	home := gitRepo(t, "https://example.com/acme/house.git")
	ask := &countingAsk{raw: `{"belong":false,"chats":[],"name":""}`}
	chats := []TabChat{
		{ID: "aaaa000000000001", Title: "Lexer", Recap: "Strict mode", Workspace: filepath.Join(repo, "cmd")},
		{ID: "bbbb000000000002", Title: "Parser", Recap: "Errors", Workspace: repo},
		{ID: "cccc000000000003", Title: "Tokens", Recap: "Names", Workspace: filepath.Join(repo, "internal")},
		{ID: "dddd000000000004", Title: "Taxes", Recap: "Unrelated", Workspace: home},
		{ID: "eeee000000000005", Title: "Garden", Recap: "Also home", Workspace: home},
		{ID: "ffff000000000006", Title: "Recipes", Recap: "Also home", Workspace: home},
	}
	offers := GroupOffers(context.Background(), home, chats, ask.ask, openGate(6, 0))
	if ask.n() != 1 {
		t.Fatalf("unclaimed home chats need one semantic check, got %d", ask.n())
	}
	if len(offers) != 1 || offers[0].Basis != "repo" || len(offers[0].IDs) != 3 || offers[0].Title != filepath.Base(repo) {
		t.Fatalf("offers = %+v", offers)
	}
	for _, id := range offers[0].IDs {
		if id == "dddd000000000004" || strings.HasPrefix(id, "eeee") || strings.HasPrefix(id, "ffff") {
			t.Fatalf("a home chat joined the repo offer: %+v", offers[0])
		}
	}
	homeOnly := []TabChat{
		{ID: "dddd000000000004", Title: "Taxes", Recap: "Unrelated", Workspace: home},
		{ID: "eeee000000000005", Title: "Garden", Recap: "Also home", Workspace: home},
		{ID: "ffff000000000006", Title: "Recipes", Recap: "Also home", Workspace: home},
	}
	if got := GroupOffers(context.Background(), home, homeOnly, ask.ask, openGate(6, 0)); len(got) != 0 || ask.n() != 2 {
		t.Fatalf("home chats offered %+v after %d model calls", got, ask.n())
	}
}

func TestRelatedChatsAcrossWorkspacesGroupOnlyFromTheModel(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	ask := &countingAsk{raw: `{"belong":true,"chats":["c1","c3","c4"],"name":"Drip irrigation"}`}
	chats := []TabChat{
		{ID: "1111111111111111", Title: "Tomato drip in July", Recap: "Morning cycles", Workspace: left},
		{ID: "2222222222222222", Title: "Roth versus traditional", Recap: "No decision", Workspace: left},
		{ID: "3333333333333333", Title: "Emitter clogs", Recap: "Hard water", Workspace: right},
		{ID: "4444444444444444", Title: "Quarter-inch tubing", Recap: "Spurs", Workspace: right},
	}
	offers := GroupOffers(context.Background(), "/home/person", chats, ask.ask, openGate(6, 0))
	if ask.n() != 1 || ask.last.Role != "placesuggest" {
		t.Fatalf("calls=%d role=%q", ask.n(), ask.last.Role)
	}
	if !strings.Contains(ask.last.User, "Tomato drip in July") || strings.Contains(ask.last.User, left) {
		t.Fatal("the question must carry the recorded title and recap, not a workspace path")
	}
	if len(offers) != 1 || offers[0].Basis != "topic" || offers[0].Title != "Drip irrigation" {
		t.Fatalf("offers = %+v", offers)
	}
	if strings.Join(offers[0].IDs, ",") != "1111111111111111,3333333333333333,4444444444444444" {
		t.Fatalf("ids = %v", offers[0].IDs)
	}
}

func TestASecondLookAtTheSameSetDoesNotAskAgain(t *testing.T) {
	gate := openGate(6, 0)
	ask := &countingAsk{raw: `{"belong":false,"chats":[],"name":""}`}
	chats := threeLoose()
	if got := GroupOffers(context.Background(), "/home/person", chats, ask.ask, gate); len(got) != 0 {
		t.Fatalf("belong false still offered %+v", got)
	}
	if got := GroupOffers(context.Background(), "/home/person", chats, ask.ask, gate); len(got) != 0 || ask.n() != 1 {
		t.Fatalf("second look offered %+v after %d calls", got, ask.n())
	}
}

func TestUnrelatedChatsStaySeparate(t *testing.T) {
	ask := &countingAsk{raw: `{"belong":false,"chats":[],"name":""}`}
	if got := GroupOffers(context.Background(), "/home/person", threeLoose(), ask.ask, openGate(6, 0)); len(got) != 0 {
		t.Fatalf("offers = %+v", got)
	}
}

func TestABadTopicAnswerIsNoOffer(t *testing.T) {
	chats := threeLoose()
	cases := []string{
		`{"belong":true,"chats":["c1","c2","c9"],"name":"Widgets"}`,
		`{"belong":true,"chats":["c1","c2"],"name":"Widgets"}`,
		`{"belong":true,"chats":["c1","c2","c3"],"name":"New group"}`,
		`{"belong":true,"chats":["c1","c2","c3"],"name":"http://evil.example/x"}`,
		`{"belong":true,"chats":["c1","c2","c3"],"name":"Widgets","under":"root"}`,
		`{"belong":true,"chats":["c1","c1","c2"],"name":"Widgets"}`,
		`not json`,
		`{"belong":true,"chats":["aaaa000000000001"],"name":"Widgets"}`,
	}
	for _, raw := range cases {
		offer, err := ReadTopicAnswer(raw, chats)
		if offer != nil || !errors.Is(err, ErrBadAnswer) {
			t.Errorf("raw %s → offer %+v err %v", raw, offer, err)
		}
		ask := &countingAsk{raw: raw}
		if got := GroupOffers(context.Background(), "/home/person", chats, ask.ask, openGate(6, 0)); len(got) != 0 {
			t.Errorf("raw %s still offered %+v", raw, got)
		}
	}
}

func TestTimeoutAndAZeroBudgetCallNothingUseful(t *testing.T) {
	chats := threeLoose()
	timed := &countingAsk{err: context.DeadlineExceeded}
	if got := GroupOffers(context.Background(), "/home/person", chats, timed.ask, openGate(6, 0)); len(got) != 0 || timed.n() != 1 {
		t.Fatalf("timeout offered %+v after %d calls", got, timed.n())
	}
	blocked := &countingAsk{raw: `{"belong":true,"chats":["c1","c2","c3"],"name":"Widgets"}`}
	if got := GroupOffers(context.Background(), "/home/person", chats, blocked.ask, openGate(0, 0)); len(got) != 0 || blocked.n() != 0 {
		t.Fatalf("a zero budget offered %+v after %d calls", got, blocked.n())
	}
	spaced := openGate(6, 60)
	first := &countingAsk{raw: `{"belong":true,"chats":["c1","c2","c3"],"name":"Widgets"}`}
	if got := GroupOffers(context.Background(), "/home/person", chats, first.ask, spaced); len(got) != 1 {
		t.Fatalf("first offer = %+v", got)
	}
	other := []TabChat{
		{ID: "aaaa00000000000a", Title: "One", Recap: "A", Workspace: "/work/a"},
		{ID: "bbbb00000000000b", Title: "Two", Recap: "B", Workspace: "/work/b"},
		{ID: "cccc00000000000c", Title: "Three", Recap: "C", Workspace: "/work/c"},
	}
	second := &countingAsk{raw: `{"belong":true,"chats":["c1","c2","c3"],"name":"Other"}`}
	if got := GroupOffers(context.Background(), "/home/person", other, second.ask, spaced); len(got) != 0 || second.n() != 0 {
		t.Fatalf("a set inside the organizing interval offered %+v after %d calls", got, second.n())
	}
}

func TestOneSetSpendsOneCallUnderRace(t *testing.T) {
	gate := openGate(6, 0)
	var calls atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, call := gate.take("topic:same")
			if call {
				calls.Add(1)
				gate.finish("topic:same", &TabOffer{Basis: "topic", Key: "topic:same", IDs: []string{"a", "b", "c"}, Title: "Widgets"})
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("spent %d calls on one set", calls.Load())
	}
}

func threeLoose() []TabChat {
	return []TabChat{
		{ID: "aaaa000000000001", Title: "Alpha", Recap: "First", Workspace: "/work/a"},
		{ID: "bbbb000000000002", Title: "Beta", Recap: "Second", Workspace: "/work/b"},
		{ID: "cccc000000000003", Title: "Gamma", Recap: "Third", Workspace: "/work/c"},
	}
}

func TestTopicQuestionUsesTheOrganizingRole(t *testing.T) {
	req := TopicQuestion(threeLoose())
	if req.Role != "placesuggest" || !strings.Contains(req.User, "c1: Alpha — First") || strings.Contains(req.User, "/work/a") {
		t.Fatalf("question = %+v", req)
	}
}

func TestHomeWorkspaceTopicGroupsConcreteSubjectOnly(t *testing.T) {
	home := t.TempDir()
	chats := []TabChat{
		{ID: "1111111111111111", Title: "Tomato drip timing", Workspace: home},
		{ID: "2222222222222222", Title: "Drip emitter clogs", Workspace: home},
		{ID: "3333333333333333", Title: "Garden tubing pressure", Workspace: home},
		{ID: "4444444444444444", Title: "Drip timer rain delay", Workspace: home},
		{ID: "5555555555555555", Title: "Drip irrigation zones", Workspace: home},
		{ID: "6666666666666666", Title: "Roth conversion", Workspace: home},
		{ID: "7777777777777777", Title: "Buttermilk biscuits", Workspace: home},
		{ID: "8888888888888888", Title: "Hip stretching", Workspace: home},
	}
	ask := &countingAsk{raw: `{"belong":true,"chats":["c1","c2","c3","c4","c5"],"name":"Drip irrigation"}`}
	gate := openGate(6, 0)
	offers := GroupOffers(context.Background(), home, chats, ask.ask, gate)
	if len(offers) != 1 || offers[0].Basis != "topic" || len(offers[0].IDs) != 5 || ask.n() != 1 {
		t.Fatalf("home topic: %+v calls=%d", offers, ask.n())
	}
	if strings.Contains(ask.last.User, home) || !strings.Contains(ask.last.System, "Sharing a tool") {
		t.Fatal("question must use semantic evidence, not the common home folder")
	}
	for i, id := range offers[0].IDs {
		if id != chats[i].ID {
			t.Fatalf("unrelated chat grouped: %+v", offers)
		}
	}
	GroupOffers(context.Background(), home, chats, ask.ask, gate)
	if ask.n() != 1 {
		t.Fatalf("same home set asked again: %d", ask.n())
	}
}

func TestRepoOnlyOfferSpendsNoModelCall(t *testing.T) {
	repo := gitRepo(t, "https://example.com/acme/widgets.git")
	chats := threeLoose()
	for i := range chats {
		chats[i].Workspace = repo
	}
	ask := &countingAsk{raw: `{"belong":false,"chats":[],"name":""}`}
	offers := GroupOffers(context.Background(), t.TempDir(), chats, ask.ask, openGate(6, 0))
	if len(offers) != 1 || offers[0].Basis != "repo" || ask.n() != 0 {
		t.Fatalf("repo-only offers=%+v calls=%d", offers, ask.n())
	}
}
