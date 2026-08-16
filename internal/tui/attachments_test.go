package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/cas"
	"github.com/Agent-Field/aforge-v2/internal/exec"
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

// A generated artifact resolves the same way whichever seam asks: the surface
// reaches for the workspace resolver, and the media-only one is what an older
// embedder had.
func (c *artifactCommander) ResolveWorkspacePath(_, _ string) (string, bool) {
	return c.target, true
}

func (c *artifactCommander) ResolveMediaPath(_, _ string) (string, bool) {
	return c.target, true
}

func (c *attachmentCommander) ImageInputSupportFor(string) (string, bool) {
	return c.model, c.supported
}

type keepingCommander struct {
	*fakeCommander
	root string
	kept []string
}

func (c *keepingCommander) KeepAttachment(path string) (string, error) {
	c.kept = append(c.kept, path)
	return exec.KeepAttachment(c.root, path)
}

// The message is the mention: what it carries is a reference to our own copy,
// while the composer above it goes on naming the person's own file.
func TestSendingAnAttachmentKeepsACopyAndCarriesTheReference(t *testing.T) {
	backend := &fakeBackend{}
	commander := &keepingCommander{fakeCommander: newFakeCommander(), root: filepath.Join(t.TempDir(), "cas")}
	model := NewWithCommander(backend, "keeping", commander)
	path := imageFixture(t, "contract.pdf")
	model.input.SetValue("what does " + path + " say about termination")
	model.captureImageAttachments()
	if len(model.attachments) != 1 || model.attachments[0] != path {
		t.Fatalf("composer holds %v, want the person's own path", model.attachments)
	}

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("attachment did not submit")
	}
	_ = command()
	backend.mu.Lock()
	defer backend.mu.Unlock()
	posted := backend.posted[len(backend.posted)-1]
	if len(posted.Attachments) != 1 {
		t.Fatalf("posted = %+v", posted)
	}
	reference := posted.Attachments[0]
	digest, source, ok := cas.ParseReference(reference)
	if !ok || source != path || digest == "" {
		t.Fatalf("posted attachment %q is not a durable reference", reference)
	}
	if len(commander.kept) != 1 || commander.kept[0] != path {
		t.Fatalf("keeper saw %v", commander.kept)
	}
	// The copy is real: deleting the person's file leaves the message whole.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	blobs, err := cas.New(commander.root)
	if err != nil {
		t.Fatal(err)
	}
	if err := blobs.Verify(digest); err != nil {
		t.Fatalf("kept copy did not survive: %v", err)
	}
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

// A screenshot pasted at a talk model with no eyes used to be dropped on the
// floor: no copy, no fallback, and nothing said to anyone. It is staged like
// any other attachment now, and the chip says where it can be looked at.
func TestNonVisionAttachmentIsStagedRatherThanDropped(t *testing.T) {
	backend := &fakeBackend{}
	commander := &attachmentCommander{
		fakeCommander: &fakeCommander{current: map[string]string{"talk": "text/model"}},
		model:         "text/model", supported: false,
	}
	model := NewWithCommander(backend, "text", commander)
	path := imageFixture(t, "fallback.webp")
	model.input.SetValue("inspect " + path)
	model.captureImageAttachments()
	if rendered := ansi.Strip(model.renderInput()); !strings.Contains(rendered, "text/model can't see it here") {
		t.Fatalf("fallback hint missing: %q", rendered)
	}
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("non-vision attachment did not submit")
	}
	_ = command()
	backend.mu.Lock()
	defer backend.mu.Unlock()
	posted := backend.posted[len(backend.posted)-1]
	if len(posted.Attachments) != 1 || posted.Attachments[0] != path {
		t.Fatalf("blind talk model dropped the image: %+v", posted)
	}
	if strings.Contains(posted.Body, path) {
		t.Fatalf("image degraded into the message body: %q", posted.Body)
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

func TestMixedAttachmentsAllRideAsAttachments(t *testing.T) {
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
		!strings.Contains(rendered, "text/model can't see it here") {
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
	// Both are staged for the work; what can look at the image is decided
	// where the job runs, not by the model answering in the thread.
	if len(posted.Attachments) != 2 || posted.Attachments[0] != document || posted.Attachments[1] != image {
		t.Fatalf("posted attachments = %v", posted.Attachments)
	}
	if strings.Contains(posted.Body, image) {
		t.Fatalf("posted body = %q", posted.Body)
	}
}

// The manual promises .pdf, .docx and .pptx. The composer used to detect only
// the first two-thirds of that sentence, and said nothing about the rest.
func TestOfficeDocumentsAttachTheWayPDFsDo(t *testing.T) {
	for _, name := range []string{"minutes.docx", "deck.pptx", "filing.pdf"} {
		model := NewWithCommander(&fakeBackend{}, "docs", newFakeCommander())
		path := imageFixture(t, name)
		model.input.SetValue("read " + path)
		model.captureImageAttachments()
		if model.input.Value() != "read" || len(model.attachments) != 1 || model.attachments[0] != path {
			t.Fatalf("%s: draft=%q attachments=%v", name, model.input.Value(), model.attachments)
		}
		if rendered := ansi.Strip(model.renderInput()); !strings.Contains(rendered, "▤ "+name) {
			t.Fatalf("%s did not become a document chip: %q", name, rendered)
		}
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
	artifact := store.Message{NodeID: "task", Body: "made media/result.png"}
	_ = relativeModel.renderMediaArtifacts(artifact, 40)
	settleWorkspaceLinks(t, relativeModel)
	relative := relativeModel.renderMediaArtifacts(artifact, 40)
	if !strings.Contains(relative, "file://") || !strings.Contains(ansi.Strip(relative), "⌾ media/result.png") {
		t.Fatalf("relative generated artifact was not linked: %q", relative)
	}
}

func TestVideoArtifactsUsePlayGlyph(t *testing.T) {
	path := imageFixture(t, "result.mp4")
	commander := &artifactCommander{fakeCommander: &fakeCommander{current: map[string]string{}}, target: path}
	model := NewWithCommander(&fakeBackend{}, "media-video", commander)
	artifact := store.Message{NodeID: "task", Body: "made media/result.mp4"}
	_ = model.renderMediaArtifacts(artifact, 40)
	settleWorkspaceLinks(t, model)
	rendered := model.renderMediaArtifacts(artifact, 40)
	if !strings.Contains(ansi.Strip(rendered), "▶ media/result.mp4") {
		t.Fatalf("video artifact = %q", rendered)
	}
}
