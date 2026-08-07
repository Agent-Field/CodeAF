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

// A dragged PDF is an attachment the way an image is, but it never becomes a
// model content part: it is staged as a workspace input for compiled work, so
// a text-only talk model is not a reason to degrade it back into the draft.
func TestDocumentAttachmentBecomesADocChipAndSurvivesATextOnlyModel(t *testing.T) {
	backend := &fakeBackend{}
	commander := &attachmentCommander{
		fakeCommander: &fakeCommander{current: map[string]string{"talk": "text/model"}},
		model:         "text/model", supported: false,
	}
	model := NewWithCommander(backend, "docs", commander)
	path := imageFixture(t, "quarterly filing.pdf")
	model.input.SetValue("summarise \"" + path + "\"")
	model.captureImageAttachments()
	if model.input.Value() != "summarise" || len(model.attachments) != 1 || model.attachments[0] != path {
		t.Fatalf("draft=%q attachments=%v", model.input.Value(), model.attachments)
	}
	rendered := ansi.Strip(model.renderInput())
	if !strings.Contains(rendered, "▤ quarterly filing.pdf ⟨×⟩") {
		t.Fatalf("document chip missing: %q", rendered)
	}
	if strings.Contains(rendered, "can't see images") {
		t.Fatalf("a document chip raised the vision hint: %q", rendered)
	}

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("document attachment did not submit")
	}
	_ = command()
	backend.mu.Lock()
	defer backend.mu.Unlock()
	posted := backend.posted[len(backend.posted)-1]
	if posted.Body != "summarise" || len(posted.Attachments) != 1 || posted.Attachments[0] != path {
		t.Fatalf("posted = %+v", posted)
	}
}

func TestMixedAttachmentsSplitDocumentsFromUnsupportedImages(t *testing.T) {
	backend := &fakeBackend{}
	commander := &attachmentCommander{
		fakeCommander: &fakeCommander{current: map[string]string{"talk": "text/model"}},
		model:         "text/model", supported: false,
	}
	model := NewWithCommander(backend, "docs", commander)
	document := imageFixture(t, "brief.pdf")
	image := imageFixture(t, "chart.png")
	model.input.SetValue(document + " " + image)
	model.captureImageAttachments()
	if len(model.attachments) != 2 {
		t.Fatalf("attachments = %v", model.attachments)
	}
	rendered := ansi.Strip(model.renderInput())
	if !strings.Contains(rendered, "▤ brief.pdf") || !strings.Contains(rendered, "⌾ chart.png") ||
		!strings.Contains(rendered, "text/model can't see images") {
		t.Fatalf("mixed chips = %q", rendered)
	}

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("mixed attachments did not submit")
	}
	_ = command()
	backend.mu.Lock()
	defer backend.mu.Unlock()
	posted := backend.posted[len(backend.posted)-1]
	// The image falls back to a plain path the model can at least name; the
	// document stays an attachment so the workspace copy is made.
	if len(posted.Attachments) != 1 || posted.Attachments[0] != document {
		t.Fatalf("posted attachments = %v", posted.Attachments)
	}
	if !strings.Contains(posted.Body, image) || strings.Contains(posted.Body, document) {
		t.Fatalf("posted body = %q", posted.Body)
	}
}

func TestDocumentOnlySendGetsItsOwnPlaceholderBody(t *testing.T) {
	backend := &fakeBackend{}
	commander := &attachmentCommander{
		fakeCommander: &fakeCommander{current: map[string]string{"talk": "vision/model"}},
		model:         "vision/model", supported: true,
	}
	model := NewWithCommander(backend, "docs", commander)
	model.input.SetValue(imageFixture(t, "solo.pdf"))
	model.captureImageAttachments()
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("document-only draft did not submit")
	}
	_ = command()
	backend.mu.Lock()
	posted := backend.posted[len(backend.posted)-1]
	backend.mu.Unlock()
	if posted.Body != "Document attached." {
		t.Fatalf("document-only body = %q", posted.Body)
	}

	model.input.SetValue(imageFixture(t, "pair.pdf") + " " + imageFixture(t, "pair.png"))
	model.captureImageAttachments()
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("mixed-only draft did not submit")
	}
	_ = command()
	backend.mu.Lock()
	defer backend.mu.Unlock()
	posted = backend.posted[len(backend.posted)-1]
	if posted.Body != "Attachments added." || len(posted.Attachments) != 2 {
		t.Fatalf("mixed-only posted = %+v", posted)
	}
}

func TestUnsupportedDragsStayInTheDraft(t *testing.T) {
	spreadsheet := imageFixture(t, "numbers.xlsx")
	draft := "read " + spreadsheet
	if cleaned, paths := detectImageAttachments(draft); cleaned != draft || len(paths) != 0 {
		t.Fatalf("cleaned=%q paths=%v", cleaned, paths)
	}
	if attachmentGlyph("/tmp/a.pdf") != "▤" || attachmentGlyph("/tmp/a.png") != "⌾" {
		t.Fatal("attachment glyphs do not separate documents from images")
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
	video := artifactLink("/tmp/a.mp4", "media/a.mp4", "▶", 20)
	if !strings.Contains(ansi.Strip(video), "▶ media/a.mp4") {
		t.Fatalf("video link = %q", video)
	}
	commander := &artifactCommander{fakeCommander: &fakeCommander{current: map[string]string{}}, target: path}
	relativeModel := NewWithCommander(&fakeBackend{}, "media", commander)
	relative := relativeModel.renderMediaArtifacts(store.Message{NodeID: "task", Body: "made media/result.png"}, 40)
	if !strings.Contains(relative, "file://") || !strings.Contains(ansi.Strip(relative), "⌾ media/result.png") {
		t.Fatalf("relative generated artifact was not linked: %q", relative)
	}
}

func TestVideoArtifactsUsePlayGlyph(t *testing.T) {
	path := imageFixture(t, "result.mp4")
	commander := &artifactCommander{fakeCommander: &fakeCommander{current: map[string]string{}}, target: path}
	model := NewWithCommander(&fakeBackend{}, "media-video", commander)
	rendered := model.renderMediaArtifacts(store.Message{NodeID: "task", Body: "made media/result.mp4"}, 40)
	if !strings.Contains(ansi.Strip(rendered), "▶ media/result.mp4") {
		t.Fatalf("video artifact = %q", rendered)
	}
}
