package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/orchestrate"
)

func TestNestedDisclosureSurvivesSettledRefreshAndClosesOnLanding(t *testing.T) {
	for _, live := range []bool{false, true} {
		name := "already settled"
		if live {
			name = "running to settled"
		}
		t.Run(name, func(t *testing.T) {
			snap := orchRun4()
			if live {
				snap.Nodes[0].State = orchestrate.Running
			}
			a, agent := orchApp(t, snap)
			a.workMode = config.WorkFold
			path := filepath.Join(t.TempDir(), "plan.jsonl")
			journal := strings.Join([]string{
				`{"type":"message","role":"user","content":"read the file"}`,
				`{"type":"message","role":"assistant","content":"","toolCalls":[{"id":"c1","type":"function","function":{"name":"read","arguments":"{\"path\":\"notes.txt\"}"}}]}`,
				`{"type":"message","role":"tool","toolCallId":"c1","content":"the notes"}`,
				`{"type":"message","role":"assistant","content":"The result is ready."}`,
			}, "\n")
			if err := os.WriteFile(path, []byte(journal), 0600); err != nil {
				t.Fatal(err)
			}
			agent.journals = map[string]string{"r1:plan": path}
			a.orchOpenTranscript("plan")
			// Exercise the rendered mouse door, not only the disclosure map.
			key := -1
			for i, r := range a.roomRows(a.bodyWidth()) {
				if r.hit != hitWorkFold {
					continue
				}
				key = r.turn
				y := roomRowY(a, i)
				drive(t, a, tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft}, tea.MouseReleaseMsg{X: 4, Y: y, Button: tea.MouseLeft})
				break
			}
			if key < 0 {
				t.Fatalf("no disclosure: %s", roomText(a))
			}
			if !a.orchTranscriptDeck().workOpen[key] {
				t.Fatal("click did not open disclosure")
			}
			if live {
				changed := agent.snaps["r1"]
				changed.Nodes[0].State = orchestrate.Done
				agent.snaps["r1"] = changed
			}
			orchPollNow(t, a)
			if got := a.orchTranscriptDeck().workOpen[key]; got == live {
				t.Fatalf("after poll open=%v, want %v", got, !live)
			}
			if a.orchTranscriptDeck().workOpen[key] {
				a.toggleWorkfold(key)
			}
			rows := a.roomRows(a.bodyWidth())
			if a.roomUnfoldAtTop(len(rows), len(rows)) || a.orchTranscriptDeck().workOpen[key] {
				t.Fatal("scroll reopened settled nested transcript inside live parent")
			}
			// A different node must not inherit an old call's index-based state.
			for i := range a.orchOf().journal {
				if a.orchOf().journal[i].kind == entryTool {
					a.orchOf().journal[i].open = true
				}
			}
			agent.journals["r1:rfcs"] = path
			a.orchOpenTranscript("rfcs")
			for _, e := range a.orchOf().journal {
				if e.open || e.full {
					t.Fatal("another node inherited expanded content")
				}
			}
			if len(a.orchTranscriptDeck().workOpen) != 0 {
				t.Fatal("another node inherited an open disclosure")
			}
		})
	}
}
