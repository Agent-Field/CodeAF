package session

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// scopeRig uses the same index and presence records as the turn-context and
// tasks-tool fixtures, with an own row and another project to catch wider reads.
func scopeRig(t *testing.T) (*Agent, string) {
	t.Helper()
	root := t.TempDir()
	bucket := filepath.Join(root, "here")
	dir := filepath.Join(bucket, "0123456789abcdef")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Place = Place{Dir: dir, Workspace: c.Workspace}
		c.SessionFile = c.Place.Transcript()
	})
	if err := SaveMeta(dir, Meta{ID: "0123456789abcdef", Workspace: agent.config.Workspace}); err != nil {
		t.Fatal(err)
	}
	writeWindow(t, bucket, "theirs", "Other conversation", time.Second,
		PresenceTask{ID: "4", Title: "Sweep the call sites", State: string(TaskRunning),
			Files: []string{"internal/session/agent.go"}})
	appendTaskIndex(filepath.Join(bucket, taskIndexName),
		deltaLandedRow("99", "theirs", "Fix the nil-map crash", time.Now().Add(-time.Minute)))
	agent.mu.Lock()
	id := agent.sessionID()
	agent.mu.Unlock()
	appendTaskIndex(filepath.Join(bucket, taskIndexName),
		deltaLandedRow("7", id, "Repair this parser", time.Now().Add(-2*time.Minute)))
	machineSession(t, root, "away", "another", "Separate project", t.TempDir(), 4242, time.Second,
		PresenceTask{ID: "8", Title: "Separate project work", State: string(TaskRunning)})
	return agent, dir
}

// L-FIX9-cross A1
func TestConversationScopeLeavesOtherWorkOutOfTurnContext(t *testing.T) {
	agent, dir := scopeRig(t)
	if changed, err := SetConversationScope(dir, ScopeConversation); err != nil || !changed {
		t.Fatalf("set scope = %v, %v", changed, err)
	}
	agent.refreshElsewhere(context.Background())
	if got := volatileNote(agent); strings.Contains(got, "<elsewhere>") {
		t.Fatalf("other work reached the turn: %s", got)
	}
	if agent.elsewhereText != "" || len(agent.elsewhereTold) != 0 {
		t.Fatalf("other work was retained: %q, %+v", agent.elsewhereText, agent.elsewhereTold)
	}
	agent.SettleWrites()
	if !LastTold(dir).IsZero() {
		t.Fatal("a conversation told no other work advanced its told stamp")
	}
}

// L-FIX9-cross A2
func TestConversationScopeTasksReachOnlyItsOwnRows(t *testing.T) {
	agent, dir := scopeRig(t)
	if _, err := SetConversationScope(dir, ScopeConversation); err != nil {
		t.Fatal(err)
	}
	for _, args := range []string{`{}`, `{"scope":"everywhere"}`, `{"query":"nil-map"}`} {
		text, isError := runTool(t, agent, "tasks", args)
		if isError {
			t.Fatalf("search %s = %s", args, text)
		}
		for _, absent := range []string{"Fix the nil-map crash", "Sweep the call sites", "Separate project work", "running in other codeaf windows", "running in other projects"} {
			if strings.Contains(text, absent) {
				t.Fatalf("search %s reached %q: %s", args, absent, text)
			}
		}
		if args != `{"query":"nil-map"}` && !strings.Contains(text, "Repair this parser") {
			t.Fatalf("own work is missing: %s", text)
		}
	}
	for _, args := range []string{
		`{"id":99}`, `{"id":"fix-the-nil-map-crash"}`,
		`{"id":99,"say":"change the guard"}`, `{"id":99,"resolve":"accept"}`,
	} {
		text, isError := runTool(t, agent, "tasks", args)
		if !isError || !strings.Contains(text, "No task") || strings.Contains(text, "Fix the nil-map crash") {
			t.Fatalf("other task %s = %s (error %v)", args, text, isError)
		}
	}
	text, isError := runTool(t, agent, "tasks", `{"id":7}`)
	if isError || !strings.Contains(text, "Repair this parser") {
		t.Fatalf("own task read = %s (error %v)", text, isError)
	}
	if rows := agent.TaskIndex(); len(rows) != 2 {
		t.Fatalf("the person's task index was narrowed: %+v", rows)
	}
}

// L-FIX9-cross A3
func TestProjectScopeKeepsOtherWorkVisible(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "restored"}[reset], func(t *testing.T) {
			agent, dir := scopeRig(t)
			if reset {
				if _, err := SetConversationScope(dir, ScopeConversation); err != nil {
					t.Fatal(err)
				}
				agent.refreshElsewhere(context.Background())
				if _, err := SetConversationScope(dir, ScopeProject); err != nil {
					t.Fatal(err)
				}
			}
			agent.refreshElsewhere(context.Background())
			for _, want := range []string{"<elsewhere>", "Fix the nil-map crash", "Sweep the call sites"} {
				if got := volatileNote(agent); !strings.Contains(got, want) {
					t.Fatalf("project context lacks %q: %s", want, got)
				}
			}
			text, isError := runTool(t, agent, "tasks", `{}`)
			if isError || !strings.Contains(text, "Fix the nil-map crash") || !strings.Contains(text, "running in other codeaf windows") {
				t.Fatalf("project tasks = %s (error %v)", text, isError)
			}
			text, isError = runTool(t, agent, "tasks", `{"scope":"everywhere"}`)
			if isError || !strings.Contains(text, "Separate project work") {
				t.Fatalf("project wide tasks = %s (error %v)", text, isError)
			}
			text, isError = runTool(t, agent, "tasks", `{"id":99}`)
			if isError || !strings.Contains(text, "Fix the nil-map crash") {
				t.Fatalf("project task read = %s (error %v)", text, isError)
			}
		})
	}
}

// L-FIX9-cross A5
func TestConversationScopeClearsAnEarlierBlockOnTheNextReading(t *testing.T) {
	agent, dir := scopeRig(t)
	agent.refreshElsewhere(context.Background())
	agent.SettleWrites()
	if !strings.Contains(volatileNote(agent), "<elsewhere>") || len(agent.elsewhereTold) == 0 {
		t.Fatal("the fixture never delivered other work")
	}
	was := LastTold(dir)
	if _, err := SetConversationScope(dir, ScopeConversation); err != nil {
		t.Fatal(err)
	}
	agent.refreshElsewhere(context.Background())
	agent.SettleWrites()
	agent.mu.Lock()
	current := agent.volatileBlockLocked()
	agent.mu.Unlock()
	if agent.elsewhereText != "" || agent.elsewhereTold != nil || strings.Contains(current, "<elsewhere>") {
		t.Fatalf("earlier work survived the next reading: %s", current)
	}
	if !LastTold(dir).Equal(was) {
		t.Fatal("clearing the block advanced the told stamp")
	}
}

// L-FIX9-cross A6
func TestConversationScopeChangesOnlyItsStoredWord(t *testing.T) {
	dir := t.TempDir()
	meta := Meta{
		ID: "0123456789abcdef", Workspace: t.TempDir(), Title: "Parser repairs",
		Model: "test/model", Effort: "high", Approval: PostureAsk,
		SpentUSD: .25, Tokens: 140, Created: time.Now().Add(-time.Hour).UTC(),
		LastUserAt: time.Now().Add(-time.Minute).UTC(), Archived: true,
		Places: []PlaceRef{{Path: t.TempDir()}},
	}
	if err := SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	transcript := (Place{Dir: dir}).Transcript()
	journal := []byte("{\"type\":\"session\",\"id\":\"0123456789abcdef\"}\n")
	if err := os.WriteFile(transcript, journal, 0o600); err != nil {
		t.Fatal(err)
	}
	before := scopeMetaFields(t, dir)
	for _, word := range []string{ScopeConversation, ScopeProject} {
		changed, err := SetConversationScope(dir, word)
		if err != nil || !changed || ConversationScopeAt(dir) != word {
			t.Fatalf("set %q = %v, %v; stored %q", word, changed, err, ConversationScopeAt(dir))
		}
		after := scopeMetaFields(t, dir)
		if word == ScopeConversation {
			if string(after["scope"]) != `"conversation"` {
				t.Fatalf("scope = %s", after["scope"])
			}
		} else if _, present := after["scope"]; present {
			t.Fatal("project must be stored as absence")
		}
		delete(after, "scope")
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("scope changed other metadata: before %v, after %v", before, after)
		}
		got, err := os.ReadFile(transcript)
		if err != nil || !bytes.Equal(got, journal) {
			t.Fatalf("scope changed the transcript: %q, %v", got, err)
		}

		path := filepath.Join(dir, placeMeta)
		old := time.Unix(1234567890, 0)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		was, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		changed, err = SetConversationScope(dir, word)
		if err != nil || changed {
			t.Fatalf("repeat %q = %v, %v", word, changed, err)
		}
		now, err := os.Stat(path)
		if err != nil || !os.SameFile(was, now) || !was.ModTime().Equal(now.ModTime()) {
			t.Fatalf("repeat %q rewrote meta.json: %v", word, err)
		}
	}
}

func scopeMetaFields(t *testing.T, dir string) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, placeMeta))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

// L-FIX9-cross A8
func TestConversationScopeRefusesUnknownWordsAndMissingConversations(t *testing.T) {
	dir := t.TempDir()
	if err := SaveMeta(dir, Meta{ID: "mine", Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	before := scopeMetaFields(t, dir)
	for _, word := range []string{"", "everywhere", "Conversation", " conversation "} {
		if changed, err := SetConversationScope(dir, word); err == nil || changed {
			t.Fatalf("unknown %q = %v, %v", word, changed, err)
		}
	}
	if !reflect.DeepEqual(before, scopeMetaFields(t, dir)) {
		t.Fatal("an unknown word changed metadata")
	}
	for _, dir := range []string{"", "  ", t.TempDir(), filepath.Join(t.TempDir(), "absent")} {
		if changed, err := SetConversationScope(dir, ScopeConversation); err == nil || changed {
			t.Fatalf("no conversation at %q = %v, %v", dir, changed, err)
		}
		if dir != "" && dir != "  " {
			if _, err := os.Stat(filepath.Join(dir, placeMeta)); !os.IsNotExist(err) {
				t.Fatalf("a missing conversation acquired meta.json: %v", err)
			}
		}
	}
}

// L-FIX9-cross A3
func TestConversationScopeDefaultsQuietlyToProject(t *testing.T) {
	for _, stored := range []string{"", "project", "Conversation", "elsewhere", ScopeConversation} {
		dir := t.TempDir()
		if err := SaveMeta(dir, Meta{ID: "mine", Scope: stored}); err != nil {
			t.Fatal(err)
		}
		want := ScopeProject
		if stored == ScopeConversation {
			want = ScopeConversation
		}
		if got := ConversationScopeAt(dir); got != want {
			t.Fatalf("stored %q reads %q, want %q", stored, got, want)
		}
	}
	for _, raw := range []string{"", "{broken", `{"scope":"conversation"}`} {
		dir := t.TempDir()
		if raw != "" {
			if err := os.WriteFile(filepath.Join(dir, placeMeta), []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if got := ConversationScopeAt(dir); got != ScopeProject {
			t.Fatalf("unreadable identity %q reads %q", raw, got)
		}
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, placeMeta), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := ConversationScopeAt(dir); got != ScopeProject {
		t.Fatalf("failed metadata read = %q", got)
	}
	if got := ConversationScopeAt(""); got != ScopeProject {
		t.Fatalf("no folder = %q", got)
	}
}

// L-FIX9-cross A4
func TestConversationScopeSurvivesReloadAndLaterMetadataStamps(t *testing.T) {
	agent, dir := scopeRig(t)
	stale := agent.metaSnapshot()
	if _, err := SetConversationScope(dir, ScopeConversation); err != nil {
		t.Fatal(err)
	}
	place := t.TempDir()
	for _, stamp := range []struct {
		name  string
		write func()
	}{
		{"effort", func() {
			if !agent.SetConversationEffort("high") {
				t.Fatal("effort was refused")
			}
		}},
		{"title", func() { agent.stampTitle("Parser repairs") }},
		{"spend", func() { agent.writeSpendSnapshot(dir, stale, .25, 140) }},
		{"places", func() {
			agent.mu.Lock()
			agent.places = []PlaceRef{{Path: place, Arrival: PlaceSaid}}
			agent.mu.Unlock()
			agent.stampPlaces()
		}},
		{"trees", agent.stampTrees},
		{"model", agent.stampMeta},
		{"approval", func() {
			agent.config.ApprovalGate = &fakeApprovalGate{standing: PostureAsk}
			if err := agent.SetApprovalPosture(PostureAllow); err != nil {
				t.Fatal(err)
			}
		}},
		{"user", func() {
			agent.mu.Lock()
			agent.stampUserLocked("Keep the parser repair")
			agent.mu.Unlock()
		}},
	} {
		stamp.write()
		agent.SettleWrites()
		meta, err := LoadMeta(dir)
		if err != nil || meta.Scope != ScopeConversation || ConversationScopeAt(dir) != ScopeConversation {
			t.Fatalf("%s lost scope: %+v, %v", stamp.name, meta, err)
		}
	}
	meta, err := LoadMeta(dir)
	if err != nil || meta.Effort != "high" || meta.Title != "Parser repairs" || meta.SpentUSD != .25 || meta.Approval != PostureAllow || len(meta.Places) != 1 || meta.Places[0].Path != place {
		t.Fatalf("the later stamps did not land: %+v, %v", meta, err)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = agent.config.Workspace
		c.Place = agent.config.Place
		c.SessionFile = c.Place.Transcript()
	})
	reopened.refreshElsewhere(context.Background())
	if strings.Contains(volatileNote(reopened), "<elsewhere>") || ConversationScopeAt(dir) != ScopeConversation {
		t.Fatal("a reopened conversation forgot its scope")
	}
}

// L-FIX9-cross A2
func TestConversationScopeKeepsItsLiveGraphWhileFilteringTheMergedIndex(t *testing.T) {
	agent, _, id := runningStubbedNode(t, "Keep my live task")
	dir := filepath.Join(t.TempDir(), "mine")
	agent.mu.Lock()
	agent.config.Place = Place{Dir: dir, Workspace: agent.config.Workspace}
	agent.mu.Unlock()
	if err := SaveMeta(dir, Meta{ID: "mine"}); err != nil {
		t.Fatal(err)
	}
	number := strconv.FormatUint(id, 10)
	appendTaskIndex(agent.config.taskIndexFile(),
		deltaLandedRow(number, "theirs", "An unrelated newer task", time.Now()))
	if _, err := SetConversationScope(dir, ScopeConversation); err != nil {
		t.Fatal(err)
	}
	text, isError := runTool(t, agent, "tasks", `{}`)
	if isError || !strings.Contains(text, "Keep my live task") || strings.Contains(text, "An unrelated newer task") {
		t.Fatalf("merged listing = %s (error %v)", text, isError)
	}
	text, isError = runTool(t, agent, "tasks", `{"id":`+number+`}`)
	if isError || !strings.Contains(text, "Keep my live task") || strings.Contains(text, "An unrelated newer task") {
		t.Fatalf("merged read = %s (error %v)", text, isError)
	}
	if rows := agent.TaskIndex(); len(rows) != 2 {
		t.Fatalf("the person's merged index was narrowed: %+v", rows)
	}
}

// L-FIX9-cross A2
func TestConversationScopeLeavesATaskNodesChildrenVisible(t *testing.T) {
	owner, dir := scopeRig(t)
	graph := stubbedGraph(owner, func(*TaskNode) {})
	parent := graph.reserve()
	graph.admit(parent, taskSpec{title: "Parent task", named: true, brief: "b", acceptance: "a"})
	child := graph.reserve()
	graph.admit(child, taskSpec{title: "Its own piece", named: true, brief: "b", acceptance: "a", parent: parent})
	worker, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Place = owner.config.Place
		c.InTask, c.taskID, c.tasker = true, parent, graph
	})
	for _, scope := range []string{ScopeProject, ScopeConversation} {
		if _, err := SetConversationScope(dir, scope); err != nil {
			t.Fatal(err)
		}
		text, isError := runTool(t, worker, "tasks", `{"scope":"everywhere"}`)
		if isError || !strings.Contains(text, "Its own piece") || strings.Contains(text, "Parent task") || strings.Contains(text, "Fix the nil-map crash") || strings.Contains(text, "Separate project work") {
			t.Fatalf("node under %q = %s (error %v)", scope, text, isError)
		}
		worker.refreshElsewhere(context.Background())
		if worker.elsewhereText != "" {
			t.Fatalf("node under %q received other work: %s", scope, worker.elsewhereText)
		}
	}
}

// L-FIX9-cross A6
func TestConversationScopeStaysInMetaBesideSealedConversationSettings(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, cellStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveMeta(dir, Meta{ID: "mine", Model: "test/model", Effort: "high", Approval: PostureAsk}); err != nil {
		t.Fatal(err)
	}
	path := layoutOf(dir).metaTruth(dir)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{ScopeConversation, ScopeProject} {
		if _, err := SetConversationScope(dir, scope); err != nil {
			t.Fatal(err)
		}
		if got := ConversationScopeAt(dir); got != scope {
			t.Fatalf("stored scope = %q, want %q", got, scope)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("scope changed sealed settings: %q, %v", after, err)
		}
		meta, err := LoadMeta(dir)
		if err != nil || meta.Model != "test/model" || meta.Effort != "high" || meta.Approval != PostureAsk {
			t.Fatalf("scope changed conversation settings: %+v, %v", meta, err)
		}
	}
}
