package tui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func watchClipboard(t *testing.T) *string {
	t.Helper()
	copied := new(string)
	previous := clipboardSink
	clipboardSink = func(text string) { *copied = text }
	t.Cleanup(func() { clipboardSink = previous })
	return copied
}

func watchOpener(t *testing.T) *string {
	t.Helper()
	opened := new(string)
	previous := processOpener
	processOpener = func(target string) error {
		*opened = target
		return nil
	}
	t.Cleanup(func() { processOpener = previous })
	return opened
}

func deliveredModel(t *testing.T, workspaceFile string) *Model {
	t.Helper()
	body := "The audit found three gaps; the report is written.\n\nFiles:\n" + workspaceFile
	backend := &fakeBackend{messages: []store.Message{
		{Seq: 1, SessionID: "out", Role: store.RoleUser, Body: "audit the repo"},
		{Seq: 2, SessionID: "out", Role: store.RoleAgent, Body: body, NodeID: "job-1"},
	}}
	model := NewWithCommander(backend, "out", newFakeCommander())
	model.messages = backend.messages
	model.setSize(100, 30)
	return model
}

// The product's output is findings and file paths, and neither could leave the
// terminal: no clipboard anywhere, and the alt screen takes drag-select with
// it.
func TestCopyKeysTakeTheAnswerAndItsFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "03-security-review.md")
	if err := os.WriteFile(file, []byte("report"), 0o644); err != nil {
		t.Fatal(err)
	}
	model := deliveredModel(t, file)
	copied := watchClipboard(t)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	for model.inputFocused {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if !strings.Contains(*copied, "The audit found three gaps") || strings.Contains(*copied, "\x1b") {
		t.Fatalf("copied answer = %q", *copied)
	}
	if !strings.Contains(ansi.Strip(model.View()), "copied the answer") {
		t.Fatal("copying the answer said nothing")
	}

	*copied = ""
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Y")})
	if *copied != file {
		t.Fatalf("copied path = %q, want %q", *copied, file)
	}
	if model.status != "copied "+file {
		t.Fatalf("copying the path reported %q", model.status)
	}
}

// The copy keys are letters, so they obey the same law every other letter does.
func TestCopyKeysStayOutOfADraft(t *testing.T) {
	model := deliveredModel(t, filepath.Join(t.TempDir(), "report.md"))
	copied := watchClipboard(t)
	typeIntoModel(model, "yY")
	if model.input.Value() != "yY" {
		t.Fatalf("copy keys ate the draft: %q", model.input.Value())
	}
	if *copied != "" {
		t.Fatalf("typing copied %q", *copied)
	}
}

func TestOpenHandsTheDeliverableToThePlatform(t *testing.T) {
	file := filepath.Join(t.TempDir(), "launch note.md")
	if err := os.WriteFile(file, []byte("note"), 0o644); err != nil {
		t.Fatal(err)
	}
	model := deliveredModel(t, file)
	opened := watchOpener(t)

	_ = model.executeSlash("/open")
	if *opened != file {
		t.Fatalf("opened %q, want %q", *opened, file)
	}
	if !strings.Contains(ansi.Strip(model.View()), "opened launch note.md") {
		t.Fatalf("open said nothing:\n%s", ansi.Strip(model.View()))
	}

	// An answer with no file says so rather than opening something random.
	bare := NewWithCommander(&fakeBackend{}, "out", newFakeCommander())
	bare.messages = []store.Message{{Seq: 1, Role: store.RoleAgent, Body: "no files here"}}
	bare.setSize(100, 30)
	*opened = ""
	_ = bare.executeSlash("/open")
	if *opened != "" {
		t.Fatalf("opened %q from an answer with no file", *opened)
	}
	if !strings.Contains(ansi.Strip(bare.View()), "named no file") {
		t.Fatalf("empty open said nothing:\n%s", ansi.Strip(bare.View()))
	}
}

// OSC 52 is the route that survives ssh, which is why it is the primary one.
func TestClipboardSequenceIsOSC52Base64(t *testing.T) {
	sequence := osc52Clipboard("/tmp/report.md")
	if !strings.HasPrefix(sequence, "\x1b]52;c;") || !strings.HasSuffix(sequence, "\x07") {
		t.Fatalf("sequence = %q", sequence)
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b]52;c;"), "\x07")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || string(decoded) != "/tmp/report.md" {
		t.Fatalf("payload = %q err=%v", decoded, err)
	}
}

// Both new keys and the command are discoverable where the audit says a
// keyboard-only week looks for them.
func TestGettingWorkOutIsInHelp(t *testing.T) {
	model := NewWithCommander(&fakeBackend{}, "help", newFakeCommander())
	_ = model.executeSlash("/help")
	help := strings.Join(strings.Fields(ansi.Strip(strings.Join(model.helpContentLines(76), "\n"))), " ")
	for _, want := range []string{"y / Y", "copy the answer", "/open"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help omits %q", want)
		}
	}
}
