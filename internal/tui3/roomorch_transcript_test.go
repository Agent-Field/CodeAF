package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
)

func TestRunNodeTranscriptDoorOpensAndEscReturns(t *testing.T) {
	snap := orchRun4()
	snap.Nodes[1].State = orchestrate.Done
	a, agent := orchApp(t, snap)
	path := filepath.Join(t.TempDir(), "rfcs.jsonl")
	journal := "{\"type\":\"message\",\"role\":\"user\",\"content\":\"read the retry RFCs\"}\n" +
		"{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"RFC 7231 answers it.\"}\n"
	if err := os.WriteFile(path, []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	agent.journals = map[string]string{"r1:rfcs": path}
	a.orchCardOpen("rfcs")
	if got := roomText(a); !strings.Contains(got, "transcript · enter opens it") {
		t.Fatalf("node card has no transcript door:\n%s", got)
	}
	a.orchOf().link = len(a.orchCardLinks()) - 1
	drive(t, a, key("enter"))
	if got := roomText(a); !strings.Contains(got, "read the retry RFCs") || !strings.Contains(got, "RFC 7231 answers it") {
		t.Fatalf("journal did not render as a conversation:\n%s", got)
	}
	drive(t, a, key("esc"))
	if got := roomText(a); !strings.Contains(got, "transcript · enter opens it") {
		t.Fatalf("esc did not return to the node card:\n%s", got)
	}
}

func TestLiveRunNodeTranscriptFollowsTheJournal(t *testing.T) {
	a, agent := orchApp(t, orchRun4())
	path := filepath.Join(t.TempDir(), "rfcs.jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"message\",\"role\":\"user\",\"content\":\"start here\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent.journals = map[string]string{"r1:rfcs": path}
	a.orchCardOpen("rfcs")
	a.orchOf().link = len(a.orchCardLinks()) - 1
	drive(t, a, key("enter"))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"new live line\"}\n")
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	orchPollNow(t, a)
	if got := roomText(a); !strings.Contains(got, "new live line") {
		t.Fatalf("live journal did not follow the poll:\n%s", got)
	}
}

func TestRunNodeWithoutJournalSaysSo(t *testing.T) {
	a, _ := orchApp(t, orchRun4())
	a.orchCardOpen("client")
	a.orchOf().link = len(a.orchCardLinks()) - 1
	drive(t, a, key("enter"))
	if got := roomText(a); !strings.Contains(got, "no transcript yet") || strings.Contains(strings.ToLower(got), "error") {
		t.Fatalf("missing journal was not an honest empty page:\n%s", got)
	}
}
