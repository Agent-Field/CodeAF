package revision

// The door igel s12 went through, and the propagation igel s14 went through: a
// definition that kept its name and changed its shape, a name the JOB lost that
// no gate ever heard about, and a measurement that only happened on the path
// where a model was bought.

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

const configsIntent = "After fit, write feature_schema.joblib in the results directory. " +
	"Resolve the results path lazily from the current working directory."

// configsBefore is `igel/configs.py` as the job found it: a module that binds
// three paths and a dict at its top level.
const configsBefore = `import os
from pathlib import Path

from igel.constants import Constants

res_path = Path(os.getcwd()) / Constants.stats_dir
init_file_path = Path(os.getcwd()) / Constants.init_file
temp_post_req_data_path = Path(os.getcwd()) / Constants.post_req_data_file

configs = {
    "stats_dir": Constants.stats_dir,
    "results_path": res_path,
}
`

// configsAfter is the same module as igel s12 and s14 left it, both failures at
// once: `configs` KEPT its name and became an instance of a class with no item
// assignment on it, and the three module paths are GONE — which is what every
// one of s14's twenty-four hidden tests failed on, by import.
const configsAfter = `import os
from pathlib import Path

from igel.constants import Constants


class Configs:
    def __init__(self):
        self._cache = {}

    def get(self, key, default=None):
        return self._cache.get(key, default)

    def __getitem__(self, key):
        return self.get(key)


configs = Configs()
`

// configsJob writes the tree the job found, remembers the baseline against the
// JOB the way every leaf of it does, and then leaves the finished tree in place.
//
// It is the shape of a grown subtree deliberately: nothing here is attached to
// the node that gets judged, exactly as nothing was on igel s14.
func configsJob(t *testing.T) (root string, record []string) {
	t.Helper()
	verify.ForgetBaselines()
	t.Cleanup(verify.ForgetBaselines)
	root = t.TempDir()
	before := map[string]string{
		"igel/configs.py": configsBefore,
		"igel/utils.py": "from igel.configs import configs\n\n\n" +
			"def load():\n    return configs.get(\"results_path\")\n",
		"igel/servers/fastapi_server.py": "from igel.configs import temp_post_req_data_path\n",
		"tests/test_igel.py": "from igel.configs import configs\n\n\n" +
			"def test_fit(tmp_path):\n" +
			"    configs[\"results_path\"] = tmp_path\n" +
			"    assert configs.get(\"results_path\") == tmp_path\n",
	}
	for path, body := range before {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The baseline is the tree before the job's FIRST change, and it carries the
	// surface on every path — including this one, where no check could be read.
	verify.RememberBaseline(root, verify.JobKey(configsIntent), "", verify.Reading{
		Surface: verify.PublicSurface(root),
		Unread:  "this project declares no way of checking itself",
	})
	if err := os.WriteFile(filepath.Join(root, "igel/configs.py"),
		[]byte(configsAfter), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, []string{filepath.Join(root, "igel/configs.py")}
}

// namesRestored is the tree after a repair round put the three module paths
// back: the loss finding is closed and `configs` is still the rebound name, so a
// judge is actually reached.
func namesRestored(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "igel/configs.py"),
		[]byte(strings.Replace(configsAfter, "configs = Configs()",
			"res_path = Path(os.getcwd())\ninit_file_path = Path(os.getcwd())\n"+
				"temp_post_req_data_path = Path(os.getcwd())\nconfigs = Configs()", 1)),
		0o644); err != nil {
		t.Fatal(err)
	}
}

func configsNode() store.Node {
	return store.Node{ID: "task-2", Brief: "persist the feature schema",
		Provenance: store.Provenance{Intent: configsIntent}}
}

func configsPoints() []plan.Point {
	return []plan.Point{
		{Behaviour: "After fit, write feature_schema.joblib in the results directory",
			Quote: "After fit, write feature_schema.joblib in the results directory"},
	}
}

// A NAME THE JOB LOST IS A FINDING AT EVERY GATE OF THAT JOB.
//
// igel s14's three grown leaves each journaled `surface {"lost": 3, "names":
// ["init_file_path", "res_path", "temp_post_req_data_path"]}` and every hidden
// test failed with `ImportError: cannot import name
// 'temp_post_req_data_path'`. Both of that job's gates carried `sourced: None`
// and cited a missing file, because the gate read the JUDGED NODE's own outcome
// and the judged node was not one of those leaves. Here the evidence carries no
// Removed at all — as it did there — and the finding still has to be raised.
func TestANameTheJobLostIsAFindingAtAGateThatDidNotLoseIt(t *testing.T) {
	root, record := configsJob(t)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":true}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, configsNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true, Accept: configsPoints()},
		"worker/model")

	if judgment.Pass {
		t.Fatalf("a job that deleted three public names delivered whole: %+v", judgment)
	}
	if judgment.Finding != FindingRemovedName {
		t.Fatalf("the finding does not name itself as the measurement it is: %q", judgment.Finding)
	}
	if !judgment.Sourced {
		t.Error("a measurement of the world was left to be refused as an ungrounded citation")
	}
	for _, wanted := range []string{"temp_post_req_data_path", "res_path", "init_file_path"} {
		if !strings.Contains(judgment.Gaps, wanted) {
			t.Errorf("the gap does not name %s: %s", wanted, judgment.Gaps)
		}
	}
	// And it was settled without buying a judge, because the answer was already
	// measured.
	if len(judge.sent) != 0 {
		t.Errorf("a model round was bought for a fact about the tree: %d calls", len(judge.sent))
	}
}

// AND IT CLEARS ITSELF. A repair round that puts the names back must stop the
// finding, and only a fresh reading of the finished tree can say so — which is
// the whole reason the gate re-takes the reading rather than carrying a leaf's
// list forward.
func TestAJobThatPutTheNamesBackIsNoLongerHeldToTheLoss(t *testing.T) {
	root, record := configsJob(t)
	if err := os.WriteFile(filepath.Join(root, "igel/configs.py"),
		[]byte(configsBefore), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":true}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, configsNode(), "done", "",
		// The leaf's own stale list still says three names went. The tree says
		// otherwise, and the tree is what decides.
		Evidence{Artifacts: record, Workspace: root, Observed: true, Accept: configsPoints(),
			Removed: []string{"res_path", "init_file_path", "temp_post_req_data_path"}},
		"worker/model")

	if judgment.Finding == FindingRemovedName {
		t.Fatalf("a name the job restored was still being held against it: %s", judgment.Gaps)
	}
}

// A NAME IS NOT A CONTRACT, and the judge is handed the fact that says so —
// read off the job's own two readings and never off a diff, because on the belts
// a headless run uses there is no diff to read.
func TestTheJudgeIsShownWhatStillUsesWhatTheRunReshaped(t *testing.T) {
	root, record := configsJob(t)
	// The three deleted names are put back, so the loss finding does not settle
	// the gate before a judge is ever shown anything.
	namesRestored(t, root)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":true}`}}

	JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, configsNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true, Accept: configsPoints()},
		"worker/model")

	prompt := judge.lastPrompt()
	for _, wanted := range []string{
		"What this run did to definitions the rest of the project uses",
		"`configs` (igel/configs.py)",
		"subscript-assign",
		"tests/test_igel.py:5",
	} {
		if !strings.Contains(prompt, wanted) {
			t.Errorf("the judge was not shown %q:\n%s", wanted, prompt)
		}
	}
}

// THE READING HAPPENS ON EVERY JUDGED NODE, NOT ONLY WHERE A MODEL IS BOUGHT.
//
// Both of igel s14's gates were mechanical — a file the plan promised was not on
// disk — so the gate returned above the block that took this reading, and the
// store holds no consumers event of any kind for that run. A measurement that is
// absent exactly when a run is already going wrong is not a measurement.
func TestEveryJudgedNodeJournalsTheChangedDefinitionReading(t *testing.T) {
	root, record := configsJob(t)
	graph := gateStore(t)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":true}`}}

	// A mechanical gap: the plan promised a file the disk does not hold, which
	// settles the gate before any model round.
	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), graph, configsNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true,
			Done: plan.Done{Produces: []string{"model_results/feature_schema.joblib"}}},
		"worker/model")

	if judgment.Pass {
		t.Fatalf("a promised file that is not on disk passed: %+v", judgment)
	}
	readings, err := graph.ConsumersFor("task-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(readings) == 0 {
		t.Fatal("a gate that settled before a model round journaled no reading at all")
	}
	held := readings[len(readings)-1]
	if held.Weighed == 0 {
		t.Fatalf("the reading found the rebound definition and journaled nothing: %+v", held)
	}
	var shapes []string
	for _, definition := range held.Changed {
		for _, shape := range definition.Shapes {
			shapes = append(shapes, definition.Name+" "+shape.Shape)
		}
	}
	if !strings.Contains(strings.Join(shapes, ", "), "configs subscript-assign") {
		t.Errorf("the journal does not carry the shape its consumers use: %v", shapes)
	}
}

// The refusal that fact makes possible, and the shape that admits it: a file
// that USES what the run changed rather than one the run wrote, and a quote that
// is the consumer's own line rather than a behaviour of the request.
func TestAConsumerGroundedRefusalStandsAndSaysWhatMoved(t *testing.T) {
	root, record := configsJob(t)
	namesRestored(t, root)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":false,` +
		`"file":"tests/test_igel.py",` +
		`"gaps":"Configs implements __getitem__ and no __setitem__, so this line raises TypeError.",` +
		`"quote":"configs[\"results_path\"] = tmp_path","exercised":false}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, configsNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true, Accept: configsPoints()},
		"worker/model")

	if len(judge.sent) != 1 {
		t.Fatalf("a readable verdict was asked again: %d calls", len(judge.sent))
	}
	if !judgment.Checked || judgment.Pass {
		t.Fatalf("the refusal did not stand: %+v", judgment)
	}
	if len(judgment.Consumers) == 0 {
		t.Fatalf("a consumer-grounded refusal carried no finding of its own: %+v", judgment)
	}
	finding := judgment.Consumers[0]
	for _, wanted := range []string{"configs changed", "still use it as subscript-assign",
		"tests/test_igel.py:5"} {
		if !strings.Contains(finding, wanted) {
			t.Errorf("the finding does not say %q: %s", wanted, finding)
		}
	}
	if !strings.Contains(judgment.Gaps, finding) {
		t.Errorf("the line a person reads does not carry the finding: %s", judgment.Gaps)
	}
	if !judgment.Sourced {
		t.Error("a measurement of the world was left to be refused as an ungrounded citation")
	}
	if !strings.HasPrefix(judgment.HeldPoint, "consumer: ") {
		t.Errorf("the record says this verdict was held to a checklist: %q", judgment.HeldPoint)
	}
}

// And the fence's own rule is unmoved: a refusal that quotes NEITHER a stated
// behaviour NOR a line the judge was shown is still not a verdict this gate can
// read. Widening the ground is not opening the door.
func TestARefusalQuotingNeitherGroundIsStillNotAVerdict(t *testing.T) {
	root, record := configsJob(t)
	namesRestored(t, root)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":false,"file":"tests/test_igel.py",` +
		`"gaps":"this does not look finished to me","quote":"the code should be clean",` +
		`"exercised":false}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, configsNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true, Accept: configsPoints()},
		"worker/model")

	if judgment.Fault == "" || judgment.Checked {
		t.Fatalf("a preference wearing a citation was accepted: %+v", judgment)
	}
}

// EVERY SILENCE FAVOURS THE WORK. A job that took no baseline leaves the judge
// seeing exactly what it saw before any of this existed.
func TestWithNoBaselineTheConsumersBlockIsAbsent(t *testing.T) {
	root, record := configsJob(t)
	verify.ForgetBaselines()
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":true}`}}

	JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, configsNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true, Accept: configsPoints()},
		"worker/model")

	if strings.Contains(judge.lastPrompt(), "definitions the rest of the project uses") {
		t.Error("a reading nobody could take was printed anyway")
	}
}

// ── igel s15: the contract that ate a correct verdict ────────────────────────

// s15Node is a request that names a file by name, the way igel's does, with no
// structured produces list behind it — so the mechanical gate above cannot
// settle it and the model judge is the one that has to name the absence.
func s15Node() store.Node {
	return store.Node{ID: "task-2", Brief: "persist the feature schema",
		Provenance: store.Provenance{Intent: "After fit, write feature_schema.joblib " +
			"in the results directory."}}
}

func s15Points() []plan.Point {
	return []plan.Point{
		{Behaviour: "After fit, write feature_schema.joblib in the results directory",
			Quote: "After fit, write feature_schema.joblib in the results directory"},
	}
}

// A FILE THAT WAS PROMISED AND IS NOT THERE IS THE ONE THING A JUDGE MUST ALWAYS
// BE ABLE TO NAME.
//
// It is in no record, because the record is what the run left behind and the
// whole complaint is that this is not in it. igel s15 answered
// `"file": "feature_schema.joblib"` about a file the request names and the disk
// does not hold, was refused by the enum, re-asked, refused again, and the run
// ended after eight minutes with `the review could not be read, so this delivery
// was never checked` and nothing further started.
func TestARefusalNamingAPromisedAbsentFileIsAVerdict(t *testing.T) {
	root, record := configsJob(t)
	namesRestored(t, root)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":false,` +
		`"file":"feature_schema.joblib",` +
		`"gaps":"fit never writes the schema; nothing of that name is on disk.",` +
		`"quote":"After fit, write feature_schema.joblib in the results directory",` +
		`"exercised":false}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, s15Node(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true, Accept: s15Points(),
			Named: NamedFiles(s15Node().Provenance.Intent)}, "worker/model")

	if len(judge.sent) != 1 {
		t.Fatalf("a correct verdict was re-asked: %d calls", len(judge.sent))
	}
	if judgment.Fault != "" || !judgment.Checked {
		t.Fatalf("the gate faulted over a finding it was right about: %+v", judgment)
	}
	// And it reads as what it is: the mechanical gap, found by a judge, so it
	// buys the round an absent deliverable has always bought.
	if !judgment.Mechanical || judgment.Finding != FindingMissingProduces {
		t.Errorf("an absent promised file did not read as the mechanical gap: %+v", judgment)
	}
	if !strings.Contains(judgment.Gaps, "feature_schema.joblib") {
		t.Errorf("the gap does not name the file: %s", judgment.Gaps)
	}
}

// AN UNREADABLE REVIEW MAY NOT END A RUN WITH NOTHING STARTED. What replaces it
// is the fact the filesystem can settle, not a looser opinion: the gate keeps
// refusing verdicts the tree contract refuses, and the run still gets — and
// still buys — the finding it plainly has.
func TestAnUnreadableTreeVerdictStillRaisesTheAbsentDeliverable(t *testing.T) {
	root, record := configsJob(t)
	namesRestored(t, root)
	settings := config.Config{Model: "worker/model"}
	// An answer no tree contract can hold, twice: the seam asks once more and
	// gets the same thing.
	judge := &recordingJudge{replies: []string{
		`{"pass":false,"file":"src/nowhere.py","gaps":"it is wrong","quote":"be better"}`,
	}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, s15Node(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true, Accept: s15Points(),
			Named: NamedFiles(s15Node().Provenance.Intent)}, "worker/model")

	if judgment.Fault != "" {
		t.Fatalf("the run ended with nothing started over an absent deliverable: %+v", judgment)
	}
	if !judgment.Mechanical || judgment.Finding != FindingMissingProduces {
		t.Fatalf("the fact the filesystem could settle was never raised: %+v", judgment)
	}
	if judgment.Subject != SubjectFallbackWords {
		t.Errorf("the record does not say how this was settled: %q", judgment.Subject)
	}
	// And the opinion the contract refused was NOT adopted on the way.
	if strings.Contains(judgment.Gaps, "it is wrong") {
		t.Errorf("a verdict the tree contract refused was taken anyway: %s", judgment.Gaps)
	}
}

// AND WHEN EVEN THAT IS UNREADABLE, THE FILESYSTEM STILL SETTLES IT. A promised
// file that is not on disk is a fact no opinion is needed for, and a run that
// stops with "nothing further was started" over one has broken the floor.
func TestProseWhereAVerdictShouldBeStillRaisesTheAbsentDeliverable(t *testing.T) {
	root, record := configsJob(t)
	namesRestored(t, root)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`I think it looks fine to me.`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, s15Node(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true, Accept: s15Points(),
			Named: NamedFiles(s15Node().Provenance.Intent)}, "worker/model")

	if judgment.Fault != "" {
		t.Fatalf("an absent deliverable was reported as an unchecked delivery: %+v", judgment)
	}
	if !judgment.Mechanical || !strings.Contains(judgment.Gaps, "feature_schema.joblib") {
		t.Fatalf("the finding the filesystem could settle was never raised: %+v", judgment)
	}
	if judgment.Subject != SubjectFallbackWords {
		t.Errorf("the record does not say how this was settled: %q", judgment.Subject)
	}
}
