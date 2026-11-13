package serve

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// jobOrg is the org scene with the job doors open that these tests drive.
// The server's own machine is signed in to GitHub as santosh, the way an org
// server set up from someone's laptop is.
func jobOrg(t *testing.T) *wsOrg {
	t.Helper()
	return newWSOrg(t, func(s *factory.Seam) {
		s.Stop = func(int) error { return nil }
		s.Steer = func(int, string) error { return nil }
		s.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "santosh"}, nil }
	})
}

// santoshJob is a job of santosh's, in workstream ws ("" for none).
func (o *wsOrg) santoshJob(t *testing.T, ws string) factory.Item {
	t.Helper()
	it, err := o.st.Create(factory.Item{Title: "job in " + ws, Repo: "acme/api", State: factory.StateQueued, Assignee: "santosh", PolicyGroup: ws,
		Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "code", State: factory.PhaseRunning}}}})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func jobPath(it factory.Item, verb string) string {
	return "/v1/jobs/" + itoa(it.ID) + "/" + verb
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// floor is the titles of the jobs who sees in the queue.
func (o *wsOrg) floor(t *testing.T, who string) []string {
	t.Helper()
	var q api.Queue
	o.do(t, who, "GET", "/v1/queue", -1, nil, 200, &q)
	var titles []string
	for _, j := range q.Jobs {
		titles = append(titles, j.Title)
	}
	return titles
}

// A JOB OUTSIDE EVERY WORKSTREAM IS CHANGED BY ITS PERSON AND THE OPERATOR;
// a teammate who can read the repository can no longer stop or steer it.
func TestAJobOutsideAWorkstreamIsChangedByItsPersonAlone(t *testing.T) {
	o := jobOrg(t)
	o.orgScene(t)
	job := o.santoshJob(t, "")
	o.do(t, "abir", "POST", jobPath(job, "stop"), -1, map[string]any{}, 403, nil)
	o.do(t, "abir", "POST", jobPath(job, "steer"), -1, map[string]any{"words": "use a queue"}, 403, nil)
	o.do(t, "abir", "POST", jobPath(job, "priority"), -1, map[string]any{"priority": "urgent"}, 403, nil)
	o.do(t, "abir", "POST", jobPath(job, "reply"), -1, map[string]any{"words": "hi"}, 403, nil)
	// Following a job is abir's own switch and changes nothing of santosh's.
	o.do(t, "abir", "POST", jobPath(job, "follow"), -1, map[string]any{"on": true}, 200, nil)
	o.do(t, "owner", "POST", jobPath(job, "priority"), -1, map[string]any{"priority": "high"}, 200, nil)
	o.do(t, "santosh", "POST", jobPath(job, "stop"), -1, map[string]any{}, 200, nil)
}

// INSIDE A WORKSTREAM abir can edit, abir may stop santosh's job; santosh, its
// person, may undo that stop.
func TestAnEditorOfTheWorkstreamMayStopItsJobsAndThePersonUndoes(t *testing.T) {
	o := jobOrg(t)
	webhook, launch, _, _ := o.orgScene(t)
	shared := o.santoshJob(t, webhook.ID)
	private := o.santoshJob(t, launch.ID)

	o.do(t, "abir", "POST", jobPath(shared, "stop"), -1, map[string]any{}, 200, nil)
	o.do(t, "santosh", "POST", jobPath(shared, "undo"), -1, map[string]any{}, 200, nil)
	got, err := o.st.Get(shared.ID)
	if err != nil || got.StopUndo != nil || got.State == factory.StateNew {
		t.Fatalf("undo by the job's person left %+v, %v", got.StopUndo, err)
	}
	o.do(t, "abir", "POST", jobPath(shared, "steer"), -1, map[string]any{"words": "use a queue"}, 200, nil)

	// Launch plan is santosh's alone: abir cannot even find its job.
	o.do(t, "abir", "POST", jobPath(private, "stop"), -1, map[string]any{}, 404, nil)
}

// A WORKSTREAM MEMBER WHO CANNOT EDIT IS REFUSED, and a job with no person in
// a workstream needs edit on it.
func TestAJobWithNoPersonNeedsEditOnItsWorkstream(t *testing.T) {
	o := jobOrg(t)
	_, _, pricing, _ := o.orgScene(t)
	it, err := o.st.Create(factory.Item{Title: "pricing copy", Repo: "acme/api", State: factory.StateQueued, PolicyGroup: pricing.ID})
	if err != nil {
		t.Fatal(err)
	}
	// The operator sees Pricing page but holds no role on it; abir cannot see it.
	o.do(t, "abir", "POST", jobPath(it, "priority"), -1, map[string]any{"priority": "urgent"}, 404, nil)
	o.do(t, "mia", "POST", jobPath(it, "priority"), -1, map[string]any{"priority": "urgent"}, 200, nil)
	o.do(t, "owner", "POST", jobPath(it, "priority"), -1, map[string]any{"priority": "low"}, 200, nil)
}

// A JOB IS SEEN ONLY WITH SIGHT OF ITS WORKSTREAM: abir's floor leaves out the
// jobs of santosh's private workstream, the job itself is a 404, its activity
// never reaches abir, and a sign-in that names no one sees no workstream job.
func TestJobsOfAWorkstreamYouCannotSeeAreAbsent(t *testing.T) {
	o := jobOrg(t)
	webhook, launch, _, _ := o.orgScene(t)
	shared := o.santoshJob(t, webhook.ID)
	private := o.santoshJob(t, launch.ID)
	loose := o.santoshJob(t, "")

	if got := o.floor(t, "abir"); !slices.Contains(got, shared.Title) || slices.Contains(got, private.Title) || !slices.Contains(got, loose.Title) {
		t.Fatalf("abir's floor = %q; want %q and %q without %q", got, shared.Title, loose.Title, private.Title)
	}
	if got := o.floor(t, "santosh"); !slices.Contains(got, private.Title) {
		t.Fatalf("santosh's floor = %q; want %q", got, private.Title)
	}
	o.do(t, "abir", "GET", "/v1/jobs/"+itoa(private.ID), -1, nil, 404, nil)

	// santosh's own activity on the private job is not in abir's record.
	o.do(t, "santosh", "POST", jobPath(private, "priority"), -1, map[string]any{"priority": "urgent"}, 200, nil)
	var rec struct {
		Events []struct {
			Job int `json:"job"`
		} `json:"events"`
	}
	o.do(t, "abir", "GET", "/v1/audit", -1, nil, 200, &rec)
	for _, e := range rec.Events {
		if e.Job == private.ID {
			t.Fatalf("abir's record names private job %d", private.ID)
		}
	}

	// A token whose row names no one used to read as the host's own login,
	// santosh here, and so saw santosh's private workstream.
	row, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	if err := tokens.Add(o.cfg.TokensPath, row); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/v1/queue", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer "+row.Token)
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	if w.Code == 200 && (strings.Contains(w.Body.String(), shared.Title) || strings.Contains(w.Body.String(), private.Title)) {
		t.Fatalf("a sign-in with no profile sees workstream jobs: %s", w.Body)
	}
	// Nor does it act as santosh on santosh's job outside every workstream.
	r = httptest.NewRequest("POST", jobPath(loose, "stop"), strings.NewReader("{}"))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer "+row.Token)
	w = httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	if w.Code != 403 && w.Code != 401 {
		t.Fatalf("a sign-in with no profile stopped santosh's job: %d %s", w.Code, w.Body)
	}
}
