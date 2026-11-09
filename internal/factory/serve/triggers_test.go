package serve_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// repoless is a Jira job in Backlog that no rule or person has given a
// repository yet.
func repoless(t *testing.T, f *v2Jobs) factory.Item {
	t.Helper()
	it := f.create(t, factory.Item{State: factory.StateNew, Kind: factory.KindIssue, Origin: "jira", SourceRef: "jira:FL-1"})
	if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.Repo, cur.Places = "", nil; return nil }); err != nil {
		t.Fatal(err)
	}
	got, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// START ON A JOB WITH NO REPOSITORY IS A REFUSAL IN THE FACTORY'S WORDS: a
// definite conflict saying what is missing. It used to fail the launch, which
// the app reads as "may have happened", and the desk dropped the reason.
func TestStartOnAJobWithNoRepositoryIsARefusalInItsWords(t *testing.T) {
	f := newV2Jobs(t)
	it := repoless(t, f)
	for _, verb := range []string{"start_now", "run"} {
		w := f.post(t, it, verb, `{}`, 409)
		var body struct {
			Error  string `json:"error"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Error != factory.NoRepoRefusal || body.Reason != "conflict" {
			t.Fatalf("%s answered %s", verb, w.Body)
		}
	}
	if saved, _ := f.st.Get(it.ID); saved.Marked || saved.State != factory.StateNew || saved.Starter != "" {
		t.Fatalf("a refused start wrote the job: %+v", saved)
	}
}

// A JOB WAITING FOR A REPOSITORY SAYS SO ON ITS READ, so the desk can show
// why it is not starting instead of "ready to run".
func TestABacklogJobWithNoRepositorySaysItWaitsForOne(t *testing.T) {
	f := newV2Jobs(t)
	j := f.read(t, repoless(t, f))
	if p := j.StatePresentation; p.Title != "Waiting for a repository" || p.Detail != "Say which repository the work is on, or add a routing rule that sends it to one." {
		t.Fatalf("presentation %+v", p)
	}
	if p := f.read(t, f.create(t, factory.Item{State: factory.StateNew})).StatePresentation; p.Title != "Backlog" {
		t.Fatalf("a job with a repository reads %+v", p)
	}
}

// A QUEUED JOB THAT NEVER RAN MAY STILL TAKE A PIPELINE: it was refused as
// "This job has already run", which was false. Its steps are swapped and its
// run laid out again on them.
func TestAQueuedJobThatNeverRanTakesAPickedPipeline(t *testing.T) {
	f := startWithDocs(t)
	pending := []factory.Phase{{Name: "plan", State: factory.PhasePending}, {Name: "write", State: factory.PhasePending}, {Name: "proof", State: factory.PhasePending}}
	it := f.create(t, factory.Item{State: factory.StateQueued, Kind: factory.KindIssue, Cap: 5, Stages: []factory.Stage{{Name: "plan"}, {Name: "write"}, {Name: "proof"}},
		Stream: &factory.Stream{Phases: pending}})
	w := f.post(t, it, "start_now", `{"pipeline":"docs"}`, 200)
	if strings.Contains(w.Body.String(), "already run") {
		t.Fatalf("answered %s", w.Body)
	}
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := stageNames(saved.Stages); got != "write,proof" {
		t.Fatalf("stages %s", got)
	}
	var laid []string
	for _, p := range saved.Stream.Phases {
		laid = append(laid, p.Name)
	}
	if strings.Join(laid, ",") != "write,proof" {
		t.Fatalf("the queued run still lays out %v", laid)
	}
}

// GIVING OUTSIDE WORK A REPOSITORY: the job takes the repository, its
// places and that repository's steps, remembers who chose it, and the
// factory routes it again at once. A job that started keeps its repository,
// and a repository must be owner/name.
func TestSetRepoGivesAJobThatNeverRanItsRepository(t *testing.T) {
	f := startWithDocs(t)
	routed := 0
	f.cfg.Route = func(context.Context) error { routed++; return nil }
	f.cfg.Pipeline = nil
	f.seam.RecipeAt = func(string) (factory.Recipe, []factory.Problem, string, error) {
		file, err := pipeline.Parse(docsPipelineFile)
		return file.Recipe(), nil, "", err
	}
	f.h.Close()
	f.h = serve.New(f.seam, f.cfg)
	it := repoless(t, f)
	if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.Route = "old rule"; return nil }); err != nil {
		t.Fatal(err)
	}
	f.post(t, it, serve.SetRepoVerb, `{"repo":"no-owner"}`, 422)
	f.post(t, it, serve.SetRepoVerb, `{"repo":"acme/api"}`, 200)
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Repo != "acme/api" || len(saved.Places) != 1 || saved.Places[0] != "acme/api" || saved.RepoSetBy == "" || saved.Route != "" ||
		stageNames(saved.Stages) != "plan,write,proof" {
		t.Fatalf("job %+v stages %s", saved, stageNames(saved.Stages))
	}
	if routed != 1 {
		t.Fatalf("routed %d times, want once", routed)
	}
	if p := f.read(t, saved).StatePresentation; p.Title != "Backlog" {
		t.Fatalf("a job with a repository still reads %+v", p)
	}
	ran := repoless(t, f)
	if err := f.st.Update(ran.ID, func(cur *factory.Item) error {
		cur.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "plan", Round: 1, State: factory.PhaseDone}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w := f.post(t, ran, serve.SetRepoVerb, `{"repo":"acme/api"}`, 409)
	if !strings.Contains(w.Body.String(), serve.SetRepoStarted) {
		t.Fatalf("answered %s", w.Body)
	}
	issue := f.create(t, factory.Item{State: factory.StateNew, Kind: factory.KindIssue, Origin: factory.OriginForge, Num: 9})
	f.post(t, issue, serve.SetRepoVerb, `{"repo":"acme/web"}`, 409)
}
