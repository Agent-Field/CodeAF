package serve_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// newOffFloor is the v2 jobs floor with the watched list's own door, watching
// repos. The repository switch reads the same store the jobs are on.
func newOffFloor(t *testing.T, repos ...string) *v2Jobs {
	t.Helper()
	f := newV2Jobs(t)
	f.h.Close()
	f.seam.SetRepos = f.st.SetRepos
	f.h = serve.New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	if err := f.st.SetRepos(repos); err != nil {
		t.Fatal(err)
	}
	return f
}

// watched is the list the store keeps now.
func (f *v2Jobs) watched(t *testing.T) []string {
	t.Helper()
	repos, err := f.st.Repos()
	if err != nil {
		t.Fatal(err)
	}
	return repos
}

// refusal reads a refusal's sentence and reason.
func refusal(t *testing.T, w *httptest.ResponseRecorder) (words, reason string) {
	t.Helper()
	var body struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Error, body.Reason
}

// jobsByID is each job on the queue by id, as its JSON.
func jobsByID(t *testing.T, q api.Queue) map[int]string {
	t.Helper()
	out := map[int]string{}
	for _, j := range q.Jobs {
		data, err := json.Marshal(j)
		if err != nil {
			t.Fatal(err)
		}
		out[j.ID] = string(data)
	}
	return out
}

func runningItem(num int) factory.Item {
	return factory.Item{Num: num, State: factory.StateRunning, Assignee: "sam",
		Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "write", State: factory.PhaseRunning}}}}
}

func TestARepoTurnedOffLeavesTheQueueAndComesBackAsItWas(t *testing.T) {
	f := newOffFloor(t, "acme/api", "acme/web")
	backlog := f.create(t, factory.Item{Num: 3, State: factory.StateNew, Assignee: "sam"})
	done := f.create(t, factory.Item{Num: 4, State: factory.StateShipped, DoneHow: "merged", Assignee: "sam"})
	web := f.create(t, factory.Item{Repo: "acme/web", Num: 5, State: factory.StateNew, Assignee: "sam"})
	before := jobsByID(t, queueJobs(t, f, ""))
	if len(before) != 3 {
		t.Fatalf("queue before = %v", before)
	}

	// NOTHING IS UNDER WAY, so the switch turns off at once.
	checkJSON(t, verbRequest(f.h, "PUT", "/v1/repos/acme/api", `{"on": false}`), 200)
	if got := f.watched(t); !reflect.DeepEqual(got, []string{"acme/web"}) {
		t.Fatalf("watched after off = %v", got)
	}
	q := queueJobs(t, f, "")
	if len(q.Jobs) != 1 || q.Jobs[0].ID != web.ID {
		t.Fatalf("an off repository's jobs stayed on the queue: %+v", q.Jobs)
	}
	for _, r := range q.Repos {
		if strings.EqualFold(r.Name, "acme/api") || !r.Watched {
			t.Fatalf("queue repositories = %+v", q.Repos)
		}
	}

	// THE JOB'S OWN READ STAYS, and says why it left the queue.
	j := f.read(t, backlog)
	if !j.RepoOff || j.Can.Run || j.Can.StartNow || j.Can.RunAgain || j.Controls.RerunStep {
		t.Fatalf("off job read = repo_off %v can %+v controls %+v", j.RepoOff, j.Can, j.Controls)
	}
	if j := f.read(t, web); j.RepoOff {
		t.Fatal("a job of a repository that is on read as off")
	}

	// TURNING IT BACK ON brings every job back exactly as it was.
	checkJSON(t, verbRequest(f.h, "PUT", "/v1/repos/acme/api", `{"on": true}`), 200)
	after := jobsByID(t, queueJobs(t, f, ""))
	for _, id := range []int{backlog.ID, done.ID, web.ID} {
		if after[id] != before[id] {
			t.Fatalf("job %d came back changed:\nbefore %s\nafter  %s", id, before[id], after[id])
		}
	}
}

func TestARepoWithWorkUnderWayAsksBeforeItStopsIt(t *testing.T) {
	f := newOffFloor(t, "acme/api")
	running := f.create(t, runningItem(12))
	waiting := f.create(t, factory.Item{Num: 7, State: factory.StateNeedsYou, Question: "ok?", Assignee: "sam"})
	quiet := f.create(t, factory.Item{Num: 8, State: factory.StateNew, Assignee: "sam"})

	w := verbRequest(f.h, "PUT", "/v1/repos/acme/api", `{"on": false}`)
	checkJSON(t, w, 409)
	words, reason := refusal(t, w)
	if reason != api.ReasonHasRunning || words != "acme/api has 1 job running (api#12) and 1 waiting on you (api#7)." {
		t.Fatalf("refusal = %q %q", reason, words)
	}
	if w.Header().Get(api.PhaseHeader) != api.PhasePreMutation {
		t.Fatal("a refusal that changed nothing did not say so")
	}
	if got := f.watched(t); !reflect.DeepEqual(got, []string{"acme/api"}) {
		t.Fatalf("refused off changed the list: %v", got)
	}
	if j := f.read(t, running); j.State != api.StateRunning {
		t.Fatalf("refused off touched the running job: %s", j.State)
	}

	// THE PERSON'S YES: each job is stopped as its own Stop would, then off.
	checkJSON(t, verbRequest(f.h, "PUT", "/v1/repos/acme/api", `{"on": false, "stop": true}`), 200)
	if got := f.watched(t); len(got) != 0 {
		t.Fatalf("watched after stop and off = %v", got)
	}
	for _, it := range []factory.Item{running, waiting} {
		got, err := f.st.Get(it.ID)
		if err != nil {
			t.Fatal(err)
		}
		if factory.UnderWay(got) || got.State != factory.StateNew || got.StopUndo == nil || got.StopUndo.By != "sam" {
			t.Fatalf("job %d was not stopped as a person's Stop: %+v", it.ID, got)
		}
	}
	kept, err := f.st.Get(running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Failure == nil || kept.Failure.Code != factory.CodeStopped || kept.Stream == nil || len(kept.Stream.Phases) != 1 {
		t.Fatalf("the stop did not keep the work and mark the cut step: %+v", kept)
	}
	if got, _ := f.st.Get(quiet.ID); got.StopUndo != nil {
		t.Fatal("a job that was not under way was stopped")
	}
	events, err := f.rec.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	stopped := map[int]bool{}
	for _, e := range events {
		if e.Kind == record.Stopped {
			stopped[e.Job] = true
		}
	}
	if !stopped[running.ID] || !stopped[waiting.ID] {
		t.Fatalf("the stops were not recorded: %+v", events)
	}
}

func TestAStopThatFailsLeavesTheRepoOn(t *testing.T) {
	f := newOffFloor(t, "acme/api")
	running := f.create(t, runningItem(12))
	// The stop's receipt cannot be written: the record is a folder.
	path := filepath.Join(f.st.Root(), "events.jsonl")
	_ = os.Remove(path)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	w := verbRequest(f.h, "PUT", "/v1/repos/acme/api", `{"on": false, "stop": true}`)
	checkJSON(t, w, 409)
	words, reason := refusal(t, w)
	if reason != api.ReasonHasRunning || !strings.HasPrefix(words, "api#12 could not be stopped, so acme/api is still on: ") {
		t.Fatalf("failed stop = %q %q", reason, words)
	}
	if got := f.watched(t); !reflect.DeepEqual(got, []string{"acme/api"}) {
		t.Fatalf("a failed stop turned the repository off: %v", got)
	}
	if got, err := f.st.Get(running.ID); err != nil || got.State != factory.StateRunning {
		t.Fatalf("job after a failed stop = %+v %v", got, err)
	}
}

func TestTheWholeListAsksOnceForEveryRepoItTurnsOff(t *testing.T) {
	f := newOffFloor(t, "acme/api", "acme/docs", "acme/web")
	api12 := f.create(t, runningItem(12))
	web3 := f.create(t, factory.Item{Repo: "acme/web", Num: 3, State: factory.StateQueued, Assignee: "sam"})

	w := verbRequest(f.h, "PUT", "/v1/repos", `{"watched": ["acme/docs"]}`)
	checkJSON(t, w, 409)
	words, reason := refusal(t, w)
	if reason != api.ReasonHasRunning || words != "acme/api has 1 job running (api#12). acme/web has 1 job queued (web#3)." {
		t.Fatalf("whole-list refusal = %q %q", reason, words)
	}
	if got := f.watched(t); len(got) != 3 {
		t.Fatalf("a refused list was saved: %v", got)
	}

	checkJSON(t, verbRequest(f.h, "PUT", "/v1/repos", `{"watched": ["acme/docs"], "stop": true}`), 200)
	if got := f.watched(t); !reflect.DeepEqual(got, []string{"acme/docs"}) {
		t.Fatalf("watched after the whole list = %v", got)
	}
	for _, it := range []factory.Item{api12, web3} {
		if got, _ := f.st.Get(it.ID); factory.UnderWay(got) {
			t.Fatalf("job %d is still under way: %+v", it.ID, got)
		}
	}
	if q := queueJobs(t, f, ""); len(q.Jobs) != 0 {
		t.Fatalf("off repositories' jobs on the queue: %+v", q.Jobs)
	}
}

func TestNothingWorksOnAJobOfARepoThatIsOff(t *testing.T) {
	f := newOffFloor(t, "acme/web")
	f.h.Close()
	f.seam.New = func(string, string) (int, error) {
		t.Fatal("a job was made on a repository that is off")
		return 0, nil
	}
	// Every door the floor would offer is there, so each refusal is the
	// switch's and never a door this floor lacks.
	f.seam.Hold = func(int, bool) error { t.Fatal("an off job went on"); return nil }
	f.seam.SendBack = func(int, string) error { t.Fatal("an off job was sent back"); return nil }
	f.seam.Reverify = func(int) error { t.Fatal("an off job's checks ran"); return nil }
	f.h = serve.New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	it := f.create(t, factory.Item{Num: 9, State: factory.StateNew, Assignee: "sam"})
	const want = "acme/api is off · turn it on in Repos to run its jobs."
	for _, verb := range []string{"run", "start_now", "run_again", "steps/write/rerun", "approve", "retry_publish", "merge", "request_changes", "recheck", "resume", "sync"} {
		w := verbRequest(f.h, "POST", "/v1/jobs/"+strconv.Itoa(it.ID)+"/"+verb, `{}`)
		if w.Code != 409 {
			t.Fatalf("%s on an off repository = %d %s", verb, w.Code, w.Body)
		}
		if words, reason := refusal(t, w); words != want || reason != api.ReasonRepoOff {
			t.Fatalf("%s refusal = %q %q", verb, reason, words)
		}
	}
	if got, _ := f.st.Get(it.ID); got.State != factory.StateNew || got.Cap != 0 || got.Marked {
		t.Fatalf("a refused door wrote the job: %+v", got)
	}
	w := verbRequest(f.h, "POST", "/v1/jobs", `{"repo": "acme/api", "title": "new work"}`)
	if words, reason := refusal(t, w); w.Code != 409 || words != want || reason != api.ReasonRepoOff {
		t.Fatalf("new job on an off repository = %d %s", w.Code, w.Body)
	}
	// A door that only tidies the job stays open.
	checkJSON(t, verbRequest(f.h, "POST", "/v1/jobs/"+strconv.Itoa(it.ID)+"/priority", `{"priority": "high"}`), 200)
}
