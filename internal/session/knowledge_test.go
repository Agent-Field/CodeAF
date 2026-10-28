package session

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/knowledgefixture"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
)

// The saved-turn seam: the same vault opened by Memory receives durable chat
// turns only after creation. No live model is needed for local capture.
func TestKnowledgeCapturesSavedTurnsOnlyAfterOptIn(t *testing.T) {
	profile := t.TempDir()
	journal := filepath.Join(t.TempDir(), "chat.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("Before creation"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("The architecture uses a durable queue."), nil
		},
	}}, func(c *Config) {
		c.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")
		c.ProfileDir = profile
		c.SessionFile = journal
	})
	vault := agent.Knowledge()
	if vault == nil {
		t.Fatal("persisted conversation has no knowledge vault")
	}
	collect(t, mustSubmit(t, agent, "Before creation"))
	snapshot, err := vault.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Enabled || snapshot.Pending != 0 {
		t.Fatalf("capture before opt-in: %+v", snapshot)
	}
	if err := vault.StartBuild(); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "Explain our durable queue architecture"))
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err = vault.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Pending == 2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("wanted two completed sources, got %+v", snapshot)
}

func TestKnowledgeUsesDefaultProfileForPersistedChats(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")
		c.SessionFile = filepath.Join(t.TempDir(), "chat.jsonl")
	})
	if agent.Knowledge() == nil {
		t.Fatal("default-profile conversation cannot open a knowledge base")
	}
}

// Recovery is observed through reopening the real journal and browsing the
// vault. The fixture represents compaction replay and a harness-authored note.
func TestKnowledgeRecoversJournalWithoutReplayingCompactionOrNotes(t *testing.T) {
	profile := t.TempDir()
	journal := filepath.Join(t.TempDir(), "chat.jsonl")
	agent, workspace := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("The durable journal is the recovery floor."), nil
		},
	}}, func(c *Config) {
		c.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")
		c.ProfileDir = profile
		c.SessionFile = journal
	})
	vault := agent.Knowledge()
	if err := vault.StartBuild(); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "Explain journal recovery"))
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := vault.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Pending != 2 {
		t.Fatalf("close did not preserve two sources: %+v", snapshot)
	}
	f, err := os.OpenFile(journal, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, entry := range []map[string]any{
		{"type": "message", "role": "user", "content": "Harness note is not a human claim", "note": true, "timestamp": now},
		{"type": "compaction", "window": 2, "timestamp": now},
		{"type": "message", "role": "user", "content": "Replayed synthetic summary", "timestamp": now},
		{"type": "message", "role": "assistant", "content": "Replayed synthetic answer", "timestamp": now},
		{"type": "message", "role": "user", "content": "The recovered source must survive", "timestamp": now},
	} {
		if err := json.NewEncoder(f).Encode(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	resumed, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")
		c.ProfileDir = profile
		c.SessionFile = journal
		c.Workspace = workspace
	})
	// Closing drains recovery as well as live capture, without waiting for a model.
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err = vault.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Pending != 3 {
		t.Fatalf("recovery duplicated or captured harness prose: %+v", snapshot)
	}
}

func TestKnowledgeRecallToolsReturnCitedProjectPagesToTheConversation(t *testing.T) {
	profile := t.TempDir()
	journal := filepath.Join(t.TempDir(), "chat.jsonl")
	client := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("lookup", "search_knowledge", `{"query":"queues"}`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			found := false
			for _, m := range messages {
				if m.Role == "tool" && strings.Contains(messageContentText(m), "routing") {
					found = true
				}
			}
			if !found {
				t.Error("search tool did not return the project page")
			}
			return toolResponse("read", "read_knowledge", `{"id":"routing"}`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			found := false
			for _, m := range messages {
				if m.Role == "tool" && strings.Contains(messageContentText(m), `"source":"s1"`) && strings.Contains(messageContentText(m), "Cedar queues jobs.") {
					found = true
				}
			}
			if !found {
				t.Error("read tool lost original-source citations")
			}
			return textResponse("Cedar queues jobs. [routing, source s1]"), nil
		},
	}}
	agent, _ := newTestAgent(t, client, func(c *Config) {
		c.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")
		c.ProfileDir = profile
		c.SessionFile = journal
	})
	vault := agent.Knowledge()
	if err := vault.StartBuild(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(vault.Directory(), "knowledge")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := `{"sources":{"s1":{"role":"user","ts":"2026-10-25T12:00:00Z","text":"Cedar queues jobs."}},"claims":[{"id":"c1","text":"Cedar queues jobs.","kind":"fact","status":"active","evidence":[{"source":"s1","quote":"Cedar queues jobs."}]}],"notes":[{"id":"routing","title":"Routing","claims":["c1"]}]}`
	if err := knowledgefixture.Write(filepath.Join(filepath.Dir(dir), "knowledge.sqlite"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "How are Cedar jobs queued?"))
}

func TestKnowledgeRecallIsBoundedAndLongPagesCanBeContinued(t *testing.T) {
	var next string
	client := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("search", "search_knowledge", `{"query":"Cedar"}`), nil
		},
		func(_ context.Context, msg []ai.Message) (*ai.Response, error) {
			for _, m := range msg {
				if m.Role == "tool" {
					body := messageContentText(m)
					if len(body) > 12000 {
						t.Errorf("search returned %d bytes", len(body))
					}
					if strings.Contains(body, `"sections"`) {
						t.Error("search serialized entire pages")
					}
				}
			}
			return toolResponse("read1", "read_knowledge", `{"id":"cedar"}`), nil
		},
		func(_ context.Context, msg []ai.Message) (*ai.Response, error) {
			for _, m := range msg {
				if m.Role == "tool" && m.ToolCallID == "read1" {
					body := messageContentText(m)
					if len(body) > 12000 {
						t.Errorf("read returned %d bytes", len(body))
					}
					var r struct {
						Truncated bool   `json:"truncated"`
						Next      string `json:"next_cursor"`
					}
					if e := json.Unmarshal([]byte(body), &r); e != nil {
						t.Fatal(e)
					}
					if !r.Truncated || r.Next == "" {
						t.Error("large page cannot be continued")
					}
					next = r.Next
				}
			}
			args, _ := json.Marshal(map[string]string{"id": "cedar", "cursor": next})
			return toolResponse("read2", "read_knowledge", string(args)), nil
		},
		func(_ context.Context, msg []ai.Message) (*ai.Response, error) {
			for _, m := range msg {
				if m.Role == "tool" && m.ToolCallID == "read2" {
					body := messageContentText(m)
					if !strings.Contains(body, `"source":"s1"`) {
						t.Error("continued prose lacks source citation")
					}
					if strings.Contains(body, "Invalid arguments") {
						t.Error(body)
					}
				}
			}
			return textResponse("Cedar is sourced."), nil
		},
	}}
	agent, _ := newTestAgent(t, client, func(c *Config) {
		c.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")
		c.ProfileDir = t.TempDir()
		c.SessionFile = filepath.Join(t.TempDir(), "chat.jsonl")
	})
	vault := agent.Knowledge()
	if e := vault.StartBuild(); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(vault.Directory(), "knowledge")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	fixture := map[string]any{"sources": map[string]any{"s1": map[string]any{"role": "user", "text": "Cedar indexes documents."}}, "claims": []any{map[string]any{"id": "c1", "text": "Cedar indexes documents.", "status": "active", "evidence": []any{map[string]any{"source": "s1", "quote": "Cedar indexes documents."}}}}, "notes": []any{map[string]any{"id": "cedar", "title": "Cedar", "claims": []string{"c1"}, "sections": []any{map[string]any{"heading": "Architecture", "paragraphs": []any{map[string]any{"text": strings.Repeat("Cedar indexes documents. ", 3000), "claims": []string{"c1"}}}}}}}}
	b, e := json.Marshal(fixture)
	if e != nil {
		t.Fatal(e)
	}
	if e = knowledgefixture.Write(filepath.Join(filepath.Dir(dir), "knowledge.sqlite"), b, 0600); e != nil {
		t.Fatal(e)
	}
	collect(t, mustSubmit(t, agent, "Explain Cedar from the knowledge base"))
}

func TestKnowledgeRemainsSharedAcrossWorkspaceAnchor(t *testing.T) {
	profile := t.TempDir()
	scratch := t.TempDir()
	project := t.TempDir()
	journal := filepath.Join(t.TempDir(), ".cell", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(journal), 0700); err != nil {
		t.Fatal(err)
	}
	target := knowledge.Open(profile)
	if err := target.StartBuild(); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("Scratch response"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("Project response"), nil
		},
	}}, func(c *Config) {
		c.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")

		c.ProfileDir = profile
		c.SessionFile = journal
		c.Workspace = scratch
		c.Place = Place{Owned: true, Workspace: scratch}
	})
	initial := agent.Knowledge()
	if err := initial.StartBuild(); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "Scratch discussion must stay in scratch"))
	if _, err := agent.AnchorWorkspace(project); err != nil {
		t.Fatal(err)
	}
	if agent.Knowledge().Directory() != target.Directory() {
		t.Fatal("knowledge stayed in scratch after workspace anchor")
	}
	collect(t, mustSubmit(t, agent, "Project discussion must stay in project"))
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := target.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Pending != 4 {
		t.Fatalf("shared vault missed cross-project conversation: %+v", snapshot)
	}
	resumed, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")
		c.ProfileDir = profile
		c.SessionFile = journal
		c.Workspace = project
	})
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err = target.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Pending != 4 {
		t.Fatalf("reopen duplicated cross-project conversation: %+v", snapshot)
	}
}

func TestKnowledgeCapturesCellJournalWithSealedWorkspaceHeader(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	if err := os.MkdirAll(filepath.Join(dir, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	place := Place{Dir: dir, Owned: true}
	place.Workspace = place.Work()
	if err := os.MkdirAll(place.Work(), 0700); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("Cell answer"), nil }}}, func(c *Config) {
		c.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")

		c.ProfileDir = t.TempDir()
		c.Workspace = place.Work()
		c.Place = place
		c.SessionFile = place.Transcript()
	})
	raw, err := os.ReadFile(place.Transcript())
	if err != nil {
		t.Fatal(err)
	}
	var header struct {
		Cwd string `json:"cwd"`
	}
	if err = json.Unmarshal([]byte(strings.Split(string(raw), "\n")[0]), &header); err != nil {
		t.Fatal(err)
	}
	if header.Cwd != "" {
		t.Fatal("fixture must use sealed cell header")
	}
	vault := agent.Knowledge()
	if err := vault.StartBuild(); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "Cell conversation"))
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := vault.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Pending != 2 {
		t.Fatalf("sealed cell journal did not capture: %+v", snapshot)
	}
}

func TestKnowledgeSavedTurnsUseThePersistedCompilerModel(t *testing.T) {
	profile := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("Durable queues retain work."), nil
		},
	}}, func(c *Config) {
		c.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")
		c.ProfileDir = profile
		c.SessionFile = filepath.Join(t.TempDir(), "chat.jsonl")
	})
	registry := config.NewSettings(config.SettingsOptions{ProfileDir: profile})
	row, ok := registry.Row("knowledge.model")
	if !ok {
		t.Fatal("Settings has no knowledge model choice")
	}
	if err := row.Apply("example/knowledge-model"); err != nil {
		t.Fatal(err)
	}
	vault := agent.Knowledge()
	if err := vault.StartBuild(); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "Explain durable queues"))
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := knowledge.Open(profile)
	status, err := reopened.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Pending != 2 || status.Model != "example/knowledge-model" {
		t.Fatalf("saved chat did not use persisted knowledge model: %+v", status)
	}
	if err := row.Apply("example/updated-model"); err != nil {
		t.Fatal(err)
	}
	status, err = reopened.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Model != "example/updated-model" {
		t.Fatalf("existing vault kept stale model: %q", status.Model)
	}
}

func TestKnowledgeRecallLabelsHumanEditsWithoutOldCitations(t *testing.T) {
	profile := t.TempDir()
	client := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("human-read", "read_knowledge", `{"id":"routing"}`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			found := false
			for _, message := range messages {
				if message.Role != "tool" {
					continue
				}
				text := messageContentText(message)
				if strings.Contains(text, "Human corrected routing") {
					found = true
					if !strings.Contains(text, `"human_edited":true`) || strings.Contains(text, `"source":"s1"`) {
						t.Error("manual prose was presented as verified by old evidence: " + text)
					}
				}
			}
			if !found {
				t.Error("human correction was absent from agent recall")
			}
			return textResponse("The human-edited note describes corrected routing."), nil
		},
	}}
	agent, _ := newTestAgent(t, client, func(c *Config) {
		c.ProfileDir = profile
		c.SessionFile = filepath.Join(t.TempDir(), "chat.jsonl")
		c.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")
	})
	vault := agent.Knowledge()
	if err := vault.StartBuild(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(vault.Directory(), "knowledge")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := `{"sources":{"s1":{"role":"user","ts":"2026-10-25T12:00:00Z"}},"claims":[{"id":"c1","kind":"fact","text":"Old routing","status":"active","evidence":[{"source":"s1","quote":"Old routing"}]}],"notes":[{"id":"routing","title":"Routing","claims":["c1"]}]}`
	if err := knowledgefixture.Write(filepath.Join(filepath.Dir(dir), "knowledge.sqlite"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := vault.Read("routing")
	if err != nil {
		t.Fatal(err)
	}
	if err = vault.EditPage(page.ID, page.Revision, "Human corrected routing"); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "Read the corrected routing note"))
}
