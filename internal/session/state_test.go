package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stateAgent is a session whose journal — and therefore whose state file —
// lives in a directory the test owns.
func stateAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	return agent, journal
}

func trackBelief(t *testing.T, agent *Agent, text, evidence string) string {
	t.Helper()
	out, isError := runTool(t, agent, "track",
		`{"text":"`+text+`","kind":"belief","evidence":"`+evidence+`"}`)
	if isError {
		t.Fatalf("track belief %q failed: %s", text, out)
	}
	return out
}

func trackProgress(t *testing.T, agent *Agent, text, evidence string) string {
	t.Helper()
	out, isError := runTool(t, agent, "track",
		`{"text":"`+text+`","kind":"progress","evidence":"`+evidence+`"}`)
	if isError {
		t.Fatalf("track progress %q failed: %s", text, out)
	}
	return out
}

// sectionIndex is where one section header sits in a rendered block, or -1.
func sectionIndex(block, section string) int {
	for index, line := range strings.Split(block, "\n") {
		if line == section+":" {
			return index
		}
	}
	return -1
}

// ── the round trip ──────────────────────────────────────────────────────────

// The whole contract in one test: a belief and a subgoal go in, recall shows
// both under the right headings with the evidence they cite, commit closes the
// subgoal, and recall then shows it as history rather than as work.
func TestTrackCommitRecallRoundTrip(t *testing.T) {
	agent, _ := stateAgent(t)

	if out := trackBelief(t, agent, "the module path is github.com/Agent-Field/aforge-v2", "read: go.mod"); !strings.Contains(out, "b1") {
		t.Fatalf("track did not report the belief's id:\n%s", out)
	}
	if out := trackProgress(t, agent, "wire StateBlock into the compaction rebuild", "grep: compact loop.go"); !strings.Contains(out, "p1") {
		t.Fatalf("track did not report the progress id:\n%s", out)
	}

	block, isError := runTool(t, agent, "recall", `{}`)
	if isError {
		t.Fatalf("recall failed: %s", block)
	}
	for _, want := range []string{
		"[state]", "beliefs:", "open:",
		"b1 the module path is github.com/Agent-Field/aforge-v2  ← read: go.mod",
		"p1 wire StateBlock into the compaction rebuild  ← grep: compact loop.go",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("recall is missing %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "done:") {
		t.Fatalf("recall shows a done section with nothing done:\n%s", block)
	}

	out, isError := runTool(t, agent, "commit", `{"id":"p1"}`)
	if isError {
		t.Fatalf("commit failed: %s", out)
	}
	if !strings.Contains(out, "done") {
		t.Fatalf("commit did not report the new status:\n%s", out)
	}

	block, _ = runTool(t, agent, "recall", `{}`)
	if strings.Contains(block, "open:") {
		t.Fatalf("the committed subgoal is still open:\n%s", block)
	}
	if index := sectionIndex(block, "done"); index < 0 {
		t.Fatalf("the committed subgoal is not in a done section:\n%s", block)
	}

	// Committing something already closed is a FACT, not a failure: a model
	// told this was an error would retry it in different words.
	out, isError = runTool(t, agent, "commit", `{"id":"p1"}`)
	if isError {
		t.Fatalf("re-committing p1 was reported as an error: %s", out)
	}
	if !strings.Contains(out, "already") {
		t.Fatalf("re-committing p1 did not say it was already done:\n%s", out)
	}

	// A belief is not deleted by commit, it goes stale — and a stale belief
	// leaves the block, because it is no longer true.
	if out, isError := runTool(t, agent, "commit", `{"id":"b1"}`); isError {
		t.Fatalf("committing a belief failed: %s", out)
	}
	block, _ = runTool(t, agent, "recall", `{}`)
	if strings.Contains(block, "the module path") {
		t.Fatalf("a stale belief is still rendered:\n%s", block)
	}

	// An unknown id is a mistake the model can fix with one call, so it is an
	// error that names the call.
	out, isError = runTool(t, agent, "commit", `{"id":"p9"}`)
	if !isError {
		t.Fatalf("committing an unknown id succeeded: %s", out)
	}
	if !strings.Contains(out, "recall") {
		t.Fatalf("the unknown-id error does not point at recall:\n%s", out)
	}
}

// Evidence is the load-bearing field: a record that cites nothing is a guess
// that a future compaction will hand forward as a fact. The tool refuses it.
func TestTrackRequiresEvidence(t *testing.T) {
	agent, _ := stateAgent(t)

	for _, arguments := range []string{
		`{"text":"tests pass","kind":"belief","evidence":""}`,
		`{"text":"tests pass","kind":"belief","evidence":"   "}`,
		`{"text":"tests pass","kind":"belief"}`,
	} {
		out, isError := runTool(t, agent, "track", arguments)
		if !isError {
			t.Fatalf("track(%s) was accepted with no evidence: %s", arguments, out)
		}
		if !strings.Contains(out, "evidence is required") {
			t.Fatalf("track(%s) did not say why:\n%s", arguments, out)
		}
	}
	// And nothing was recorded on the way to being refused.
	if block := agent.StateBlock(); block != "" {
		t.Fatalf("a refused track left a record:\n%s", block)
	}

	// The same law for the other two required fields.
	if out, isError := runTool(t, agent, "track", `{"text":"","kind":"belief","evidence":"read: go.mod"}`); !isError {
		t.Fatalf("track with no text was accepted: %s", out)
	}
	if out, isError := runTool(t, agent, "track", `{"text":"x","kind":"guess","evidence":"read: go.mod"}`); !isError {
		t.Fatalf("track with an unknown kind was accepted: %s", out)
	}
}

// ── the file ────────────────────────────────────────────────────────────────

// State survives the process, which is the half of the point that compaction
// does not cover: a session resumed tomorrow re-reads its beliefs instead of
// re-deriving them from a summary of a summary.
func TestStateSurvivesAReconstructedAgent(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{}
	first, err := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM", SessionFile: journal,
	}, completer)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	trackBelief(t, first, "the build is green", "bash: go build ./...")
	trackProgress(t, first, "write the state tests", "write: internal/session/state_test.go")
	if out, isError := runTool(t, first, "commit", `{"id":"p1"}`); isError {
		t.Fatalf("commit failed: %s", out)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	path := statePath(journal)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no state file beside the journal at %s: %v", path, err)
	}

	second, err := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM", SessionFile: journal,
	}, completer)
	if err != nil {
		t.Fatalf("newAgent (resumed): %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	block := second.StateBlock()
	if !strings.Contains(block, "b1 the build is green  ← bash: go build ./...") {
		t.Fatalf("the belief did not survive the resume:\n%s", block)
	}
	if !strings.Contains(block, "p1 write the state tests") {
		t.Fatalf("the finished subgoal did not survive the resume:\n%s", block)
	}
	if sectionIndex(block, "done") < 0 {
		t.Fatalf("the finished subgoal came back as something other than done:\n%s", block)
	}

	// Ids continue rather than restart: a resumed session that minted b1 again
	// would give two different facts the same name.
	out := trackBelief(t, second, "the tests pass", "bash: go test ./internal/session")
	if !strings.Contains(out, "b2") {
		t.Fatalf("a resumed session reused an id:\n%s", out)
	}
}

// A corrupt state file costs the person a block the model can rebuild by
// working. Refusing to start costs them the conversation. So it is dropped
// whole, the session opens, and the next track writes a valid file over it.
func TestCorruptStateFileIsIgnored(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(statePath(journal), []byte(`{"type":"state","version":1,"records":[{`), 0o644); err != nil {
		t.Fatalf("write the corrupt file: %v", err)
	}

	agent, err := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM", SessionFile: journal,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("a corrupt state file made the session fatal: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })

	if block := agent.StateBlock(); block != "" {
		t.Fatalf("a corrupt state file was partially loaded:\n%s", block)
	}
	trackBelief(t, agent, "the file was replaced", "write: state.json")
	if block := agent.StateBlock(); !strings.Contains(block, "the file was replaced") {
		t.Fatalf("the session could not record after a corrupt load:\n%s", block)
	}
	content, err := os.ReadFile(statePath(journal))
	if err != nil {
		t.Fatalf("read the rewritten file: %v", err)
	}
	if _, err := decodeState(content); err != nil {
		t.Fatalf("the rewritten file does not validate: %v", err)
	}
}

// Every rule decodeState enforces is one the tools enforce on the way in, so a
// file that breaks one was not written by this store — and half-loading it
// would be a state nobody wrote.
func TestDecodeStateRejects(t *testing.T) {
	cases := map[string]string{
		"not json":      `{`,
		"wrong type":    `{"type":"memory","version":1,"records":[]}`,
		"wrong version": `{"type":"state","version":99,"records":[]}`,
		"no evidence":   `{"type":"state","version":1,"records":[{"id":"b1","kind":"belief","text":"x","status":"live","evidence":""}]}`,
		"no text":       `{"type":"state","version":1,"records":[{"id":"b1","kind":"belief","text":"","status":"live","evidence":"read: go.mod"}]}`,
		"unknown kind":  `{"type":"state","version":1,"records":[{"id":"x1","kind":"vibe","text":"x","status":"live","evidence":"read: go.mod"}]}`,
		"wrong status":  `{"type":"state","version":1,"records":[{"id":"b1","kind":"belief","text":"x","status":"done","evidence":"read: go.mod"}]}`,
		"duplicated id": `{"type":"state","version":1,"records":[{"id":"b1","kind":"belief","text":"x","status":"live","evidence":"e"},{"id":"b1","kind":"belief","text":"y","status":"live","evidence":"e"}]}`,
		"no id at all":  `{"type":"state","version":1,"records":[{"kind":"belief","text":"x","status":"live","evidence":"e"}]}`,
	}
	for name, content := range cases {
		if _, err := decodeState([]byte(content)); err == nil {
			t.Fatalf("decodeState accepted %s", name)
		}
	}
	if _, err := decodeState([]byte(`{"type":"state","version":1,"records":[]}`)); err != nil {
		t.Fatalf("decodeState rejected an empty valid file: %v", err)
	}
}

// The state file belongs to ONE conversation. The session directory holds every
// session this workspace ever had, so a shared state.json there would be every
// window writing over each other's beliefs.
func TestStatePathIsPerJournal(t *testing.T) {
	if got, want := statePath("/tmp/x/20260815-120000_ab12cd.jsonl"), "/tmp/x/20260815-120000_ab12cd.state.json"; got != want {
		t.Fatalf("statePath = %q, want %q", got, want)
	}
	if got := statePath("  "); got != "" {
		t.Fatalf("a session with no journal got a state file at %q", got)
	}
	if a, b := statePath("/tmp/x/one.jsonl"), statePath("/tmp/x/two.jsonl"); a == b {
		t.Fatalf("two sessions in one directory share a state file: %s", a)
	}
}

// A session with no journal still records, still renders a block, and still
// survives a compaction — it just has nothing to resume from. Absent
// persistence must not mean an absent store.
func TestStateWorksWithNoJournal(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	trackBelief(t, agent, "there is no session file", "read: config")
	if block := agent.StateBlock(); !strings.Contains(block, "there is no session file") {
		t.Fatalf("a journal-less session kept no state:\n%s", block)
	}
}

// ── the compaction seam ─────────────────────────────────────────────────────

// The block is what a compaction pass re-injects, so its shape is a contract:
// nothing when there is nothing, sections in the order beliefs → open → done,
// and the newest record of each section first.
func TestStateBlockShapeAndOrdering(t *testing.T) {
	agent, _ := stateAgent(t)
	if block := agent.StateBlock(); block != "" {
		t.Fatalf("an empty store rendered a block:\n%s", block)
	}

	trackBelief(t, agent, "belief one", "read: a.go")
	trackBelief(t, agent, "belief two", "read: b.go")
	trackProgress(t, agent, "subgoal one", "bash: go test ./...")
	trackProgress(t, agent, "subgoal two", "bash: go build ./...")
	if out, isError := runTool(t, agent, "commit", `{"id":"p1"}`); isError {
		t.Fatalf("commit failed: %s", out)
	}

	block := agent.StateBlock()
	if !strings.HasPrefix(block, "[state] ") {
		t.Fatalf("the block does not open with its bracket:\n%s", block)
	}
	beliefs, open, done := sectionIndex(block, "beliefs"), sectionIndex(block, "open"), sectionIndex(block, "done")
	if beliefs < 0 || open < 0 || done < 0 {
		t.Fatalf("a section is missing:\n%s", block)
	}
	if !(beliefs < open && open < done) {
		t.Fatalf("sections are out of order (beliefs %d, open %d, done %d):\n%s", beliefs, open, done, block)
	}
	// Newest first inside a section: belief two was tracked after belief one.
	if strings.Index(block, "belief two") > strings.Index(block, "belief one") {
		t.Fatalf("beliefs are oldest-first:\n%s", block)
	}
	// A blocked subgoal is still open — it is not done — and says so.
	if out, isError := runTool(t, agent, "track",
		`{"text":"subgoal three","kind":"progress","evidence":"bash: go vet ./...","status":"blocked"}`); isError {
		t.Fatalf("tracking a blocked subgoal failed: %s", out)
	}
	block = agent.StateBlock()
	if !strings.Contains(block, "subgoal three (blocked)") {
		t.Fatalf("a blocked subgoal is not marked:\n%s", block)
	}
	if index := strings.Index(block, "subgoal three"); index < strings.Index(block, "open:") || index > strings.Index(block, "done:") {
		t.Fatalf("a blocked subgoal is not in the open section:\n%s", block)
	}

	// Re-tracking the same thing confirms it rather than duplicating it.
	trackBelief(t, agent, "belief one", "bash: go test ./internal/session")
	block = agent.StateBlock()
	if got := strings.Count(block, "belief one"); got != 1 {
		t.Fatalf("re-tracking duplicated a belief (%d rows):\n%s", got, block)
	}
	if !strings.Contains(block, "belief one  ← bash: go test ./internal/session") {
		t.Fatalf("re-tracking did not refresh the evidence:\n%s", block)
	}
	if strings.Index(block, "belief one") > strings.Index(block, "belief two") {
		t.Fatalf("a re-tracked belief did not move to the front:\n%s", block)
	}
}

// The cap is the promise that makes the seam affordable: every request after a
// compaction carries this block, so it may never grow with the conversation.
// Past the cap it elides OLDEST first and says how many it dropped — and it
// never lets finished history crowd out open work.
func TestStateBlockCap(t *testing.T) {
	agent, _ := stateAgent(t)

	for index := 1; index <= 30; index++ {
		trackBelief(t, agent, "belief "+itoa(index), "read: file"+itoa(index)+".go")
	}
	for index := 1; index <= 30; index++ {
		trackProgress(t, agent, "open subgoal "+itoa(index), "bash: step "+itoa(index))
	}
	for index := 31; index <= 60; index++ {
		trackProgress(t, agent, "closed subgoal "+itoa(index), "bash: step "+itoa(index))
		if out, isError := runTool(t, agent, "commit", `{"id":"p`+itoa(index)+`"}`); isError {
			t.Fatalf("commit failed: %s", out)
		}
	}

	block := agent.StateBlock()
	lines := strings.Split(block, "\n")
	if len(lines) > stateBlockLines {
		t.Fatalf("the block is %d lines, cap is %d:\n%s", len(lines), stateBlockLines, block)
	}
	if !strings.Contains(block, "not shown") {
		t.Fatalf("the block dropped records without saying so:\n%s", block)
	}
	// Neither of the two sections a compaction exists to preserve is starved by
	// the other, and finished history is squeezed hardest.
	beliefRows := strings.Count(block, "- b")
	openRows := strings.Count(block, "- p")
	if beliefRows < 5 || openRows < 5 {
		t.Fatalf("a section was starved (%d beliefs, %d progress rows):\n%s", beliefRows, openRows, block)
	}
	// Newest first survives the cap: the last thing tracked is present, the
	// first is not.
	if !strings.Contains(block, "open subgoal 30") {
		t.Fatalf("the newest open subgoal was elided:\n%s", block)
	}
	if strings.Contains(block, "belief 1 ") {
		t.Fatalf("the oldest belief survived a full cap:\n%s", block)
	}

	// recall's larger cap shows what the block could not.
	full, _ := runTool(t, agent, "recall", `{}`)
	if len(strings.Split(full, "\n")) <= len(lines) {
		t.Fatalf("recall showed no more than the block:\n%s", full)
	}
	if !strings.Contains(full, "belief 1 ") {
		t.Fatalf("recall is missing the oldest belief:\n%s", full)
	}
}

// itoa keeps the cap test readable without importing strconv for one call.
func itoa(number int) string {
	if number < 10 {
		return string(rune('0' + number))
	}
	return string(rune('0'+number/10)) + string(rune('0'+number%10))
}
