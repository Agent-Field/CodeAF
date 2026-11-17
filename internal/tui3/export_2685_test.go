package tui3

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// TestExportAfterCompactKeepsAllJournalTurns2685 verifies that /export after
// /compact includes every turn from the journal (turns 1..3 before compaction
// as well as the post-compaction turn), that an uncompacted conversation
// exports byte-for-byte identically, and that the default file name matches
// the manual's transcript-<session name>.md form.
func TestExportAfterCompactKeepsAllJournalTurns2685(t *testing.T) {
	t.Run("compacted_journal_exports_all_turns", func(t *testing.T) {
		longReply := strings.Repeat("detailed analysis of the parser state machine and token stream. ", 80)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "You are compacting") || !strings.Contains(string(body), `"stream":true`) {
				summary := "Earlier turns covered questions 1 through 3 about the parser state machine."
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":`+strconv.Quote(summary)+`}}]}`)
				return
			}
			text := longReply
			if strings.Contains(string(body), "user turn 7 after compact") {
				text = "final answer after compact"
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":`+strconv.Quote(text)+`},"finish_reason":"stop"}]}`+"\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		}))
		defer server.Close()

		dir := t.TempDir()
		sessionDir := filepath.Join(dir, "0123456789abcdef")
		if err := os.MkdirAll(sessionDir, 0o700); err != nil {
			t.Fatal(err)
		}
		journalPath := filepath.Join(sessionDir, session.TranscriptName)

		agent, err := session.New(session.Config{
			Workspace:      dir,
			SessionFile:    journalPath,
			Model:          "vendor/m",
			APIKey:         "test",
			BaseURL:        server.URL,
			System:         "SYSTEM",
			ContextWindow:  65_536,
			CompactEnabled: false,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = agent.Close() })

		padding := strings.Repeat("pasted parser log line ", 140)
		var userTurns []string
		for i := 1; i <= 6; i++ {
			marker := fmt.Sprintf("user turn %d before compact", i)
			userTurns = append(userTurns, marker)
			events, err := agent.Submit(t.Context(), marker+" — "+padding)
			if err != nil {
				t.Fatalf("submit turn %d: %v", i, err)
			}
			for range events {
			}
		}

		if err := agent.Compact(t.Context()); err != nil {
			t.Fatalf("compact: %v", err)
		}
		if liveOnly := exportDocument("Port The Resume Picker", agent.Transcript()); strings.Contains(liveOnly, "user turn 1 before compact") {
			t.Fatalf("live transcript after compact still held turn 1; compaction did not fold earlier turns:\n%s", liveOnly)
		}

		lastPrompt := "user turn 7 after compact"
		userTurns = append(userTurns, lastPrompt)
		events, err := agent.Submit(t.Context(), lastPrompt)
		if err != nil {
			t.Fatalf("submit turn 7: %v", err)
		}
		for range events {
		}

		// Direct exportDocument fed the journal of the compacted conversation
		// must contain every user line the journal holds.
		rec := session.ReadTranscript(journalPath)
		doc := exportDocument("Port The Resume Picker", exportRecord(rec))
		for _, want := range userTurns {
			if !strings.Contains(doc, want) {
				t.Fatalf("exportDocument from compacted journal is missing %q:\n%s", want, doc)
			}
		}

		// End-to-end /export on the app writes transcript-<session name>.md
		// with turns 1..3 and turn 7 present.
		outDir := t.TempDir()
		a := newApp(t.Context(), Options{
			Agent:       agent,
			Workspace:   outDir,
			SessionFile: journalPath,
			Resumed:     true,
			ProfileDir:  t.TempDir(),
		})
		a.width, a.height = 160, 20
		a.pal = newPalette(tokens.ANSI256, false)
		a.welcome = welcome{spent: true}
		a.title = "port the resume picker"
		a.touch()

		typeLine(t, a, "/export")
		wantFile := filepath.Join(outDir, "transcript-port-the-resume-picker.md")
		raw, err := os.ReadFile(wantFile)
		if err != nil {
			t.Fatalf("reading exported file %s: %v", wantFile, err)
		}
		exported := string(raw)
		for _, want := range userTurns {
			if !strings.Contains(exported, want) {
				t.Fatalf("/export file is missing %q:\n%s", want, exported)
			}
		}
	})

	t.Run("uncompacted_conversation_exports_byte_for_byte_identically", func(t *testing.T) {
		sample := exportSample()
		want := exportDocument("Port The Resume Picker", sample)
		got := exportDocument("Port The Resume Picker", exportEntries(nil, sample, 0))
		if got != want {
			t.Fatalf("uncompacted export changed:\ngot:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("default_file_name_matches_manual", func(t *testing.T) {
		file := filepath.Join("/state/v3/projects/repo/0123456789abcdef", session.TranscriptName)
		got := exportFileName(file, "port the resume picker")
		const want = "transcript-port-the-resume-picker.md"
		if got != want {
			t.Fatalf("exportFileName(%q, %q) = %q, want %q", file, "port the resume picker", got, want)
		}

		raw, err := os.ReadFile(filepath.Join("..", "manual", "chat", "commands.md"))
		if err != nil {
			t.Fatal(err)
		}
		manual := string(raw)
		if !strings.Contains(manual, "transcript-<session name>.md") || !strings.Contains(manual, want) {
			t.Fatalf("commands.md does not document %q / transcript-<session name>.md", want)
		}
		if strings.Contains(manual, "20260817-150405_a3f2") {
			t.Fatal("commands.md still names the obsolete 20260817-150405_a3f2 stem")
		}
	})
}
