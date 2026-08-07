package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestNotebookCommandListsRetractsAndRestores(t *testing.T) {
	t.Setenv("AFORGE_DAILY_BUDGET", "")
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "bad", Brief: "exercise notebook output", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, Intent: "exercise notebook output"}); err != nil {
		t.Fatal(err)
	}
	active, err := graph.RecordFact("", "tool:git", store.FactLesson, "always verify changes")
	if err != nil {
		t.Fatal(err)
	}
	quarantined, err := graph.RecordFact("", "user", store.FactPreference, "prefer compact tables")
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []store.FactQuery{
		{Cues: []string{"tool:git"}, Limit: 1},
		{Cues: []string{"user"}, Limit: 1},
	} {
		if facts, err := graph.SearchFacts(query); err != nil || len(facts) != 1 {
			t.Fatalf("prime uses: facts=%+v err=%v", facts, err)
		}
	}
	if err := graph.RecordFactInjection("bad", []int64{active.Seq}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("bad", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.Fail(claim, "bad outcome"); err != nil {
		t.Fatal(err)
	}
	if err := graph.QuarantineFact(quarantined.Seq, 0, store.FactOriginCLI); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	now := quarantined.Time.Add(3*time.Hour + time.Second)
	if err := runNotebookTo([]string{"--db", path}, &output, now); err != nil {
		t.Fatal(err)
	}
	rendered := output.String()
	for _, heading := range []string{"SEQ", "SCOPE", "KIND", "AGE", "USES", "RIDES", "BAD", "STATUS", "BELIEF"} {
		if !strings.Contains(rendered, heading) {
			t.Errorf("notebook output omitted heading %q: %q", heading, rendered)
		}
	}
	if !strings.Contains(rendered, "today's spend: $0.00 of $20.00 daily rail") {
		t.Fatalf("notebook omitted daily rail: %q", rendered)
	}
	activeLine := normalizedNotebookLine(rendered, active.Seq)
	wantActive := fmt.Sprintf("#%d tool:git lesson 3h ago 1 1 1 active always verify changes", active.Seq)
	if activeLine != wantActive {
		t.Fatalf("active notebook row = %q, want %q", activeLine, wantActive)
	}
	quarantineLine := normalizedNotebookLine(rendered, quarantined.Seq)
	wantQuarantine := fmt.Sprintf("#%d user preference 3h ago 1 0 0 quarantined prefer compact tables", quarantined.Seq)
	if quarantineLine != wantQuarantine {
		t.Fatalf("quarantined notebook row = %q, want %q", quarantineLine, wantQuarantine)
	}

	output.Reset()
	if err := runNotebookTo([]string{"retract", fmt.Sprintf("#%d", active.Seq), "--db", path},
		&output, now); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(output.String()); got != fmt.Sprintf("quarantined #%d: %s", active.Seq, active.Body) {
		t.Fatalf("retract output = %q", got)
	}
	assertNotebookStatus(t, path, active.Seq, store.FactQuarantined)

	output.Reset()
	if err := runNotebookTo([]string{"restore", fmt.Sprint(active.Seq), "--db", path},
		&output, now); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(output.String()); got != fmt.Sprintf("restored #%d: %s", active.Seq, active.Body) {
		t.Fatalf("restore output = %q", got)
	}
	assertNotebookStatus(t, path, active.Seq, store.FactActive)
}

func TestNotebookDisplaysCanonicalScopesAndAliases(t *testing.T) {
	t.Setenv("AFORGE_DAILY_BUDGET", "")
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	historical, err := graph.RecordFact("", "domain:podcasts", store.FactPlain, "podcasts use a loudness target")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.ReplaceFact(historical.Seq, "", "domain:podcasts", store.FactPlain,
		"podcasts use an explicit loudness target"); err != nil {
		t.Fatal(err)
	}
	if err := graph.AliasScope("domain:podcasts", "domain:podcast"); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := writeNotebook(&output, graph, time.Now()); err != nil {
		t.Fatal(err)
	}
	if line := normalizedNotebookLine(output.String(), historical.Seq); !strings.Contains(line, "domain:podcast") || strings.Contains(line, "domain:podcasts") {
		t.Fatalf("historical notebook row did not display its canonical scope: %q", line)
	}
	if !strings.Contains(output.String(), "domain:podcasts → domain:podcast") {
		t.Fatalf("notebook alias list = %q", output.String())
	}
}

func TestChatNotebookSearchEvidenceAndRetractMoment(t *testing.T) {
	commander, graph := openBudgetCommander(t, 20)
	commander.sessionID = "notebook-chat"
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "notebook-source", Brief: "learn the parser", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "notebook-chat", Intent: "learn the parser"}); err != nil {
		t.Fatal(err)
	}
	fact, err := graph.RecordFact("notebook-source", "repo:parser", store.FactLesson,
		"buffer parser reads before rendering")
	if err != nil {
		t.Fatal(err)
	}
	found := commander.SearchNotebook("buffer parser", 10)
	if len(found) != 1 || found[0].Seq != fact.Seq {
		t.Fatalf("notebook search = %+v", found)
	}
	if refs := commander.NotebookEvidence(fact.Seq); len(refs) != 1 || refs[0] != "notebook-source" {
		t.Fatalf("notebook evidence = %v", refs)
	}
	if err := commander.RetractNotebook(fact.Seq); err != nil {
		t.Fatal(err)
	}
	retracted, ok, err := graph.FactBySeq(fact.Seq)
	if err != nil || !ok || retracted.Status != store.FactQuarantined ||
		retracted.StatusOrigin != store.FactOriginUser {
		t.Fatalf("retracted fact = %+v ok=%t err=%v", retracted, ok, err)
	}
	messages, err := graph.Messages("notebook-chat", 0, 0)
	if err != nil || len(messages) != 1 || messages[0].Role != store.RoleSystem ||
		messages[0].Body != "· let go — buffer parser reads before rendering" {
		t.Fatalf("retract moment = %+v err=%v", messages, err)
	}
}

func TestChatNewSessionAdvancesAttachedWatermark(t *testing.T) {
	commander, graph := openBudgetCommander(t, 20)
	commander.attachSession = func(string) error { return nil }
	sessionID, err := commander.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	seen, found, err := graph.LastSeen()
	if err != nil || !found || seen.State != store.SeenAttached || seen.SessionID != sessionID {
		t.Fatalf("new-session seen = %+v found=%t err=%v", seen, found, err)
	}
}

func normalizedNotebookLine(rendered string, seq int64) string {
	prefix := fmt.Sprintf("#%d ", seq)
	for _, line := range strings.Split(rendered, "\n") {
		normalized := strings.Join(strings.Fields(line), " ")
		if strings.HasPrefix(normalized, prefix) {
			return normalized
		}
	}
	return ""
}

func assertNotebookStatus(t *testing.T, path string, seq int64, want string) {
	t.Helper()
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	fact, found, err := graph.FactBySeq(seq)
	if err != nil || !found || fact.Status != want {
		t.Fatalf("fact #%d status = %q found=%t err=%v, want %q", seq, fact.Status, found, err, want)
	}
}
