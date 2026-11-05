package session

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
)

type libraryAccountingTransport func(*http.Request) (*http.Response, error)

func (f libraryAccountingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLibraryAccountingOwnedVaultWritesLedgerOnce(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprintf("failed=%t", failed), func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			profile := t.TempDir()
			row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: profile}).Row(config.KeyKnowledgeModel)
			if !ok {
				t.Fatal("knowledge model setting missing")
			}
			if err := row.Apply("deepseek/deepseek-v4.1-flash-1017"); err != nil {
				t.Fatal(err)
			}
			ledger := filepath.Join(t.TempDir(), UsageLedgerName)
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
				c.ProfileDir = profile
				c.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
				c.usageLedger = ledger
			})
			v := agent.Knowledge()
			if v == nil {
				t.Fatal("persisted conversation has no public vault")
			}
			if err := v.StartBuild(); err != nil {
				t.Fatal(err)
			}
			if err := v.Capture(knowledge.Source{ID: "s", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar queues jobs."}); err != nil {
				t.Fatal(err)
			}
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			calls := 0
			http.DefaultTransport = libraryAccountingTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				var request struct{ Messages []struct{ Content string } }
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					return nil, err
				}
				var payload map[string]any
				if err := json.Unmarshal([]byte(request.Messages[1].Content), &payload); err != nil {
					return nil, err
				}
				content := `{"subjects":[]}`
				if payload["messages"] != nil {
					content = `{"claims":[{"local_id":"c","text":"Cedar queues jobs.","scope":"Cedar","topic":"Queues","kind":"fact","evidence":[{"source":"s","quote":"Cedar queues jobs."}]}]}`
				}
				if payload["question"] != nil {
					claim := payload["claims"].([]any)[0].(map[string]any)
					result, _ := json.Marshal(map[string]any{"answer": claim["text"], "citations": []any{claim["id"]}})
					content = string(result)
				}
				finish := "stop"
				if failed {
					finish, content = "length", "{"
				}
				raw := fmt.Sprintf(`{"model":"deepseek/deepseek-v4.1-flash-1017","choices":[{"finish_reason":%q,"message":{"content":%q}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"cost":0.125}}`, finish, content)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}, nil
			})
			err := v.Flush(knowledge.WithOwnerSessions(context.Background(), []string{"chat"}), "fixture", true)
			if failed && (err == nil || !strings.Contains(err.Error(), "length")) {
				t.Fatalf("paid failure: %v", err)
			}
			if !failed && err != nil {
				t.Fatal(err)
			}
			FlushUsage()
			lines, err := ReadUsage(ledger, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if calls == 0 || len(lines) != calls {
				t.Fatalf("paid calls=%d ledger rows=%d", calls, len(lines))
			}
			line := lines[0]
			if line.LibraryActivity != "learning" || line.Role != "memory" || line.USD != .125 || line.Calls != 1 || line.Input != 10 || line.Output != 2 || line.Session == "" {
				t.Fatalf("owned receipt: %+v", line)
			}
			if used := agent.Usage(); used.CostUSD != .125*float64(calls) || used.Calls != calls {
				t.Fatalf("agent must count bill once: %+v", used)
			}
			if !failed {
				before := calls
				answer, err := v.RecallSaved(context.Background(), "fixture", "What does Cedar queue?", "Cedar")
				if err != nil || len(answer.Citations) != 1 {
					t.Fatalf("public owned recall: %+v %v", answer, err)
				}
				FlushUsage()
				lines, err = ReadUsage(ledger, time.Time{})
				if err != nil {
					t.Fatal(err)
				}
				if calls != before+1 || len(lines) != calls {
					t.Fatalf("owned recall lost its ledger row: paid calls=%d owned rows=%d", calls, len(lines))
				}
				if lines[len(lines)-1].LibraryActivity != "answers" {
					t.Fatalf("owned recall attribution: %+v", lines[len(lines)-1])
				}
			}
			t.Logf("public vault: paid calls=%d owned rows=%d", calls, len(lines))
		})
	}
}
