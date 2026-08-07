package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type attachmentCommander struct {
	*fakeCommander
	model     string
	supported bool
}

type artifactCommander struct {
	*fakeCommander
	target string
}

func (c *artifactCommander) ResolveMediaPath(_, _ string) (string, bool) {
	return c.target, true
}

func (c *attachmentCommander) ImageInputSupport() (string, bool) {
	return c.model, c.supported
}

func imageFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDetectImageAttachmentsPresentAbsentAndQuoted(t *testing.T) {
	path := imageFixture(t, "dragged image.png")
	draft := "compare this \"" + path + "\" please"
	cleaned, paths := detectImageAttachments(draft)
	if cleaned != "compare this please" || len(paths) != 1 || paths[0] != path {
		t.Fatalf("cleaned=%q paths=%v", cleaned, paths)
	}
	untouched := "mention missing.png but do not attach it"
	if cleaned, paths := detectImageAttachments(untouched); cleaned != untouched || len(paths) != 0 {
		t.Fatalf("absent path changed: %q %v", cleaned, paths)
	}
}

func TestVisionAttachmentChipRemovalAndSubmit(t *testing.T) {
	backend := &fakeBackend{}
	commander := &attachmentCommander{
		fakeCommander: &fakeCommander{current: map[string]string{"talk": "vision/model"}},
		model:         "vision/model", supported: true,
	}
	model := NewWithCommander(backend, "vision", commander)
	path := imageFixture(t, "look.png")
	model.input.SetValue("describe " + path)
	model.captureImageAttachments()
	if model.input.Value() != "describe" || len(model.attachments) != 1 {
		t.Fatalf("draft=%q attachments=%v", model.input.Value(), model.attachments)
	}
	if rendered := ansi.Strip(model.renderInput()); !strings.Contains(rendered, "⌾ look.png ⟨×⟩") {
		t.Fatalf("attachment chip missing: %q", rendered)
	}

	// Backspace on an empty draft removes the last chip.
	model.input.Reset()
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if len(model.attachments) != 0 {
		t.Fatal("backspace-on-empty did not remove the chip")
	}

	model.input.SetValue("describe " + path)
	model.captureImageAttachments()
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("vision attachment did not submit")
	}
	_ = command()
	backend.mu.Lock()
	defer backend.mu.Unlock()
	posted := backend.posted[len(backend.posted)-1]
	if posted.Body != "describe" || len(posted.Attachments) != 1 || posted.Attachments[0] != path {
		t.Fatalf("posted = %+v", posted)
	}
}

func TestNonVisionAttachmentShowsHintAndFallsBackToPlainPath(t *testing.T) {
	backend := &fakeBackend{}
	commander := &attachmentCommander{
		fakeCommander: &fakeCommander{current: map[string]string{"talk": "text/model"}},
		model:         "text/model", supported: false,
	}
	model := NewWithCommander(backend, "text", commander)
	path := imageFixture(t, "fallback.webp")
	model.input.SetValue("inspect " + path)
	model.captureImageAttachments()
	if rendered := ansi.Strip(model.renderInput()); !strings.Contains(rendered, "text/model can't see images — try a vision model") {
		t.Fatalf("fallback hint missing: %q", rendered)
	}
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("non-vision fallback did not submit")
	}
	_ = command()
	backend.mu.Lock()
	defer backend.mu.Unlock()
	posted := backend.posted[len(backend.posted)-1]
	if len(posted.Attachments) != 0 || !strings.Contains(posted.Body, path) {
		t.Fatalf("fallback posted = %+v", posted)
	}
}

func TestMediaArtifactLinksAreGlyphPrefixedAndWidthSafe(t *testing.T) {
	path := imageFixture(t, strings.Repeat("long-name-", 8)+"result.png")
	model := New(&fakeBackend{}, "media")
	for width := 8; width <= 32; width++ {
		rendered := model.renderMediaArtifacts(store.Message{Attachments: []string{path}}, width)
		if !strings.Contains(ansi.Strip(rendered), "⌾ ") || lipgloss.Width(rendered) > width {
			t.Fatalf("width %d rendered %d: %q", width, lipgloss.Width(rendered), rendered)
		}
		if markers := strings.Count(rendered, "\x1b]8;;"); markers != 2 {
			t.Fatalf("width %d has %d OSC8 markers: %q", width, markers, rendered)
		}
	}
	audio := artifactLink("/tmp/a.mp3", "media/a.mp3", "♪", 20)
	if !strings.Contains(ansi.Strip(audio), "♪ media/a.mp3") {
		t.Fatalf("audio link = %q", audio)
	}
	commander := &artifactCommander{fakeCommander: &fakeCommander{current: map[string]string{}}, target: path}
	relativeModel := NewWithCommander(&fakeBackend{}, "media", commander)
	relative := relativeModel.renderMediaArtifacts(store.Message{NodeID: "task", Body: "made media/result.png"}, 40)
	if !strings.Contains(relative, "file://") || !strings.Contains(ansi.Strip(relative), "⌾ media/result.png") {
		t.Fatalf("relative generated artifact was not linked: %q", relative)
	}
}
