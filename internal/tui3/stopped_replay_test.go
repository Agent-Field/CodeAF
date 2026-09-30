package tui3

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A real stopped session writes the partial answer before the surface reopens
// it, so this test crosses the same journal boundary as leaving for Home.
func TestAStoppedPlainAnswerReopensBehindAReadableDisclosure(t *testing.T) {
	for _, tc := range []struct {
		name, streamed, visible string
		tool                    bool
	}{
		{"plain answer", "The river bends past the old bridge.", "The river bends past the old bridge.", false},
		{"explicit update", "[update] The first file is ready.", "The first file is ready.", false},
		{"forming tool call", "I am checking the river map.", "I am checking the river map.", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":`+strconv.Quote(tc.streamed)+`}}]}`+"\n\n")
				w.(http.Flusher).Flush()
				if tc.tool {
					_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":"}}]}}]}`+"\n\n")
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			defer server.Close()

			dir := t.TempDir()
			path := filepath.Join(dir, "session.jsonl")
			agent, err := session.New(session.Config{Workspace: dir, SessionFile: path, Model: "vendor/m", APIKey: "test", BaseURL: server.URL, System: "SYSTEM"})
			if err != nil {
				t.Fatal(err)
			}
			events, err := agent.Submit(t.Context(), "Tell me about the river")
			if err != nil {
				t.Fatal(err)
			}
			gotText, gotTool := false, !tc.tool
			for !gotText || !gotTool {
				select {
				case ev, ok := <-events:
					if !ok {
						t.Fatal("stream ended before the person stopped it")
					}
					gotText = gotText || ev.Kind == session.EventTextDelta && strings.Contains(ev.Text, tc.streamed)
					gotTool = gotTool || ev.Kind == session.EventToolForming
				case <-time.After(5 * time.Second):
					t.Fatal("stream did not reach the stop point")
				}
			}
			agent.Interrupt()
			for range events {
			}
			if err := agent.Close(); err != nil {
				t.Fatal(err)
			}

			record := session.ReadTranscript(path)
			if len(record.Entries) < 2 || !record.Entries[1].Interrupted || record.Entries[1].Answer {
				t.Fatalf("journal did not keep the interrupted response: %#v", record.Entries)
			}
			if tc.name == "plain answer" && (record.Entries[1].Role != "aside" || record.Entries[1].Addressed) {
				t.Fatalf("the unmarked partial changed audience: %#v", record.Entries[1])
			}
			if tc.tool {
				for _, e := range record.Entries {
					if e.Role == "tool" {
						t.Fatalf("a forming call that never ran entered the record: %#v", e)
					}
				}
			}

			reopened := resumedAgent(t, dir, path)
			a := newApp(t.Context(), Options{Agent: reopened, Workspace: dir, Resumed: true})
			a.width, a.height = 90, 30
			a.workMode = config.WorkFold
			a.touch()
			closed := strings.Join(plainRows(a), "\n")
			if tc.name == "explicit update" {
				if strings.Count(closed, tc.visible) != 1 || !strings.Contains(closed, "interrupted") {
					t.Fatalf("the addressed update changed on reopen:\n%s", closed)
				}
				return
			}
			if !strings.Contains(closed, "▸ stopped by you") || strings.Contains(closed, tc.visible) {
				t.Fatalf("the stopped response has no closed disclosure:\n%s", closed)
			}
			drive(t, a, key("ctrl+e"))
			opened := strings.Join(plainRows(a), "\n")
			if strings.Count(opened, tc.visible) != 1 || !strings.Contains(opened, "▾ stopped by you") {
				t.Fatalf("opening the stopped turn did not reveal its words once:\n%s", opened)
			}
			if line := rowWithText(t, a, tc.visible); !strings.HasPrefix(plain(line.text), "  ") || !strings.Contains(line.text, sgrOf(a.pal.narr)) {
				t.Fatalf("the stopped words were promoted to an answer: %q", line.text)
			}
			for _, e := range a.entries {
				if e.kind == entryAssistant && confirmedAnswer(&e) {
					t.Fatal("the stopped turn reopened as a completed answer")
				}
			}
		})
	}
}
