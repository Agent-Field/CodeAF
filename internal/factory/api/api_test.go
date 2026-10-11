package api

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// TestEverySeamDoorIsWiredOrTUIOnly is the structural law: a door added to
// factory.Seam must be given a verb or named TUI-only, never silently left
// off the wire.
func TestEverySeamDoorIsWiredOrTUIOnly(t *testing.T) {
	wired := map[string]bool{}
	for _, v := range Verbs {
		for _, f := range v.Seam {
			wired[f] = true
		}
	}
	doors := map[string]bool{}
	st := reflect.TypeOf(factory.Seam{})
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		if !f.IsExported() || f.Type.Kind() != reflect.Func {
			continue
		}
		doors[f.Name] = true
		_, tuiOnly := TUIOnly[f.Name]
		if !wired[f.Name] && !tuiOnly {
			t.Errorf("factory.Seam.%s is neither in api.Verbs nor in api.TUIOnly", f.Name)
		}
		if wired[f.Name] && tuiOnly {
			t.Errorf("factory.Seam.%s is both wired and TUI-only", f.Name)
		}
	}
	for name := range wired {
		if !doors[name] {
			t.Errorf("api.Verbs names %s, which is not a factory.Seam door", name)
		}
	}
	for name := range TUIOnly {
		if !doors[name] {
			t.Errorf("api.TUIOnly names %s, which is not a factory.Seam door", name)
		}
	}
}

func TestVerbNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, v := range Verbs {
		if seen[v.Name] {
			t.Errorf("verb %q twice", v.Name)
		}
		seen[v.Name] = true
	}
}

var at = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

// A comment receipt keeps pr explicitly null and survives both wire doors.
func TestPublishedCommentRoundTripsWithNullPR(t *testing.T) {
	url := "https://github.com/acme/api/issues/12#issuecomment-7"
	it := factory.Item{ID: 12, State: factory.StateShipped, Publish: "comment", CommentURL: url}
	j := FromItem(it)
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	pr, present := fields["pr"]
	if !present || pr != nil || fields["comment"] != url || fields["publish"] != "comment" || fields["state"] != StateDone {
		t.Fatalf("published wire = %s", raw)
	}
	back := ToItem(j)
	if back.PullRequest != nil || back.CommentURL != url || back.Publish != "comment" || back.State != factory.StateShipped {
		t.Fatalf("comment receipt lost in job codec: %+v", back)
	}
	report := factory.RunReport{Publish: "comment", CommentURL: url}
	if got := ToWorkerReport(FromRunReport(report)); !reflect.DeepEqual(got, report) {
		t.Fatalf("comment receipt lost in worker codec: %+v", got)
	}
}

// wire is the trip a client seam's Load makes: through JSON and back.
func wire(t *testing.T, s factory.Snapshot) factory.Snapshot {
	t.Helper()
	b, err := json.Marshal(FromSnapshot(s, "santosh"))
	if err != nil {
		t.Fatal(err)
	}
	var q Queue
	if err := json.Unmarshal(b, &q); err != nil {
		t.Fatal(err)
	}
	return ToSnapshot(q, nil)
}

// TestFixtureRoundTrips: the fixture floor the TUI page is drawn from comes
// back whole.
func TestFixtureRoundTrips(t *testing.T) {
	want := factory.Fixture(at)
	got := wire(t, want)
	for _, d := range diff(reflect.ValueOf(want), reflect.ValueOf(got), "Snapshot") {
		t.Error(d)
	}
}

// TestFixtureSeamRoundTrips: FixtureSeam's Load through the codec, field by
// field for every Item field the TUI page reads (internal/tui3/factory*.go).
func TestFixtureSeamRoundTrips(t *testing.T) {
	want, err := factory.FixtureSeam(at).Load()
	if err != nil {
		t.Fatal(err)
	}
	got := wire(t, want)
	if len(got.Items) != len(want.Items) {
		t.Fatalf("items: %d, want %d", len(got.Items), len(want.Items))
	}
	for i := range want.Items {
		w, g := reflect.ValueOf(want.Items[i]), reflect.ValueOf(got.Items[i])
		for _, f := range pageReads {
			if _, lost := Lost["Item."+f]; lost {
				t.Errorf("the page reads Item.%s but the wire names it lost", f)
			}
			for _, d := range diff(w.FieldByName(f), g.FieldByName(f), fmt.Sprintf("Item[%d].%s", i, f)) {
				t.Error(d)
			}
		}
	}
}

// pageReads are the Item fields internal/tui3/factory*.go reads, directly
// or through internal/factory's helpers (Notes, Heard and Asking through the
// item page's conversation and inbox).
var pageReads = []string{
	"ID", "Repo", "Product", "Places", "Num", "Kind", "Title", "Body", "Author", "Tier", "Origin",
	"Synced", "Created", "Changed", "State", "Triage", "Stages", "Cap", "Stream", "Question", "QKind",
	"Proof", "Policy", "Marked", "Labels", "Checks", "Diff", "Asking", "Adapted", "Notes", "Talk",
	"Heard", "URL", "Comments", "Files", "CheckRuns", "Base", "Head", "HeadSHA", "Activity",
}

// TestEveryFieldRoundTrips fills every field of a snapshot, items and all,
// and asks for each back. A field added to the floor fails here until the
// wire carries it or Lost names it.
func TestEveryFieldRoundTrips(t *testing.T) {
	var want factory.Snapshot
	fill(reflect.ValueOf(&want).Elem(), "Snapshot", 0)
	got := wire(t, want)
	for _, d := range diff(reflect.ValueOf(want), reflect.ValueOf(got), "Snapshot") {
		t.Error(d)
	}
}

// TestStepsLineUpPipelineAndRun: a run that holds a phase the pipeline has
// since dropped, out of the pipeline's order, keeps both lists whole.
func TestStepsLineUpPipelineAndRun(t *testing.T) {
	it := factory.Item{ID: 1, State: factory.StateRunning,
		Stages: []factory.Stage{{Name: "a", On: true}, {Name: "b", On: true}, {Name: "c"}},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "b", State: factory.PhaseDone}, {Name: "x", State: factory.PhaseRunning}, {Name: "a", State: factory.PhaseDone}}}}
	j := FromItem(it)
	var names, states []string
	for _, s := range j.Steps {
		names, states = append(names, s.Name), append(states, s.State)
	}
	if fmt.Sprint(names) != "[a b x a c]" || fmt.Sprint(states) != "[pending done running done skipped]" {
		t.Errorf("steps %v %v", names, states)
	}
	if j.Step == nil || j.Step.Name != "x" || j.Step.Count != 3 {
		t.Errorf("step %+v", j.Step)
	}
	for _, d := range diff(reflect.ValueOf(it), reflect.ValueOf(ToItem(j)), "Item") {
		t.Error(d)
	}
}

// TestStepFinishAndPlannedFiles: a run job's finish time and the plan's
// file list are on the job the dashboard reads, including GET /v1/jobs/{id}
// which embeds this Job.
func TestStepFinishAndPlannedFiles(t *testing.T) {
	at := time.Date(2026, 10, 11, 15, 4, 0, 0, time.UTC)
	it := factory.Item{
		ID: 7, State: factory.StateRunning, Title: "fix",
		PlannedFiles: []string{"internal/a.go", "internal/b.go"},
		Stream: &factory.Stream{Cur: 0, Started: at, Phases: []factory.Phase{
			{Name: "plan", State: factory.PhaseDone, At: at, Result: &factory.StageResult{Output: "a plan", Spent: 0.2}},
			{Name: "write", State: factory.PhaseRunning},
		}},
	}
	j := FromItem(it)
	if j.Steps[0].At == nil || !j.Steps[0].At.Equal(at) {
		t.Fatalf("step.at %+v", j.Steps[0].At)
	}
	if j.Steps[1].At != nil {
		t.Fatalf("a step still running carries %v", j.Steps[1].At)
	}
	if !reflect.DeepEqual(j.PlannedFiles, it.PlannedFiles) {
		t.Fatalf("planned %v", j.PlannedFiles)
	}
	raw, err := json.Marshal(struct {
		Factory Factory `json:"factory"`
		Job
	}{Factory{Address: "localhost", Version: "test"}, j})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	files, _ := wire["planned_files"].([]any)
	if len(files) != 2 || files[0] != "internal/a.go" {
		t.Fatalf("planned_files %#v", wire["planned_files"])
	}
	steps, _ := wire["steps"].([]any)
	plan, _ := steps[0].(map[string]any)
	if plan["at"] != at.Format(time.RFC3339Nano) {
		t.Fatalf("step.at %v", plan["at"])
	}
	if _, ok := steps[1].(map[string]any)["at"]; ok {
		t.Fatal("running step has at")
	}
	back := ToItem(j)
	if !back.Stream.Phases[0].At.Equal(at) || !reflect.DeepEqual(back.PlannedFiles, it.PlannedFiles) {
		t.Fatalf("round trip planned %v at %v", back.PlannedFiles, back.Stream.Phases[0].At)
	}
	if !back.Stream.Phases[1].At.IsZero() {
		t.Fatal("running step's finish came back")
	}
}

func TestStatesAndGates(t *testing.T) {
	cases := []struct {
		state     factory.State
		q, qkind  string
		wantState string
		wantGate  string
		wantReady bool
	}{
		{factory.StateNew, "", "", StateBacklog, "", false},
		{factory.StateQueued, "", "", StateQueued, "", false},
		{factory.StateRunning, "", "", StateRunning, "", false},
		{factory.StateNeedsYou, "plan is ready", factory.QKindApprove, StateNeedsYou, GatePlan, false},
		{factory.StateNeedsYou, "raise it?", "cap", StateNeedsYou, GateCap, false},
		{factory.StateNeedsYou, "which file?", "scope", StateNeedsYou, GateQuestion, false},
		{factory.StateLanded, "", "", StateNeedsYou, GateProof, true},
		{factory.StateShipped, "", "", StateDone, "", false},
		{factory.StateDismissed, "", "", StateDropped, "", false},
		{"someday", "", "", "someday", "", false},
	}
	for _, c := range cases {
		it := factory.Item{State: c.state, Question: c.q, QKind: c.qkind}
		j := FromItem(it)
		if j.State != c.wantState || j.Gate != c.wantGate || j.Ready != c.wantReady {
			t.Errorf("%s: state %q gate %q ready %v", c.state, j.State, j.Gate, j.Ready)
		}
		if back := ToItem(j).State; back != c.state {
			t.Errorf("%s came back %s", c.state, back)
		}
	}
}

func TestQuestionCodecKeepsDistinctAsksAndRoundTripsCombinedAsks(t *testing.T) {
	for _, tc := range []struct {
		name, text, with string
		count            int
	}{
		{"reported ask", "continue?", factory.AskedYou, 1},
		{"different question", "which file?", factory.AskedYou, 2},
		{"manager holds same words", "continue?", factory.AskedManager, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := factory.Item{State: factory.StateNeedsYou, Question: "continue?", QKind: factory.QKindApprove,
				Asking: &factory.Asked{Stage: "approve", Question: tc.text, With: tc.with, At: at,
					Options: []string{"yes", "no"}, Pick: "yes", Why: "your choice"}}
			got := wire(t, factory.Snapshot{Items: []factory.Item{it}})
			if qs := FromItem(it).Questions; len(qs) != tc.count {
				t.Fatalf("questions = %+v; want %d asks", qs, tc.count)
			}
			for _, d := range diff(reflect.ValueOf(it), reflect.ValueOf(got.Items[0]), "Item") {
				t.Error(d)
			}
		})
	}
}

// TestNilIsNotEmpty: the two lists whose nil means "not read yet" keep the
// difference across the wire.
func TestNilIsNotEmpty(t *testing.T) {
	s := factory.Snapshot{Items: []factory.Item{{ID: 1}, {ID: 2, Comments: []factory.Comment{}}}}
	got := wire(t, s)
	if got.Items[0].Comments != nil || got.Items[1].Comments == nil {
		t.Errorf("comments nil-ness lost: %#v %#v", got.Items[0].Comments, got.Items[1].Comments)
	}
	if got.Checkouts != nil {
		t.Errorf("nil checkouts came back %#v", got.Checkouts)
	}
	s.Checkouts = map[string]string{}
	if wire(t, s).Checkouts == nil {
		t.Error("empty checkouts came back nil")
	}
}

func TestFixtureQueueCarriesPresenceCounts(t *testing.T) {
	b, err := json.Marshal(FromSnapshot(factory.Fixture(at), "me"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	capacity, _ := raw["capacity"].(map[string]any)
	if capacity["machines_online"] != float64(0) || capacity["busy"] != float64(0) {
		t.Fatalf("fixture capacity %#v", capacity)
	}
	if _, ok := capacity["workers"]; ok {
		t.Fatal("the unused workers list was serialized")
	}
	for _, job := range raw["jobs"].([]any) {
		if _, ok := job.(map[string]any)["held_by"]; ok {
			t.Fatal("the fixture has no lease, so no job carries held_by")
		}
	}
}

func TestWireSpeaksSnakeCase(t *testing.T) {
	b, err := json.Marshal(FromSnapshot(factory.Fixture(at), "me"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	var keys []string
	walkKeys(raw, &keys)
	sort.Strings(keys)
	for _, k := range keys {
		for _, r := range k {
			if r >= 'A' && r <= 'Z' {
				t.Errorf("key %q is not snake_case", k)
				break
			}
		}
	}
}

func walkKeys(v any, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			*out = append(*out, k)
			walkKeys(e, out)
		}
	case []any:
		for _, e := range x {
			walkKeys(e, out)
		}
	}
}

// diff walks two values and names where they differ. A nil and an empty
// list or map are the same here (TestNilIsNotEmpty holds the two that
// matter), times compare by instant, and fields Lost names are skipped.
func diff(a, b reflect.Value, path string) []string {
	if a.Type() == reflect.TypeOf(time.Time{}) {
		if !a.Interface().(time.Time).Equal(b.Interface().(time.Time)) {
			return []string{fmt.Sprintf("%s: %v, want %v", path, b, a)}
		}
		return nil
	}
	switch a.Kind() {
	case reflect.Pointer, reflect.Interface:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				return []string{fmt.Sprintf("%s: nil-ness differs", path)}
			}
			return nil
		}
		return diff(a.Elem(), b.Elem(), path)
	case reflect.Struct:
		var out []string
		for i := 0; i < a.NumField(); i++ {
			f := a.Type().Field(i)
			if _, lost := Lost[a.Type().Name()+"."+f.Name]; lost || !f.IsExported() {
				continue
			}
			out = append(out, diff(a.Field(i), b.Field(i), path+"."+f.Name)...)
		}
		return out
	case reflect.Slice, reflect.Array:
		if a.Len() != b.Len() {
			return []string{fmt.Sprintf("%s: len %d, want %d", path, b.Len(), a.Len())}
		}
		var out []string
		for i := 0; i < a.Len(); i++ {
			out = append(out, diff(a.Index(i), b.Index(i), fmt.Sprintf("%s[%d]", path, i))...)
		}
		return out
	case reflect.Map:
		if a.Len() != b.Len() {
			return []string{fmt.Sprintf("%s: len %d, want %d", path, b.Len(), a.Len())}
		}
		var out []string
		for _, k := range a.MapKeys() {
			bv := b.MapIndex(k)
			if !bv.IsValid() {
				out = append(out, fmt.Sprintf("%s[%v]: missing", path, k))
				continue
			}
			out = append(out, diff(a.MapIndex(k), bv, fmt.Sprintf("%s[%v]", path, k))...)
		}
		return out
	case reflect.Func:
		return nil
	}
	if !reflect.DeepEqual(a.Interface(), b.Interface()) {
		return []string{fmt.Sprintf("%s: %v, want %v", path, b, a)}
	}
	return nil
}

// fill sets every field it can reach to a value no other field shares, so a
// field the codec crossed with another shows up as a difference.
func fill(v reflect.Value, path string, depth int) {
	if depth > 6 {
		return
	}
	if v.Type() == reflect.TypeOf(time.Time{}) {
		v.Set(reflect.ValueOf(at.Add(time.Duration(len(path)) * time.Minute)))
		return
	}
	if v.Type() == reflect.TypeOf(time.Duration(0)) {
		v.SetInt(int64(len(path)) * int64(time.Second))
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(path)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int64:
		v.SetInt(int64(len(path)))
	case reflect.Float64:
		v.SetFloat(float64(len(path)) + 0.25)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fill(v.Elem(), path, depth+1)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				fill(v.Field(i), path+"."+v.Type().Field(i).Name, depth+1)
			}
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fill(v.Index(0), path+"[0]", depth+1)
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			fill(v.Index(i), fmt.Sprintf("%s[%d]", path, i), depth+1)
		}
	case reflect.Map:
		k, e := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
		fill(k, path+".key", depth+1)
		fill(e, path+".value", depth+1)
		v.Set(reflect.MakeMap(v.Type()))
		v.SetMapIndex(k, e)
	}
}
