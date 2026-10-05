package secaf

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/delegate"
)

// stubHost is the host a run of the program is handed: what it says is kept,
// and its models are the stub API's.
type stubHost struct {
	mu        sync.Mutex
	workspace string
	records   string
	api       delegate.ModelAPI
	ceilings  delegate.Ceilings
	stages    []delegate.StageRecord
	steps     []delegate.StepRecord
	ending    *delegate.Ending
}

func (h *stubHost) Workspace() string           { return h.workspace }
func (h *stubHost) Ceilings() delegate.Ceilings { return h.ceilings }
func (h *stubHost) Models() delegate.ModelAPI   { return h.api }
func (h *stubHost) Records() string             { return h.records }
func (h *stubHost) Hello([]string)              {}
func (h *stubHost) Stage(s delegate.StageRecord) {
	h.mu.Lock()
	h.stages = append(h.stages, s)
	h.mu.Unlock()
}
func (h *stubHost) Step(s delegate.StepRecord) {
	h.mu.Lock()
	h.steps = append(h.steps, s)
	h.mu.Unlock()
}
func (h *stubHost) Terminal(e delegate.Ending) { h.mu.Lock(); h.ending = &e; h.mu.Unlock() }

// stubModels is a model API that answers every call with the smallest value
// its schema allows — and, for the location scanner, one real location — so
// the whole of sec-af's pipeline runs with no model behind it.
type stubModels struct {
	mu      sync.Mutex
	calls   int
	threads map[string]bool
	scanned []string
}

func (m *stubModels) serve(t *testing.T) delegate.ModelAPI {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Messages       []ai.Message       `json:"messages"`
			ResponseFormat *ai.ResponseFormat `json:"response_format"`
			Thread         string             `json:"prompt_cache_key"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("a call does not parse: %v", err)
		}
		m.mu.Lock()
		m.calls++
		if m.threads == nil {
			m.threads = map[string]bool{}
		}
		m.threads[body.Thread] = true
		m.mu.Unlock()
		text := "no chains"
		if schema := schemaOf(body.ResponseFormat, body.Messages); schema != nil {
			value := minimal(schema, schema)
			if title, _ := schema["title"].(string); title == "ScanLocationsResult" {
				user := body.Messages[len(body.Messages)-1].Content[0].Text
				m.mu.Lock()
				m.scanned = append(m.scanned, user)
				m.mu.Unlock()
				value = map[string]any{"locations": []any{map[string]any{
					"file_path": "app/views.py", "start_line": 4, "code_snippet": "os.popen(cmd)", "pattern_type": "command_injection"}}}
			}
			encoded, _ := json.Marshal(value)
			text = string(encoded)
		}
		cost := 0.001
		_ = json.NewEncoder(w).Encode(ai.Response{Choices: []ai.Choice{{Message: textMessage("assistant", text)}}, Usage: &ai.Usage{Cost: &cost}})
	}))
	t.Cleanup(server.Close)
	return delegate.ModelAPI{BaseURL: server.URL + "/v1", Token: "stub"}
}

func textMessage(role, text string) ai.Message {
	return ai.Message{Role: role, Content: []ai.ContentPart{{Type: "text", Text: text}}}
}

// schemaOf is the schema a call asks its answer to meet: its response format,
// or the one an agent session's system message carries.
func schemaOf(format *ai.ResponseFormat, messages []ai.Message) map[string]any {
	var raw []byte
	if format != nil && format.JSONSchema != nil {
		raw = format.JSONSchema.Schema
	} else if len(messages) > 0 && messages[0].Role == "system" {
		system := messages[0].Content[0].Text
		if at := strings.Index(system, "code fence:\n"); at >= 0 {
			raw = []byte(system[at+len("code fence:\n"):])
		}
	}
	var schema map[string]any
	if json.Unmarshal(raw, &schema) != nil {
		return nil
	}
	return schema
}

// minimal is the smallest value a schema allows: every required field, the
// first of every enum, an empty list.
func minimal(schema, root map[string]any) any {
	if ref, ok := schema["$ref"].(string); ok {
		defs, _ := root["$defs"].(map[string]any)
		target, _ := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
		return minimal(target, root)
	}
	if values, ok := schema["enum"].([]any); ok && len(values) > 0 {
		return values[0]
	}
	if options, ok := schema["anyOf"].([]any); ok && len(options) > 0 {
		first, _ := options[0].(map[string]any)
		return minimal(first, root)
	}
	kind := schema["type"]
	if list, ok := kind.([]any); ok && len(list) > 0 {
		kind = list[0]
	}
	switch kind {
	case "object":
		out := map[string]any{}
		properties, _ := schema["properties"].(map[string]any)
		required, _ := schema["required"].([]any)
		for _, name := range required {
			if property, ok := properties[name.(string)].(map[string]any); ok {
				out[name.(string)] = minimal(property, root)
			}
		}
		return out
	case "array":
		return []any{}
	case "string":
		return "x"
	case "integer":
		return 1
	case "number":
		return 0.5
	case "boolean":
		return false
	case "null":
		return nil
	}
	return "x"
}

func vulnerableRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for rel, text := range map[string]string{
		"app/views.py":     "import os\n\ndef run(request):\n    return os.popen(request.args['cmd']).read()\n",
		"app/models.py":    "class User:\n    password = 'hunter2'\n",
		"requirements.txt": "flask==0.12\n",
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	real, _ := filepath.EvalSymlinks(root)
	return real
}

// snapshot is every file under root and its bytes.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			data, _ := os.ReadFile(path)
			files[path] = string(data)
		}
		return nil
	})
	return files
}

// THE WHOLE AUDIT RUNS ON CODEAF'S ROAD AND LEAVES THE FOLDER AS IT WAS. sec-af's
// pipeline, from recon to the report, runs over the stub model API: every
// call reaches it as its own thread, the program reports its phases and its
// sessions, the full report lands in the record folder, the conversation's
// account names it, and not one byte of the repository changed.
func TestAnAuditRunsTheWholePipelineAndChangesNothing(t *testing.T) {
	repo := vulnerableRepo(t)
	before := snapshot(t, repo)
	models := &stubModels{}
	host := &stubHost{workspace: repo, records: t.TempDir(), api: models.serve(t)}
	run(context.Background(), host, options{brief: "whole repository quick", severity: "low", sessions: 4, maxTurns: 5}, io.Discard)

	if host.ending == nil {
		t.Fatal("the run wrote no ending")
	}
	ending := *host.ending
	if ending.Status != delegate.StatusPass {
		t.Fatalf("the audit ended %s: %s (%s)", ending.Status, ending.Message, ending.Reason)
	}
	if ending.Extra["status"] != "pass" {
		t.Fatalf("the ending's data says %v", ending.Extra["status"])
	}
	if !strings.HasPrefix(ending.Deliverable, "Security audit of the whole repository, quick.") {
		t.Fatalf("the account begins %q", firstLine(ending.Deliverable))
	}
	readable, _ := os.ReadFile(filepath.Join(host.records, reportMarkdown))
	for _, want := range []string{"# sec — security audit of the whole repository, quick", "## What it found", "## How it ran"} {
		if !strings.Contains(string(readable), want) {
			t.Errorf("the readable report lacks %q:\n%s", want, readable)
		}
	}
	for _, unwanted := range []string{"Verdict", "inconclusive", "not exploitable", "$0.00", "Provider"} {
		if strings.Contains(string(readable), unwanted) {
			t.Errorf("the readable report says %q:\n%s", unwanted, readable)
		}
	}
	for _, name := range []string{reportMarkdown, reportJSON, reportSARIF} {
		path := filepath.Join(host.records, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("the report %s was not written: %v", name, err)
		}
		if !strings.Contains(ending.Deliverable, path) {
			t.Errorf("the account does not name %s:\n%s", path, ending.Deliverable)
		}
	}
	if after := snapshot(t, repo); len(after) != len(before) {
		t.Fatalf("the repository changed: %d files before, %d after", len(before), len(after))
	} else {
		for path, text := range before {
			if after[path] != text {
				t.Fatalf("%s changed", path)
			}
		}
	}
	var stages []string
	for _, stage := range host.stages {
		stages = append(stages, stage.Stage+":"+stage.Status)
	}
	for _, want := range []string{"recon:running", "hunt:running", "prove:running", "report:running"} {
		if !strings.Contains(strings.Join(stages, " "), want) {
			t.Errorf("the stages %v lack %s", stages, want)
		}
	}
	sessions := map[string]bool{}
	for _, step := range host.steps {
		if step.Tool == toolSession {
			sessions[step.Command] = true
		}
	}
	for _, want := range []string{"architecture mapper", "hunt location scanner", "hunt finding enricher", "deciding agent", "dependency checker"} {
		if !sessions[want] {
			t.Errorf("no %s session was recorded; sessions were %v", want, keys(sessions))
		}
	}
	if len(models.threads) < 10 || models.calls < 10 {
		t.Fatalf("the pipeline made %d calls on %d threads", models.calls, len(models.threads))
	}
	for _, prompt := range models.scanned {
		if strings.Contains(prompt, "CHANGE UNDER AUDIT") {
			t.Fatal("an audit of the whole repository told its hunters about a change")
		}
	}
}

// AN AUDIT OF THE CHANGES TELLS ITS HUNTERS WHAT CHANGED: the changed file,
// uncommitted, and the change itself; and with nothing changed it ends at once,
// having spent nothing.
func TestAnAuditOfTheChangesFocusesItsHunters(t *testing.T) {
	repo := vulnerableRepo(t)
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "base")

	models := &stubModels{}
	host := &stubHost{workspace: repo, records: t.TempDir(), api: models.serve(t)}
	run(context.Background(), host, options{brief: "changes", severity: "low", sessions: 4, maxTurns: 5}, io.Discard)
	if host.ending == nil || host.ending.Status != delegate.StatusPass || !strings.Contains(host.ending.Message, "nothing to audit") {
		t.Fatalf("an audit with nothing changed ended %+v", host.ending)
	}
	if models.calls != 0 {
		t.Fatalf("an audit with nothing changed made %d model calls", models.calls)
	}

	if err := os.WriteFile(filepath.Join(repo, "app/views.py"), []byte("import os\n\ndef run(request):\n    return os.system(request.args['cmd'])\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	host = &stubHost{workspace: repo, records: t.TempDir(), api: models.serve(t)}
	run(context.Background(), host, options{brief: "changes quick", severity: "low", sessions: 4, maxTurns: 5}, io.Discard)
	if host.ending == nil || host.ending.Status != delegate.StatusPass {
		t.Fatalf("an audit of the changes ended %+v", host.ending)
	}
	if !strings.Contains(host.ending.Deliverable, "(1 changed files since the last commit") && !strings.Contains(host.ending.Deliverable, "1 changed files since") {
		t.Fatalf("the account does not say what changed:\n%s", host.ending.Deliverable)
	}
	if len(models.scanned) == 0 {
		t.Fatal("no hunter scanned")
	}
	for _, prompt := range models.scanned {
		if !strings.Contains(prompt, "CHANGE UNDER AUDIT") || !strings.Contains(prompt, "- app/views.py") || !strings.Contains(prompt, "os.system") {
			t.Fatalf("a hunter was not told the change:\n%s", prompt)
		}
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// THE BRIEF'S FEW WORDS ARE READ AS THE GUIDE TEACHES THEM, and a shell's
// flags win over them.
func TestTheBriefSaysTheScope(t *testing.T) {
	for _, tc := range []struct {
		brief       string
		changes     bool
		base, depth string
	}{
		{"whole repository", false, "", "standard"},
		{"", false, "", "standard"},
		{"changes", true, "", "standard"},
		{"the changes since origin/main, quick", true, "origin/main", "quick"},
		{"Changes against v1.2.0 thorough", true, "v1.2.0", "thorough"},
		{"diff vs main", true, "main", "standard"},
		{"look for injection in the login changes", false, "", "standard"},
	} {
		got := ReadScope(tc.brief, false, "", "")
		if got.Changes != tc.changes || got.Base != tc.base || got.Depth != tc.depth {
			t.Errorf("%q read as %+v", tc.brief, got)
		}
	}
	if got := ReadScope("whole repository quick", true, "dev", "thorough"); !got.Changes || got.Base != "dev" || got.Depth != "thorough" {
		t.Errorf("the flags did not win: %+v", got)
	}
}

// The crew's working seat reads the code and its light seat makes the single
// calls; a model the person asked for reads the code in its place.
func TestTheCrewSeatsTheAudit(t *testing.T) {
	if got := strings.Join(crewFlags(delegate.Crew{Hands: "v/hands", Light: "v/light"}), " "); got != "--model v/hands --light v/light" {
		t.Fatalf("crew flags %q", got)
	}
	if got := strings.Join(crewFlags(delegate.Crew{Hands: "v/hands", Asked: []string{"v/asked", "v/other"}}), " "); got != "--model v/asked" {
		t.Fatalf("asked flags %q", got)
	}
	if err := Program.Validate(); err != nil {
		t.Fatal(err)
	}
}

// NO MACHINERY WORDS REACH THE PAGE: sec-af's notes say what its provers
// decide in its own words, and the page says them in a person's.
func TestThePageSpeaksAPersonsWords(t *testing.T) {
	read := presentActions()
	for _, action := range []delegate.Action{
		{Kind: delegate.ActionStep, Tool: toolNote, Step: stageProve, Command: "PROVE phase complete: 3 verified"},
		{Kind: delegate.ActionStep, Tool: toolNote, Step: stageProve, Command: "Verdict agent starting"},
		{Kind: delegate.ActionStep, Tool: toolSession, Step: stageProve, Command: "verdict agent", Observation: "answered · 2 turns · 0 reads"},
		{Kind: delegate.ActionStage, Stage: stageProve, Status: "done", Data: stageData(map[string]any{"note": plainWords("PROVE phase complete: 2 verified")})},
	} {
		shown, ok := read(action)
		if !ok {
			t.Fatalf("%+v was left off the page", action)
		}
		if lower := strings.ToLower(shown.Text + " " + shown.Outcome); strings.Contains(lower, "verdict") || strings.Contains(lower, "verified") {
			t.Errorf("the page says %q", shown.Text)
		}
		if shown.Step != "prove" {
			t.Errorf("%q is under %q", shown.Text, shown.Step)
		}
	}
	for stage := range stageWords {
		if stepWords[stage] == "" {
			t.Errorf("the stage %s has no step word", stage)
		}
	}
}

// THE PAGE SPEAKS sec-af's PHASE NAMES, from the row to the notes: its step
// headings are the phases' own, its running and closing lines are sec-af's
// notes, and the notes that expose plumbing are reworded or left off.
func TestThePageUsesSecAfsPhaseNames(t *testing.T) {
	read := presentActions()
	for _, tc := range []struct {
		action delegate.Action
		step   string
		text   string
	}{
		{delegate.Action{Kind: delegate.ActionStage, Stage: stageRecon, Status: "running", Data: stageData(map[string]any{"note": "RECON phase starting"})}, "recon", "RECON phase starting"},
		{delegate.Action{Kind: delegate.ActionStage, Stage: stageRecon, Status: "done", Data: stageData(map[string]any{"note": "RECON phase complete"})}, "recon", "RECON phase complete"},
		{delegate.Action{Kind: delegate.ActionStage, Stage: stageRemediation, Status: "running"}, "remediate", "remediate"},
	} {
		shown, ok := read(tc.action)
		if !ok || shown.Step != tc.step || shown.Text != tc.text {
			t.Errorf("%s %s read as %+v (%v), want %q under %q", tc.action.Stage, tc.action.Status, shown, ok, tc.text, tc.step)
		}
	}
	for stage, word := range stageWords {
		if stage != stageStarting && stepWords[stage] != word {
			t.Errorf("the row says %q for %s and the page %q", word, stage, stepWords[stage])
		}
	}
	if got := rewriteNote("CWE expansion suggested 8 additional CWEs"); got != "" {
		t.Errorf("the CWE expansion note was kept: %q", got)
	}
	if got := rewriteNote("HUNT found 6 fingerprint-unique findings, running semantic dedup"); got != "HUNT found 6 distinct findings, merging duplicates" {
		t.Errorf("the dedup note reads %q", got)
	}
	if got := plainWords("Verifier starting"); got != "Verifier starting" {
		t.Errorf("sec-af's Verifier became %q", got)
	}
}
