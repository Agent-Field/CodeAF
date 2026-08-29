package revision

// The door igel s12 went through: a definition that kept its name, a class that
// no longer answers to how its callers use it, and a judge that had no fact in
// front of it to say so.

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
)

// configsFixture is igel s12's own shape: the module rebinds `configs` from a
// dict to an instance of a class the run wrote, the class has `__getitem__` and
// no `__setitem__`, and the project's own check still assigns into it.
func configsFixture(t *testing.T) (root, patch string, record []string) {
	t.Helper()
	root = t.TempDir()
	files := map[string]string{
		"igel/configs.py": "import os\n\n\n" +
			"class Configs:\n" +
			"    def __init__(self):\n        self._cache = {}\n\n" +
			"    def get(self, key, default=None):\n        return self._cache.get(key, default)\n\n" +
			"    def __getitem__(self, key):\n        return self.get(key)\n\n\n" +
			"configs = Configs()\n",
		"igel/utils.py": "from igel.configs import configs\n\n\n" +
			"def load():\n    return configs.get(\"results_path\")\n",
		"tests/test_igel.py": "from igel.configs import configs\n\n\n" +
			"def test_fit(tmp_path):\n" +
			"    configs[\"results_path\"] = tmp_path\n" +
			"    assert configs.get(\"results_path\") == tmp_path\n",
	}
	for path, body := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	patch = filepath.Join(t.TempDir(), "change.patch")
	diff := "diff --git a/igel/configs.py b/igel/configs.py\n" +
		"--- a/igel/configs.py\n+++ b/igel/configs.py\n" +
		"@@ -1,8 +1,15 @@\n-configs = {}\n+class Configs:\n+    pass\n+configs = Configs()\n"
	if err := os.WriteFile(patch, []byte(diff), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, patch, []string{filepath.Join(root, "igel/configs.py")}
}

// configsNode is a request that states behaviours and — like every real request
// — says nothing whatever about item assignment. Nobody writes that down.
func configsNode() store.Node {
	return store.Node{ID: "task-2", Brief: "persist the feature schema",
		Provenance: store.Provenance{Intent: "After fit, write feature_schema.joblib in the " +
			"results directory. Resolve the results path lazily from the current working directory."}}
}

func configsPoints() []plan.Point {
	return []plan.Point{
		{Behaviour: "After fit, write feature_schema.joblib in the results directory",
			Quote: "After fit, write feature_schema.joblib in the results directory"},
	}
}

// A NAME IS NOT A CONTRACT, and the judge is handed the fact that says so.
//
// The presence photograph read `compared: 8, lost: 0` on this change and was
// right: `configs` is still there. What moved is what stands behind it, and the
// only evidence of that is the declaration the diff rewrote beside the sites
// that still use the old shape.
func TestTheJudgeIsShownWhatStillUsesWhatTheRunReshaped(t *testing.T) {
	root, patch, record := configsFixture(t)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":true}`}}

	JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, configsNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Patch: patch, Observed: true,
			Accept: configsPoints()}, "worker/model")

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

// The refusal that fact makes possible, and the shape that admits it: a file
// that USES what the run changed rather than one the run wrote, and a quote that
// is the consumer's own line rather than a behaviour of the request.
//
// It is Sourced, so the citation invariant does not refuse it — there is no span
// of the request to cite, for the same reason a regression has none — and it is
// its own line, so the stream prints it and the journal keeps it.
func TestAConsumerGroundedRefusalStandsAndSaysWhatMoved(t *testing.T) {
	root, patch, record := configsFixture(t)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":false,` +
		`"file":"tests/test_igel.py",` +
		`"gaps":"Configs implements __getitem__ and no __setitem__, so this line raises TypeError.",` +
		`"quote":"configs[\"results_path\"] = tmp_path","exercised":false}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, configsNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Patch: patch, Observed: true,
			Accept: configsPoints()}, "worker/model")

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
	root, patch, record := configsFixture(t)
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":false,"file":"tests/test_igel.py",` +
		`"gaps":"this does not look finished to me","quote":"the code should be clean",` +
		`"exercised":false}`}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, configsNode(), "done", "",
		Evidence{Artifacts: record, Workspace: root, Patch: patch, Observed: true,
			Accept: configsPoints()}, "worker/model")

	if judgment.Fault == "" || judgment.Checked {
		t.Fatalf("a preference wearing a citation was accepted: %+v", judgment)
	}
}

// EVERY SILENCE FAVOURS THE WORK. A run with no diff to read leaves the judge
// seeing exactly what it saw before any of this existed.
func TestWithNoDiffTheConsumersBlockIsAbsent(t *testing.T) {
	root, _, record := configsFixture(t)
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
