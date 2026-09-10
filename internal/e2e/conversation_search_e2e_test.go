//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// This experiment uses the live chat engine, its complete toolbelt and a real
// DeepSeek V4 Flash turn against a disposable indexed history. The fixtures
// carry the only possible answers; tool receipts and final prose must agree.
func TestConversationSearchLive(t *testing.T) {
	w := newManualWorld(t)
	brain, err := store.Open(filepath.Join(w.home, "conversation-fixture.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	add := func(id string, role store.Role, text string) int64 {
		msg, err := brain.PostMessage(store.Message{SessionID: id, Role: role, Body: text})
		if err != nil {
			t.Fatal(err)
		}
		return msg.Seq
	}
	alpha := w.place(w.projectBucket("/projects/alpha"), "/projects/alpha")
	beta := w.place(w.projectBucket("/projects/beta"), "/projects/beta")
	_, err = brain.OpenSession(alpha.ID(), "Amber rollout", "chat")
	if err != nil {
		t.Fatal(err)
	}
	_, err = brain.OpenSession(beta.ID(), "Harbour labels", "chat")
	if err != nil {
		t.Fatal(err)
	}
	anchor := add(alpha.ID(), store.RoleAgent, strings.Repeat("Background context for the rollout. ", 80)+"The amber launch code is CEDAR-81.")
	add(beta.ID(), store.RoleUser, "The harbour dashboard badge is violet-kestrel-47.")
	add(alpha.ID(), store.RoleUser, "Correction: use MAPLE-92 instead. That is the final choice.")
	for i := 0; i < 30; i++ {
		add(beta.ID(), store.RoleAgent, fmt.Sprintf("Unrelated planning note %d about ordinary review schedules.", i))
	}
	scenarios := []struct {
		name, ask, want string
		maxSearch       int
	}{
		{"correction", "What launch code did we finally choose in our earlier amber conversation?", "MAPLE-92", 2},
		{"other_project", "What badge did I pick in our earlier harbour dashboard conversation?", "violet-kestrel-47", 2},
		{"task_or_chat", "Find my recent harbour dashboard task and tell me which badge I picked.", "violet-kestrel-47", 2},
		{"open_by_id", fmt.Sprintf("Open this conversation source and tell me the corrected choice in that exchange: %s", session.ConversationReference(alpha.ID(), anchor)), "MAPLE-92", 2},
		{"missing", "Search our saved conversations for quasar-zebra-995. If there is no match, say you could not find it; do not guess.", "", 2},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			// The shared world directs log and failure reporting to this subtest.
			w.t = t
			agent, place := w.open(aPlainWorkspace(t), func(cfg *session.Config) { manualConfig(cfg); cfg.Memory = brain })
			if agent.Model() != e2eModel {
				t.Fatalf("wrong model: %s", agent.Model())
			}
			out := w.say(agent, sc.ask, answerNo)
			searches := 0
			evidence := ""
			seen := map[string]bool{}
			for _, call := range conversationReceiptCalls(t, place.Transcript()) {
				if call.Name == "search_conversations" {
					var args map[string]any
					_ = json.Unmarshal([]byte(call.Args), &args)
					canonical, _ := json.Marshal(args)
					if seen[string(canonical)] {
						t.Errorf("repeated identical lookup: %s", canonical)
					}
					seen[string(canonical)] = true
					searches++
					evidence += call.Output
					if call.Failed {
						t.Errorf("search failed: %s", call.Output)
					}
				}
				if call.Name == "bash" || call.Name == "read" || call.Name == "find" || call.Name == "ls" || call.Name == "grep" {
					t.Errorf("lookup detoured through %s", call.Name)
				}
			}
			if searches < 1 || searches > sc.maxSearch {
				t.Errorf("search calls=%d, want 1..%d", searches, sc.maxSearch)
			}
			if sc.want != "" && (!strings.Contains(out.Reply, sc.want) || !strings.Contains(evidence, sc.want)) {
				t.Errorf("answer or receipt missed %q: %s", sc.want, out.Reply)
			}
			if sc.want == "" && !strings.Contains(evidence, "Nothing said in any earlier conversation matches") {
				t.Errorf("missing query did not produce a miss: %s", evidence)
			}
			if sc.want == "" && !strings.Contains(strings.ToLower(out.Reply), "find") && !strings.Contains(strings.ToLower(out.Reply), "match") {
				t.Errorf("reply did not explain the miss: %s", out.Reply)
			}
			t.Logf("RESULT model=%s searches=%d cost=$%.6f reply=%s", agent.Model(), searches, agent.Usage().CostUSD, out.Reply)
			// The journal is the full record if the event display clipped an excerpt.
			if _, err := os.Stat(place.Transcript()); err != nil {
				t.Fatal(err)
			}
			_ = agent.Close()
		})
	}
}

// StartTask is the public handoff used by the task surface. This catches a
// missing inherited reader that a foreground chat experiment cannot observe.
func TestConversationSearchInsideLiveTask(t *testing.T) {
	w := newManualWorld(t)
	brain, err := store.Open(filepath.Join(w.home, "task-history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	source, err := brain.PostMessage(store.Message{SessionID: "earlier-other-project", Role: store.RoleUser, Body: "For the copper rollout the final approval phrase is heron-lilac-731."})
	if err != nil {
		t.Fatal(err)
	}
	ground := aPlainWorkspace(t)
	agent, place := w.open(ground, func(cfg *session.Config) {
		familyConfig(w)(cfg)
		manualConfig(cfg)
		cfg.Memory = brain
		cfg.Divide = false
		cfg.TaskAudit = true
	})
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	started := time.Now()
	id, _, _, err := agent.StartTask(ctx, "Look up our earlier copper rollout decision. In your final answer, quote the approval phrase verbatim, identify the stored speaker role, and cite the conversation ID and its opaque source reference. Read the original conversation as evidence; do not guess.")
	if err != nil {
		t.Fatal(err)
	}
	run := &familyRun{t: t, w: w, agent: agent, place: place, ground: ground, root: id}
	if !run.waitForRest(ctx) {
		t.Fatal("task did not finish")
	}
	files, err := filepath.Glob(filepath.Join(place.Dir, "tasks", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	searches := 0
	checkerSearches := 0
	receipt := false
	answered := false
	var journals strings.Builder
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		journals.Write(raw)
		for _, line := range strings.Split(string(raw), "\n") {
			var row journalLine
			if json.Unmarshal([]byte(line), &row) != nil {
				continue
			}
			if !strings.Contains(filepath.Base(file), "-audit-") && row.Role == "assistant" && len(row.ToolCalls) == 0 && strings.Contains(row.Content, "heron-lilac-731") && strings.Contains(row.Content, "user") && strings.Contains(row.Content, source.SessionID) && strings.Contains(row.Content, session.ConversationReference(source.SessionID, source.Seq)) {
				answered = true
			}
			for _, call := range row.ToolCalls {
				if call.Function.Name == "search_conversations" {
					if strings.Contains(filepath.Base(file), "-audit-") {
						checkerSearches++
						t.Logf("CHECKER SEARCH %s", call.Function.Arguments)
					} else {
						searches++
						t.Logf("WORKER SEARCH %s", call.Function.Arguments)
					}
				}
			}
			if row.Role == "tool" && strings.Contains(row.Content, "heron-lilac-731") && strings.Contains(row.Content, "message ") {
				receipt = true
			}
		}
	}
	if searches < 1 || !receipt {
		t.Fatalf("worker lookup was absent: worker=%d checker=%d receipt=%v\nJOURNALS\n%s", searches, checkerSearches, receipt, journals.String())
	}
	if !answered {
		t.Fatalf("worker did not deliver the exact approval phrase, stored role and source citation\nJOURNALS\n%s", journals.String())
	}
	usd, models := ledgerSince(t, started)
	for _, model := range models {
		if model != e2eModel {
			t.Errorf("unexpected model: %s", model)
		}
	}
	t.Logf("RESULT actual task=%d worker_searches=%d checker_searches=%d cost=$%.6f models=%v", id, searches, checkerSearches, usd, models)
	// The task may investigate source attribution before writing. Measure
	// those calls rather than treating a speed target as a correctness gate.
	if searches > 3 {
		t.Logf("EFFICIENCY: worker made %d history lookups; the answer remains grounded", searches)
	}
	rows := readTaskRows(t, place.Tasks())
	for _, row := range rows {
		if row.ID == id && row.State != string(session.TaskDone) {
			checkpoint, _ := os.ReadFile(place.Tasks())
			t.Errorf("task did not pass its completion check: state=%s report=%s\nCHECKPOINT\n%s\nJOURNALS\n%s", row.State, row.Report, checkpoint, journals.String())
		}
	}
	_ = agent.Close()
}

// Receipt assertions read the full journal rather than Event.Output, whose
// display copy can cut a long opened message before the following correction.
func conversationReceiptCalls(t *testing.T, path string) []call {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var calls []call
	indexes := map[string]int{}
	results := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		var row journalLine
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		if row.Role == "tool" {
			results[row.ToolCallID] = row.Content
		}
		for _, one := range row.ToolCalls {
			indexes[one.ID] = len(calls)
			calls = append(calls, call{Name: one.Function.Name, Args: one.Function.Arguments})
		}
	}
	for id, i := range indexes {
		calls[i].Output = results[id]
	}
	return calls
}
