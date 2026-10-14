package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// row is one app on one road: the bench's record of what the owner got.
type row struct {
	ID         string   `json:"id"`
	Family     string   `json:"family,omitempty"`
	Road       string   `json:"road"`
	RoadUsed   string   `json:"road_used"`
	State      string   `json:"state"`
	CheckPass  bool     `json:"check_pass"`
	OpensInUse bool     `json:"opens_in_use"`
	RunsDryOK  bool     `json:"runs_dry_ok"`
	RawErrors  int      `json:"raw_errors"`
	RawSamples []string `json:"raw_samples,omitempty"`
	FirstLineS float64  `json:"time_to_first_line_s"`
	TotalS     float64  `json:"total_s"`
	CostUSD    float64  `json:"cost_usd"`
	Steps      int      `json:"steps"`
	Stages     stageSet `json:"stages"`
	B7         bool     `json:"b7_fail,omitempty"`
	Useful     string   `json:"usefulness"`
	UsefulWhy  string   `json:"usefulness_reason,omitempty"`
	Cause      string   `json:"cause,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// verdict is what the final Check report and the stored files say.
type verdict struct {
	pass, opens, runsDry bool
}

// judge reads the last Check report the desk would have shown against the
// screens and runs the stored app holds: every one must be in the report.
func judge(check *bridge.CheckReport, files map[string]string, state string) verdict {
	if check == nil || state != string(bridge.StatePreview) {
		return verdict{}
	}
	f := spec.Files{}
	for name, text := range files {
		f[name] = []byte(text)
	}
	bundle, err := spec.Decode(f)
	if err != nil {
		return verdict{}
	}
	return verdict{
		pass:    check.Pass,
		opens:   check.Pass && covers(check.Rendered, keys(bundle.Screens)),
		runsDry: check.Pass && covers(check.Runs, keys(bundle.Runs)),
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// covers says every wanted id is in got.
func covers(got, want []string) bool {
	have := map[string]bool{}
	for _, g := range got {
		have[g] = true
	}
	for _, w := range want {
		if !have[w] {
			return false
		}
	}
	return true
}

// roadUsed reads the rung that built the app from build-result.json.
func roadUsed(root, app string) string {
	raw, err := os.ReadFile(filepath.Join(root, app, "build-result.json"))
	if err != nil {
		return ""
	}
	var r struct {
		Road string `json:"road"`
	}
	_ = json.Unmarshal(raw, &r)
	return r.Road
}

// journalSteps counts the verbs the loop worker committed (journal lines).
func journalSteps(root, app string) int {
	f, err := os.Open(filepath.Join(root, ".builds", app, "draft.jsonl"))
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	for sc := bufio.NewScanner(f); sc.Scan(); {
		n++
	}
	return n
}

func seconds(from, to time.Time) float64 {
	if from.IsZero() || to.IsZero() {
		return 0
	}
	return to.Sub(from).Seconds()
}

// causeOf tags a known cause on a failure text. H1 is the Builder's
// `prompt: "x" is not an app file` bug on the calls road.
func causeOf(text string) string {
	if h1.MatchString(text) {
		return "H1"
	}
	return ""
}

// dumpTrace keeps every event of one app under out/events/ for the autopsy.
func dumpTrace(out, road, app string, t trace) {
	dir := filepath.Join(out, "events")
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	var lines []byte
	add := func(kind string, v any) {
		raw, _ := json.Marshal(map[string]any{"kind": kind, "event": v})
		lines = append(append(lines, raw...), '\n')
	}
	for _, b := range t.builds {
		add("build", b)
	}
	for _, d := range t.drafts {
		add("draft", d)
	}
	_ = os.WriteFile(filepath.Join(dir, road+"-"+app+".jsonl"), lines, 0o644)
}
