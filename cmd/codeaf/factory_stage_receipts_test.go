package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageCommandReceiptsPreserveCwdAndExcludeSummaryPASS(t *testing.T) {
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	journal := `{"role":"assistant","toolCalls":[{"id":"t1","function":{"name":"bash","arguments":"{\"command\":\"cd nested && GOPROXY=direct go test -race -v ./internal/retrieval\"}"}}]}
{"role":"tool","toolCallId":"t1","content":"--- PASS: TestA (0.00s)\n    --- PASS: TestA/sub (0.00s)\n--- PASS: TestB (0.00s)\nPASS\nok  example 1s\n"}
`
	if err := os.WriteFile(file, []byte(journal), 0600); err != nil {
		t.Fatal(err)
	}
	c := stageConversation{file: file, dir: "/checkout"}
	notes, counts := c.ExecutionNotes()
	if len(notes) != 1 || !strings.Contains(notes[0], "GOPROXY=direct") || !strings.Contains(notes[0], `"cwd":"/checkout"`) || counts == nil || counts.Passed != 3 {
		t.Fatalf("receipt: %v %+v", notes, counts)
	}
}

func TestStageReceiptsKeepLargestCommandAndResolveRerunFailures(t *testing.T) {
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	journal := `{"role":"assistant","toolCalls":[{"id":"a","function":{"name":"bash","arguments":"{\"command\":\"go test -v ./...\"}"}}]}
{"role":"tool","toolCallId":"a","content":"--- PASS: TestA (0s)\n--- PASS: TestB (0s)\n--- PASS: TestC (0s)\nPASS\nok  example 1s\n"}
{"role":"assistant","toolCalls":[{"id":"b","function":{"name":"bash","arguments":"{\"command\":\"go test -v -run TestA ./...\"}"}}]}
{"role":"tool","toolCallId":"b","content":"--- FAIL: TestA (0s)\nFAIL\nCommand exited with code 1"}
{"role":"assistant","toolCalls":[{"id":"c","function":{"name":"bash","arguments":"{\"command\":\"go test -v -run TestA ./...\"}"}}]}
{"role":"tool","toolCallId":"c","content":"--- PASS: TestA (0s)\nPASS\nok  example 1s\n"}
`
	if err := os.WriteFile(file, []byte(journal), 0600); err != nil {
		t.Fatal(err)
	}
	c := stageConversation{file: file, dir: "/checkout"}
	notes, counts := c.ExecutionNotes()
	if counts == nil || counts.Passed != 3 || counts.Command != "go test -v ./..." {
		t.Fatalf("a later subset replaced full command: %+v", counts)
	}
	for _, n := range notes {
		if strings.HasPrefix(n, "Unresolved verification") {
			t.Fatal("successful rerun did not resolve failure", notes)
		}
	}
}
