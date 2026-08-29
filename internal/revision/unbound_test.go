package revision

// The door igel s14's ImportError went through: the run's own checks were red,
// the gate said so, and nothing anywhere said WHICH NAME they were red about.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// unboundIntent is a request that says nothing whatever about which names must
// exist, which is the point: nobody writes that down, so no behaviour of any
// request could ever carry this finding.
const unboundIntent = "Persist the feature schema beside the model after fit."

// unboundBefore is the tree as the job found it: a module that binds `configs`,
// and a server that imports it.
var unboundBefore = map[string]string{
	"igel/configs.py": "import os\nfrom pathlib import Path\n\n\n" +
		"def _get_configs():\n" +
		"    res_path = Path(os.getcwd())\n" +
		"    return {\"results_path\": res_path}\n\n\n" +
		"configs = _get_configs()\n",
	"igel/servers/fastapi_server.py": "from igel.configs import configs\n\n\n" +
		"def upload():\n    return configs\n",
}

// unboundJob writes that tree, remembers the job's baseline, and leaves the
// server file as the run rewrote it — reaching for a name the module has never
// bound at its top level. Nothing was REMOVED, so the presence photograph is
// silent and this is the only reading that can see it.
func unboundJob(t *testing.T) (root string, record []string) {
	t.Helper()
	verify.ForgetBaselines()
	t.Cleanup(verify.ForgetBaselines)
	root = t.TempDir()
	for path, body := range unboundBefore {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	verify.RememberBaseline(root, verify.JobKey(unboundIntent), verify.Reading{
		Surface: verify.PublicSurface(root),
		Unread:  "this project declares no way of checking itself",
	})
	if err := os.WriteFile(filepath.Join(root, "igel/servers/fastapi_server.py"),
		[]byte("from igel.configs import configs, temp_post_req_data_path\n\n\n"+
			"def upload(df):\n    df.to_csv(temp_post_req_data_path)\n    return configs\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	return root, []string{filepath.Join(root, "igel/servers/fastapi_server.py")}
}

func unboundNode() store.Node {
	return store.Node{ID: "task-2", Brief: "persist the feature schema",
		Provenance: store.Provenance{Intent: unboundIntent}}
}

// A NAME NOTHING BINDS IS SETTLED BEFORE A MODEL ROUND IS BOUGHT, and it says
// the name.
//
// igel s14's own record said only `the checks this work wrote fail`, which sends
// a worker to run a suite. The name the suite was failing on was readable off
// the tree for nothing.
func TestANameNothingBindsIsAFindingWithTheNameInIt(t *testing.T) {
	root, record := unboundJob(t)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":true}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, unboundNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true},
		"worker/model")

	if judgment.Pass {
		t.Fatalf("a run that reads a name nothing defines delivered whole: %+v", judgment)
	}
	if judgment.Finding != FindingUnbound {
		t.Fatalf("want the unbound finding, got %q: %s", judgment.Finding, judgment.Gaps)
	}
	if !judgment.Sourced {
		t.Error("a measurement of the world was left to be refused as an ungrounded citation")
	}
	if len(judgment.Unbound) == 0 {
		t.Fatalf("the finding travelled as prose with no list of its own: %+v", judgment)
	}
	for _, wanted := range []string{
		"temp_post_req_data_path", "igel/servers/fastapi_server.py:1", "igel/configs.py"} {
		if !strings.Contains(judgment.Gaps, wanted) {
			t.Errorf("the gap does not say %q: %s", wanted, judgment.Gaps)
		}
	}
	// And it was settled without buying a judge, because the answer was already
	// measured.
	if len(judge.sent) != 0 {
		t.Errorf("a model round was bought for a fact about the tree: %d calls", len(judge.sent))
	}
}

// AND IT CLEARS ITSELF. A round that binds the name must stop the finding, which
// only a fresh reading of the finished tree can say — the same rule the removed
// name is settled under, for the same reason.
func TestBindingTheNameClearsTheFinding(t *testing.T) {
	root, record := unboundJob(t)
	bindTheName(t, root)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":true}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, unboundNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true,
			// A leaf's own stale list still names it. The tree decides.
			Unbound: []string{"temp_post_req_data_path (igel/servers/fastapi_server.py:1)"}},
		"worker/model")

	if judgment.Finding == FindingUnbound {
		t.Fatalf("a name the job bound was still held against it: %s", judgment.Gaps)
	}
}

// THE READING HAPPENS ON EVERY JUDGED NODE, NOT ONLY WHERE A MODEL IS BOUGHT.
// A measurement that is absent exactly when a run is already going wrong is not
// a measurement (FAILSAFE.md clause 4).
func TestEveryJudgedNodeJournalsTheUnboundReading(t *testing.T) {
	root, record := unboundJob(t)
	graph := gateStore(t)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":true}`}}

	// A mechanical gap settles the gate before any model round, and the reading
	// still has to have happened.
	JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), graph, unboundNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true,
			Done: plan.Done{Produces: []string{"model_results/feature_schema.joblib"}}},
		"worker/model")

	readings, err := graph.UnboundFor("task-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(readings) == 0 {
		t.Fatal("a gate that settled before a model round journaled no reading at all")
	}
	held := readings[len(readings)-1]
	if held.Found == 0 || len(held.Names) == 0 {
		t.Fatalf("the reading found the unbound import and journaled nothing: %+v", held)
	}
	if held.Names[0].Name != "temp_post_req_data_path" ||
		!strings.Contains(held.Names[0].Where, "fastapi_server.py") {
		t.Errorf("the journal does not carry the name and its site: %+v", held.Names[0])
	}
}

// EVERY SILENCE FAVOURS THE WORK. A run with a workspace whose every name is
// bound journals the reading that found nothing — which is the row that tells an
// autopsy the door was open — and raises no finding.
func TestATreeThatBindsEveryNameJournalsTheReadingAndRaisesNothing(t *testing.T) {
	root, record := unboundJob(t)
	bindTheName(t, root)
	graph := gateStore(t)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":true}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), graph, unboundNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true},
		"worker/model")

	if judgment.Finding == FindingUnbound {
		t.Fatalf("a tree that binds every name raised the finding anyway: %s", judgment.Gaps)
	}
	readings, err := graph.UnboundFor("task-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(readings) == 0 {
		t.Fatal("a reading that found nothing is still a reading that happened")
	}
	if readings[len(readings)-1].Found != 0 {
		t.Errorf("nothing was unbound and the row says otherwise: %+v", readings[0])
	}
}

// And with no workspace there is nothing to re-read, so the LEAF's own answer
// stands — which is removedSinceTheJobBegan's rule at the same seam.
func TestWithNoWorkspaceTheLeafsOwnAnswerStands(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":true}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, unboundNode(), "done",
		"the module was rewritten",
		Evidence{Observed: true,
			Unbound: []string{"temp_post_req_data_path (igel/servers/fastapi_server.py:1)"}},
		"worker/model")

	if judgment.Finding != FindingUnbound {
		t.Fatalf("the leaf measured it and no gate heard: %q / %s", judgment.Finding, judgment.Gaps)
	}
	if !strings.Contains(judgment.Gaps, "temp_post_req_data_path") {
		t.Errorf("the gap does not name it: %s", judgment.Gaps)
	}
}

// bindTheName is the repair round: the module binds the name at its top level,
// where an importer can reach it.
func bindTheName(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "igel/configs.py"),
		[]byte("import os\nfrom pathlib import Path\n\n"+
			"temp_post_req_data_path = Path(os.getcwd())\n\n\n"+
			"def _get_configs():\n    return {}\n\n\nconfigs = _get_configs()\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
}

// A finding with nothing in it is no claim, never a claim that nothing is
// unbound.
func TestNothingMeasuredIsNoClaim(t *testing.T) {
	if _, any := UnboundNames(nil); any {
		t.Error("an empty reading raised a finding")
	}
	if _, any := UnboundNames([]string{"  "}); any {
		t.Error("a blank name raised a finding")
	}
}
