package draft

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// TestMain lets the test binary stand in for `codeaf`: the CRM script is real
// bash, and the shim it finds as `codeaf` runs this binary with DRAFT_CLI set.
func TestMain(m *testing.M) {
	if os.Getenv("DRAFT_CLI") == "1" {
		args := os.Args[1:]
		if len(args) > 0 && args[0] == "app" {
			args = args[1:]
		}
		os.Exit(Main(args, os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

// crmDefaults are the create-sheet facts the golden CRM was made with.
func crmDefaults(t *testing.T) Defaults {
	t.Helper()
	files, _ := spec.GoldenCRM()
	var plan spec.Plan
	var app spec.App
	mustJSON(t, files[spec.PlanFile], &plan)
	mustJSON(t, files[spec.AppFile], &app)
	return Defaults{AppID: app.ID, Space: app.Space, Ask: plan.Ask, AgentModel: app.Agent.Model, BudgetMonth: app.Budget.Month, Kit: app.Kit}
}

func mustJSON(t *testing.T, data []byte, into any) {
	t.Helper()
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatal(err)
	}
}

// shortDir is a short temp folder: a unix socket path may not pass ~100 bytes.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// runScript runs a bash script whose `codeaf` is the test binary, against dir.
func runScript(t *testing.T, dir, script string) (string, error) {
	t.Helper()
	bin := filepath.Join(shortDir(t), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	shim := "#!/bin/sh\nDRAFT_CLI=1 exec '" + self + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "codeaf"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), DirEnv+"="+dir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// fakeBuilder answers the server verbs the way the Builder will: ok for check
// and try, and done writes the phase entry.
func fakeBuilder(t *testing.T, dir string, s *Store) {
	t.Helper()
	stop, err := Serve(context.Background(), dir, func(_ context.Context, r Request) Reply {
		if r.Verb == "done" {
			if _, err := s.Append(AuthorCodeaf, "phase", []string{"done"}, r.Args[0]); err != nil {
				return Reply{Text: err.Error()}
			}
		}
		return Reply{OK: true, Text: "ok " + r.Verb}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
}

func TestCRMScriptRendersTheGoldenCRM(t *testing.T) {
	dir := shortDir(t)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	fakeBuilder(t, dir, s)
	if out, err := runScript(t, dir, crmScript); err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	st, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if st.Phase != PhaseDone {
		t.Fatalf("phase = %s, want done", st.Phase)
	}
	assertSameMeaning(t, Render(st, crmDefaults(t)))
}

// assertSameMeaning compares each rendered file with the golden one after
// decoding it, so key order and whitespace do not count.
func assertSameMeaning(t *testing.T, got spec.Files) {
	t.Helper()
	want, _ := spec.GoldenCRM()
	if !reflect.DeepEqual(got.Paths(), want.Paths()) {
		t.Fatalf("files = %v, want %v", got.Paths(), want.Paths())
	}
	for _, name := range want.Paths() {
		g, w := decodeFile(t, name, got[name]), decodeFile(t, name, want[name])
		if name == spec.PlanFile {
			// The golden plan.json predates plan tint; Render writes it, app.json is the proof.
			delete(g.(map[string]any), "tint")
		}
		for _, d := range diffPaths("", g, w) {
			t.Errorf("%s differs at %s", name, d)
		}
	}
}

// diffPaths lists where two decoded JSON values differ, one line per path.
func diffPaths(path string, got, want any) []string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return []string{path + ": got " + fmt.Sprint(got) + ", want an object"}
		}
		var out []string
		for _, k := range mapKeys(g, w) {
			out = append(out, diffPaths(path+"/"+k, g[k], w[k])...)
		}
		return out
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return []string{fmt.Sprintf("%s: got %v, want %v", path, got, want)}
		}
		var out []string
		for i := range w {
			out = append(out, diffPaths(fmt.Sprintf("%s/%d", path, i), g[i], w[i])...)
		}
		return out
	}
	if !reflect.DeepEqual(got, want) {
		return []string{fmt.Sprintf("%s: got %v, want %v", path, got, want)}
	}
	return nil
}

func mapKeys(a, b map[string]any) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	return sortedKeys(set)
}

func decodeFile(t *testing.T, name string, data []byte) any {
	t.Helper()
	switch {
	case name == spec.AgentFile:
		return strings.TrimSpace(string(data))
	case strings.HasSuffix(name, ".yaml"):
		var err error
		if data, err = spec.YAMLToJSON(data); err != nil {
			t.Fatal(err)
		}
	}
	var v any
	if err := json.Unmarshal(bytes.TrimSpace(data), &v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}
