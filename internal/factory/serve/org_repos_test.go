package serve

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// orgRepoScene is a password-only organization (film D10): santosh is the
// operator, abir can edit the Webhook workstream and kim can only view it.
// The server's own GitHub grant has read acme/api and acme/web; nothing is on.
type orgRepoScene struct {
	*pwOrg
	santosh, abir, kim string
	ws                 api.Workstream
}

func newOrgRepoScene(t *testing.T) *orgRepoScene {
	t.Helper()
	o := newPWOrgWith(t, func(s *factory.Seam, st *store.Store) {
		s.NewJob = func(repo, title, _ string) (int, error) {
			it, err := st.Create(factory.Item{Repo: repo, Title: title, State: factory.StateNew})
			return it.ID, err
		}
		s.Repos = func(context.Context) ([]string, []factory.RepoInfo, error) {
			watched, err := st.Repos()
			saved, _ := st.AvailableRepos()
			return watched, saved.Repos, err
		}
	})
	if err := o.st.SetAvailableRepos(store.AvailableRepos{Identity: "acme-bot", Credential: "grant", ReadAt: o.clock(),
		Repos: []factory.RepoInfo{{Full: "acme/api", Owner: "acme", Name: "api"}, {Full: "acme/web", Owner: "acme", Name: "web"}}}); err != nil {
		t.Fatal(err)
	}
	sc := &orgRepoScene{pwOrg: o}
	sc.santosh = o.signUp(o.operatorLink(), "santosh", pwOperator)
	sc.abir = o.signUp(o.invite(sc.santosh), "abir", pwAbir)
	sc.kim = o.signUp(o.invite(sc.santosh), "kim", "a quiet harbour at low tide")
	if w := o.do("POST", "/v1/workstreams", sc.santosh, api.WorkstreamNew{Name: "Webhook reliability"}, ""); w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &sc.ws) != nil {
		t.Fatalf("workstream = %d %s", w.Code, w.Body)
	}
	shares := []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}, {Kind: "person", ID: "kim", Role: "view"}}
	if w := o.do("PUT", "/v1/workstreams/"+sc.ws.ID+"/shares", sc.santosh, api.WorkstreamSharesPut{Shares: &shares, Rev: &sc.ws.Rev}, ""); w.Code != 200 {
		t.Fatalf("shares = %d %s", w.Code, w.Body)
	}
	return sc
}

// turnOn is the operator turning repo on in Repos.
func (sc *orgRepoScene) turnOn(t *testing.T, repo string) {
	t.Helper()
	if w := sc.do("PUT", "/v1/repos/"+repo, sc.santosh, map[string]any{"on": true}, ""); w.Code != 200 {
		t.Fatalf("operator turns %s on = %d %s", repo, w.Code, w.Body)
	}
}

// pickable is the repositories who's queue offers to make a job on.
func (sc *orgRepoScene) pickable(t *testing.T, token string) []string {
	t.Helper()
	w := sc.do("GET", "/v1/queue", token, nil, "")
	var q struct {
		Repos []struct {
			Name string `json:"name"`
		} `json:"repos"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &q) != nil {
		t.Fatalf("queue = %d %s", w.Code, w.Body)
	}
	var out []string
	for _, r := range q.Repos {
		out = append(out, r.Name)
	}
	return out
}

// A PASSWORD MEMBER MAKES A JOB ON THE ORGANIZATION'S REPOSITORY: once the
// operator turns acme/api on, abir (no GitHub at all) is offered it, makes a
// job in the workstream abir can edit, and the job is in that workstream.
func TestAPasswordMemberMakesAJobOnAnOrganizationRepository(t *testing.T) {
	sc := newOrgRepoScene(t)
	if got := sc.pickable(t, sc.abir); len(got) != 0 {
		t.Fatalf("before anything is on abir is offered %q", got)
	}
	sc.turnOn(t, "acme/api")
	if got := sc.pickable(t, sc.abir); !slices.Equal(got, []string{"acme/api"}) {
		t.Fatalf("abir is offered %q; want only acme/api (acme/web is not on)", got)
	}
	w := sc.do("POST", "/v1/jobs", sc.abir, api.NewJob{Repo: "acme/api", Title: "Retry webhook deliveries", Source: factory.AskApp, Workstream: sc.ws.ID}, "")
	if w.Code != 201 {
		t.Fatalf("abir's job = %d %s", w.Code, w.Body)
	}
	var made struct{ ID int }
	_ = json.Unmarshal(w.Body.Bytes(), &made)
	ws, ok, err := sc.st.Workstream(sc.ws.ID)
	if err != nil || !ok || !slices.Contains(ws.Jobs, made.ID) {
		t.Fatalf("workstream jobs = %v (%v); want job %d in it", ws.Jobs, err, made.ID)
	}
	// kim, who views the workstream, reads the job abir made there.
	if w := sc.do("GET", "/v1/jobs/"+itoa(made.ID), sc.kim, nil, ""); w.Code != 200 {
		t.Fatalf("kim reads the job = %d %s", w.Code, w.Body)
	}
	// A repository the operator did not turn on is still nobody's to use.
	if w := sc.do("POST", "/v1/jobs", sc.abir, api.NewJob{Repo: "acme/web", Title: "x", Workstream: sc.ws.ID}, ""); w.Code != 404 {
		t.Fatalf("a job on acme/web, which is off = %d %s", w.Code, w.Body)
	}
}

// A VIEW-ONLY MEMBER SEES THE JOBS AND MAKES NONE: kim is refused in the
// workstream kim only views, and refused outside any workstream, because kim
// reads acme/api only through the organization. The refusal names what exists:
// kim, who edits no workstream, is told to ask for edit or ask the operator;
// abir, who edits one, is told to choose it (Factory film D22).
func TestAViewOnlyMemberCannotMakeAJobOnAnOrganizationRepository(t *testing.T) {
	sc := newOrgRepoScene(t)
	sc.turnOn(t, "acme/api")
	if w := sc.do("POST", "/v1/jobs", sc.kim, api.NewJob{Repo: "acme/api", Title: "mine", Workstream: sc.ws.ID}, ""); w.Code != 403 {
		t.Fatalf("kim in a workstream kim views = %d %s", w.Code, w.Body)
	}
	w := sc.do("POST", "/v1/jobs", sc.kim, api.NewJob{Repo: "acme/api", Title: "mine"}, "")
	if w.Code != 403 || !strings.Contains(w.Body.String(), NewJobNoWorkstream) {
		t.Fatalf("kim outside any workstream = %d %s", w.Code, w.Body)
	}
	// abir too must name the workstream: the organization's repository is not
	// a place to leave a job nobody's workstream holds.
	if w := sc.do("POST", "/v1/jobs", sc.abir, api.NewJob{Repo: "acme/api", Title: "loose"}, ""); w.Code != 403 || !strings.Contains(w.Body.String(), NewJobNeedsWorkstream) {
		t.Fatalf("abir outside any workstream = %d %s", w.Code, w.Body)
	}
	items, _ := sc.st.List()
	if len(items) != 0 {
		t.Fatalf("a refused create left %d jobs", len(items))
	}
	// The operator may make one anywhere.
	if w := sc.do("POST", "/v1/jobs", sc.santosh, api.NewJob{Repo: "acme/api", Title: "operator's", Source: factory.AskApp}, ""); w.Code != 201 {
		t.Fatalf("operator = %d %s", w.Code, w.Body)
	}
}

// ONLY THE OPERATOR TURNS A REPOSITORY ON OR OFF FOR THE ORGANIZATION. The
// operator, signed in with a password, sees what the server's grant reached;
// a member sees none of it until it is on, and is refused either way.
func TestOnlyTheOperatorTurnsAnOrganizationRepositoryOn(t *testing.T) {
	sc := newOrgRepoScene(t)
	read := func(token string) (watched, available []string) {
		t.Helper()
		w := sc.do("GET", "/v1/repos", token, nil, "")
		var out struct {
			Watched   []string `json:"watched"`
			Available []struct {
				Full string `json:"full"`
			} `json:"available"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("repos = %d %s", w.Code, w.Body)
		}
		for _, r := range out.Available {
			available = append(available, r.Full)
		}
		return out.Watched, available
	}
	if _, got := read(sc.santosh); !slices.Equal(got, []string{"acme/api", "acme/web"}) {
		t.Fatalf("the operator's Repos offers %q", got)
	}
	if _, got := read(sc.abir); len(got) != 0 {
		t.Fatalf("abir's Repos offers %q before anything is on", got)
	}
	if w := sc.do("PUT", "/v1/repos/acme/api", sc.abir, map[string]any{"on": true}, ""); w.Code != 403 {
		t.Fatalf("abir turns a repository on = %d %s", w.Code, w.Body)
	}
	if w := sc.do("PUT", "/v1/repos", sc.abir, map[string]any{"repos": []string{"acme/api"}}, ""); w.Code != 403 {
		t.Fatalf("abir sets the whole list = %d %s", w.Code, w.Body)
	}
	if watched, _ := read(sc.santosh); len(watched) != 0 {
		t.Fatalf("a refused turn-on left %q on", watched)
	}
	sc.turnOn(t, "acme/api")
	if watched, available := read(sc.abir); !slices.Equal(watched, []string{"acme/api"}) || !slices.Equal(available, []string{"acme/api"}) {
		t.Fatalf("abir after the turn-on reads watched %q available %q", watched, available)
	}
	if w := sc.do("PUT", "/v1/repos/acme/api", sc.abir, map[string]any{"on": false}, ""); w.Code != 403 {
		t.Fatalf("abir turns it off = %d %s", w.Code, w.Body)
	}
}

// A PERSON'S OWN GITHUB TOKEN FILTERS EXACTLY AS BEFORE, and the
// organization's repositories are added to it: mia's token reads acme/mine
// and nothing else; mia sees acme/mine, the organization's acme/api, and not
// acme/secret, which is neither. A job on a repository mia's own token reads
// still needs no workstream.
func TestAPersonsOwnTokenStillFiltersAndTheOrganizationOnlyAdds(t *testing.T) {
	sc := newOrgRepoScene(t)
	g := &fakeGitHub{t: t, login: "mia", repoStatus: 404}
	sc.h.config.GitHubClientID, sc.h.config.GitHubHTTPClient = "client", g.client()
	row, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	if err := tokens.Add(sc.cfg.TokensPath, row); err != nil {
		t.Fatal(err)
	}
	mia := signInHandle(t, sc.h, g, &sc.now, "mia", row.Token)
	asked := map[string]int{}
	sc.h.config.GitHubHTTPClient = &http.Client{Transport: githubTransport(func(r *http.Request) (*http.Response, error) {
		asked[r.URL.Path]++
		status := http.StatusNotFound
		if r.URL.Path == "/repos/acme/mine" || r.URL.Path == "/repos/acme/api" {
			status = http.StatusOK
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{}, Request: r}, nil
	})}
	sc.turnOn(t, "acme/api")
	var made []factory.Item
	for _, it := range []factory.Item{{Repo: "acme/mine", Title: "on mine", State: factory.StateQueued}, {Repo: "acme/secret", Title: "on secret", State: factory.StateQueued}} {
		it, err := sc.st.Create(it)
		if err != nil {
			t.Fatal(err)
		}
		made = append(made, it)
	}
	if got := sc.pickable(t, mia); !slices.Equal(got, []string{"acme/api"}) {
		t.Fatalf("mia is offered %q; want the organization's acme/api", got)
	}
	if w := sc.do("GET", "/v1/jobs/"+itoa(made[0].ID), mia, nil, ""); w.Code != 200 {
		t.Fatalf("mia reads the job on acme/mine = %d %s", w.Code, w.Body)
	}
	if w := sc.do("GET", "/v1/jobs/"+itoa(made[1].ID), mia, nil, ""); w.Code != 404 {
		t.Fatalf("mia reads the job on acme/secret = %d %s", w.Code, w.Body)
	}
	if asked["/repos/acme/secret"] == 0 || asked["/repos/acme/mine"] == 0 {
		t.Fatalf("mia's own token was not asked: %v", asked)
	}
	// mia's own token reads acme/api, so a job on it needs no workstream: the
	// organization's path only ever adds.
	if w := sc.do("POST", "/v1/jobs", mia, api.NewJob{Repo: "acme/api", Title: "own", Source: factory.AskApp}, ""); w.Code != 201 {
		t.Fatalf("a job on a repository mia's own token reads = %d %s", w.Code, w.Body)
	}
	if w := sc.do("POST", "/v1/jobs", mia, api.NewJob{Repo: "acme/secret", Title: "x"}, ""); w.Code != 404 {
		t.Fatalf("a job on acme/secret = %d %s", w.Code, w.Body)
	}
}

// THE QUEUE SAYS WHICH REPOSITORIES NEED A WORKSTREAM: abir reads acme/api only
// through the organization, so the queue marks it and the New job sheet asks
// for a workstream before Add to Backlog; the operator's queue never marks it
// (Factory film D22).
func TestTheQueueMarksARepositoryReadOnlyThroughTheOrganization(t *testing.T) {
	sc := newOrgRepoScene(t)
	sc.turnOn(t, "acme/api")
	marked := func(token string) map[string]bool {
		w := sc.do("GET", "/v1/queue", token, nil, "")
		var q struct {
			Repos []struct {
				Name            string `json:"name"`
				NeedsWorkstream bool   `json:"needs_workstream"`
			} `json:"repos"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &q) != nil {
			t.Fatalf("queue = %d %s", w.Code, w.Body)
		}
		out := map[string]bool{}
		for _, r := range q.Repos {
			out[r.Name] = r.NeedsWorkstream
		}
		return out
	}
	if got := marked(sc.abir); !got["acme/api"] {
		t.Fatalf("abir's queue marks %v; want acme/api to need a workstream", got)
	}
	for name, needs := range marked(sc.santosh) {
		if needs {
			t.Fatalf("the operator's queue marks %s as needing a workstream", name)
		}
	}
}
