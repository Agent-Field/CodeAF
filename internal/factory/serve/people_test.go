package serve

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func TestPeopleCardAndQueueUseFakeForgeAndVisibleJobs(t *testing.T) {
	h, clock, g := visibilityFloor(t, map[string]map[string]bool{"priya": {"acme/api": true}, "mina": {}})
	priya := signViewer(t, h, g, clock, "priya")
	mina := signViewer(t, h, g, clock, "mina")
	profileReads, ownerReads := map[string]int{}, map[string]int{}
	forgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/users/") {
			handle := strings.TrimPrefix(r.URL.Path, "/users/")
			profileReads[handle]++
			if handle == "missing" {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"name": "Name " + handle, "avatar_url": "https://avatar/" + handle, "html_url": "https://github.com/" + handle})
			return
		}
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 6 || parts[1] != "repos" || r.URL.Query().Get("ref") != "" {
			t.Errorf("forge path = %s", r.URL)
			http.NotFound(w, r)
			return
		}
		repo := parts[2] + "/" + parts[3]
		ownerReads[repo]++
		text := "* @priya\n/api/ @outside\n"
		if repo == "acme/web" {
			text = "/secret/ @priya @hidden-owner\n"
		}
		json.NewEncoder(w).Encode(map[string]string{"type": "file", "encoding": "base64", "sha": "owners", "content": base64.StdEncoding.EncodeToString([]byte(text))})
	}))
	defer forgeServer.Close()
	loader := factorygithub.NewPeopleLoader(func(context.Context) factorygithub.PeopleAPI {
		return forge.NewClient("stub").WithBaseURL(forgeServer.URL)
	}, func() time.Time { return *clock })
	h.config.PersonProfile, h.config.CodeOwners = loader.Profile, loader.Owners
	snapshot := factory.Snapshot{Repos: []factory.Repo{{Name: "acme/api"}, {Name: "acme/web"}}, Items: []factory.Item{
		{ID: 1, Repo: "acme/api", Author: "outside", Assignee: "priya", State: factory.StateNeedsYou, QKind: factory.QKindApprove, Question: factory.ApproveQuestion("plan"), Stream: plannedStream("api/a.go"), Change: &factory.Change{Files: []factory.ChangedFile{{Path: "README.md"}}}},
		{ID: 2, Repo: "acme/web", Author: "hidden-author", Assignee: "priya", State: factory.StateNeedsYou, Question: "hidden-question", Stream: plannedStream("secret/a.go")},
		{ID: 3, Repo: "acme/api", Assignee: "priya", State: factory.StateNeedsYou, Question: "which version?", PullRequest: &factory.PullRequest{Reviews: []factory.PRReview{{By: "approver", State: "approved"}}}},
		{ID: 4, Repo: "acme/api", Assignee: "priya", State: factory.StateShipped},
	}}
	h.seam.Load = func() (factory.Snapshot, error) { return snapshot, nil }
	q := queueAs(t, h, priya, false)
	if len(q.Jobs) != 3 || !reflect.DeepEqual(q.Jobs[0].Owners, []string{"outside", "priya"}) {
		t.Fatalf("queue = %+v", q)
	}
	for _, handle := range []string{"outside", "priya", "approver"} {
		if q.People[handle].Name != "Name "+handle || profileReads[handle] != 1 {
			t.Fatalf("profile %s = %+v reads %d", handle, q.People[handle], profileReads[handle])
		}
	}
	if _, ok := q.People["hidden-author"]; ok || ownerReads["acme/web"] != 0 || profileReads["hidden-owner"] != 0 {
		t.Fatal("hidden repository facts were fetched or returned")
	}
	queueAs(t, h, priya, false)
	if profileReads["outside"] != 1 || ownerReads["acme/api"] != 1 {
		t.Fatal("queue repeated profile or ownership reads")
	}
	readPerson := func(handle, token string) api.Person {
		t.Helper()
		w := identityRequest(h, "GET", "/v1/people/"+handle, "", token, nil)
		var p api.Person
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil {
			t.Fatalf("person = %d %s", w.Code, w.Body)
		}
		return p
	}
	p := readPerson("@Priya", priya)
	if p.Handle != "priya" || p.Role != "member" || p.LastSeen == nil || p.AssignedNow != 2 || p.WaitingOn != 2 || p.Questions != 1 || len(p.Owns) != 1 || p.Owns[0].Repo != "acme/api" {
		t.Fatalf("member = %+v", p)
	}
	outside := readPerson("outside", priya)
	if outside.Role != "outside" || outside.LastSeen != nil || outside.AssignedNow != 0 || outside.HTMLURL != "https://github.com/outside" || len(outside.Owns) != 1 {
		t.Fatalf("outside = %+v", outside)
	}
	none := readPerson("priya", mina)
	if none.AssignedNow != 0 || none.WaitingOn != 0 || len(none.Owns) != 0 || len(none.Machines) != 0 {
		t.Fatalf("no-visible card = %+v", none)
	}
	if empty := queueAs(t, h, mina, false); len(empty.People) != 0 {
		t.Fatalf("empty queue people = %+v", empty.People)
	}
	w := identityRequest(h, "GET", "/v1/jobs/1", "", priya, nil)
	var job api.Job
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &job) != nil || !reflect.DeepEqual(job.Owners, q.Jobs[0].Owners) {
		t.Fatalf("job = %d %s", w.Code, w.Body)
	}
	if w := identityRequest(h, "GET", "/v1/people/missing", "", priya, nil); w.Code != 404 {
		t.Fatalf("missing = %d %s", w.Code, w.Body)
	}
	if w := identityRequest(h, "POST", "/v1/people/priya", "", priya, nil); w.Code != 405 {
		t.Fatalf("method = %d", w.Code)
	}
	if w := identityRequest(h, "GET", "/v1/people/a%2Fb", "", priya, nil); w.Code != 422 {
		t.Fatalf("invalid handle = %d %s", w.Code, w.Body)
	}
	if snapshot.Items[0].Owners != nil {
		t.Fatal("request mutated source snapshot")
	}
}

func TestPeopleFixtureCard(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	h := New(factory.FixtureSeam(now), Config{Version: "test"})
	defer h.Close()
	r := httptest.NewRequest("GET", "/v1/people/priya", nil)
	r.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("card = %d %s", w.Code, w.Body)
	}
	want, err := os.ReadFile("testdata/person.json")
	if err != nil {
		t.Fatal(err)
	}
	var gotJSON, wantJSON any
	if json.Unmarshal(w.Body.Bytes(), &gotJSON) != nil || json.Unmarshal(want, &wantJSON) != nil || !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("card = %s want %s", w.Body, want)
	}
	if w := identityRequest(h, "GET", "/v1/people/priya", "", "", nil); w.Code != 401 {
		t.Fatalf("unauthorized = %d", w.Code)
	}
}

func TestPersonOwnsWatchedRepositoryWithNoJobs(t *testing.T) {
	h, clock, g := visibilityFloor(t, map[string]map[string]bool{"priya": {"acme/api": true}})
	priya := signViewer(t, h, g, clock, "priya")
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/api", "acme/web"}); err != nil {
		t.Fatal(err)
	}
	h.config.Store = st
	h.seam.Load = func() (factory.Snapshot, error) { return factory.Snapshot{}, nil }
	h.config.CodeOwners = func(_ context.Context, repo string) ([]factory.CodeOwner, error) {
		if repo != "acme/api" {
			t.Fatalf("read hidden ownership %s", repo)
		}
		return factory.ParseCodeOwners("/api/ @priya\n"), nil
	}
	w := identityRequest(h, "GET", "/v1/people/priya", "", priya, nil)
	var p api.Person
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || len(p.Owns) != 1 || p.Owns[0].Repo != "acme/api" || p.AssignedNow != 0 {
		t.Fatalf("no-job ownership = %d %s", w.Code, w.Body)
	}
}

// plannedStream is a saved stream whose finished plan declared one file.
func plannedStream(path string) *factory.Stream {
	return &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone, Result: &factory.StageResult{Output: "files: " + path}}}}
}
