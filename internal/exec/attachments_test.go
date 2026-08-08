package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/cas"
)

func attachmentFixture(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The journey the reference exists for: a contract attached on Monday, moved
// into a different folder on Tuesday, and a job retried on Wednesday that
// still reads exactly the bytes that were attached.
func TestAKeptAttachmentSurvivesTheOriginalMovingAway(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cas")
	original := attachmentFixture(t, t.TempDir(), "contract.pdf", "%PDF-1.7 termination clause")

	reference, err := KeepAttachment(root, original)
	if err != nil {
		t.Fatalf("keep: %v", err)
	}
	if !strings.HasPrefix(reference, cas.Scheme) || !strings.HasSuffix(reference, original) {
		t.Fatalf("reference = %q, want a digest carrying %q", reference, original)
	}
	if filepath.Ext(reference) != ".pdf" || filepath.Base(reference) != "contract.pdf" {
		t.Fatalf("reference hides what it is: ext=%q base=%q", filepath.Ext(reference), filepath.Base(reference))
	}

	first, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	staged, err := StageAttachments(first, root, []string{reference})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if len(staged.Documents) != 1 || !strings.HasPrefix(staged.Documents[0], "attachments/contract-") {
		t.Fatalf("staged = %+v", staged)
	}
	located, ok := first.Locate(staged.Documents[0])
	if !ok {
		t.Fatalf("staged document is not in the workspace: %+v", staged)
	}
	if data, err := os.ReadFile(located); err != nil || string(data) != "%PDF-1.7 termination clause" {
		t.Fatalf("staged bytes = %q err=%v", data, err)
	}

	// The person tidies their Downloads folder.
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}

	// A retry runs in a fresh directory, as retries do.
	retry, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	again, err := StageAttachments(retry, root, []string{reference})
	if err != nil {
		t.Fatalf("retry after the original moved: %v", err)
	}
	if len(again.Documents) != 1 || again.Documents[0] != staged.Documents[0] {
		t.Fatalf("retry staged %+v, want the same name as %+v", again, staged)
	}
	retried, ok := retry.Locate(again.Documents[0])
	if !ok {
		t.Fatal("retry did not materialize the document")
	}
	if data, err := os.ReadFile(retried); err != nil || string(data) != "%PDF-1.7 termination clause" {
		t.Fatalf("retried bytes = %q err=%v", data, err)
	}

	// Tomorrow, in a new session, the follow-up carries the same reference and
	// resolves it with nothing else in hand.
	followUp, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	later, err := StageAttachments(followUp, root, []string{reference})
	if err != nil || len(later.Documents) != 1 {
		t.Fatalf("follow-up staging = %+v err=%v", later, err)
	}
}

// The digest is the name, so the same bytes attached twice are stored once —
// and staging is idempotent, which is what lets every leaf of one job stage
// the same inputs and converge on one already-complete file.
func TestIdenticalContentIsKeptOnceAndStagedOnce(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cas")
	dir := t.TempDir()
	first := attachmentFixture(t, dir, "brief.pdf", "same bytes")
	second := attachmentFixture(t, dir, "copy-of-brief.pdf", "same bytes")

	referenceOne, err := KeepAttachment(root, first)
	if err != nil {
		t.Fatal(err)
	}
	referenceTwo, err := KeepAttachment(root, second)
	if err != nil {
		t.Fatal(err)
	}
	digestOne, _, _ := cas.ParseReference(referenceOne)
	digestTwo, _, _ := cas.ParseReference(referenceTwo)
	if digestOne != digestTwo {
		t.Fatalf("identical bytes got two digests: %s and %s", digestOne, digestTwo)
	}
	blobs, err := cas.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists, err := blobs.Stat(digestOne); err != nil || !exists {
		t.Fatalf("blob missing: exists=%v err=%v", exists, err)
	}

	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	staged, err := StageAttachments(space, root, []string{referenceOne, referenceOne})
	if err != nil || len(staged.Documents) != 1 {
		t.Fatalf("repeat staging = %+v err=%v", staged, err)
	}
}

// A screenshot becomes a workspace input like a document does, so view_image
// and the vision proxy can reach it at all.
func TestImagesStageIntoTheWorkspaceBesideDocuments(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cas")
	dir := t.TempDir()
	document := attachmentFixture(t, dir, "q3 filing.pdf", "%PDF filing")
	image := attachmentFixture(t, dir, "screenshot.png", "png-bytes")
	spreadsheet := attachmentFixture(t, dir, "numbers.xlsx", "not staged")

	references := make([]string, 0, 3)
	for _, source := range []string{document, image, spreadsheet} {
		reference, err := KeepAttachment(root, source)
		if err != nil {
			t.Fatal(err)
		}
		references = append(references, reference)
	}
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	staged, err := StageAttachments(space, root, references)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if len(staged.Documents) != 1 || !strings.HasPrefix(staged.Documents[0], "attachments/q3 filing-") {
		t.Fatalf("documents = %v", staged.Documents)
	}
	if len(staged.Images) != 1 || !strings.HasPrefix(staged.Images[0], "attachments/screenshot-") {
		t.Fatalf("images = %v", staged.Images)
	}
	if len(staged.ImageFiles) != 1 || !filepath.IsAbs(staged.ImageFiles[0]) {
		t.Fatalf("image files = %v", staged.ImageFiles)
	}
	if _, ok := space.Locate(staged.Images[0]); !ok {
		t.Fatal("staged image is not in the workspace")
	}
}

// Work journaled before copies existed still names the person's own file.
func TestAPlainPathStillStages(t *testing.T) {
	source := attachmentFixture(t, t.TempDir(), "legacy.pdf", "old work")
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	staged, err := StageAttachments(space, filepath.Join(t.TempDir(), "cas"), []string{source})
	if err != nil || len(staged.Documents) != 1 {
		t.Fatalf("legacy staging = %+v err=%v", staged, err)
	}
}

func TestStagingRefusesOversizedVanishedAndUnworkspacedInputs(t *testing.T) {
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "cas")
	oversized := filepath.Join(t.TempDir(), "huge.pdf")
	file, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxAttachmentBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := KeepAttachment(root, oversized); err == nil ||
		!strings.Contains(err.Error(), "over the 25 MB limit") {
		t.Fatalf("oversized keep error = %v", err)
	}
	if _, err := StageAttachments(space, root, []string{oversized}); err == nil ||
		!strings.Contains(err.Error(), "over the 25 MB limit") {
		t.Fatalf("oversized stage error = %v", err)
	}
	if _, err := StageAttachments(space, root, []string{filepath.Join(t.TempDir(), "gone.pdf")}); err == nil ||
		!strings.Contains(err.Error(), "file is unavailable") {
		t.Fatalf("vanished stage error = %v", err)
	}
	if _, err := KeepAttachment(root, t.TempDir()); err == nil {
		t.Fatal("a directory was kept as an attachment")
	}
	if _, err := StageAttachments(nil, root, []string{oversized}); err == nil {
		t.Fatal("staging into a nil workspace succeeded")
	}
}

// A leaf that cannot see is told what it has and who can look — and when
// nothing can, that it must say so rather than answer around the image.
func TestTheImageNoteNamesWhoCanLook(t *testing.T) {
	if note := attachedImageNote(nil, true); note != "" {
		t.Fatalf("note without images = %q", note)
	}
	proxied := attachedImageNote([]string{"attachments/screenshot-ab12cd34.png"}, true)
	if !strings.Contains(proxied, "view_image") ||
		!strings.Contains(proxied, "attachments/screenshot-ab12cd34.png") {
		t.Fatalf("proxied note = %q", proxied)
	}
	blind := attachedImageNote([]string{"attachments/screenshot-ab12cd34.png"}, false)
	if !strings.Contains(blind, "could not be read") || strings.Contains(blind, "view_image") {
		t.Fatalf("blind note = %q", blind)
	}
}

func TestReferenceRoundTripsAndLeavesPathsAlone(t *testing.T) {
	digest := cas.DigestBytes([]byte("bytes"))
	reference := cas.Reference(digest, "/Users/me/Documents/contract.pdf")
	ref, source, ok := cas.ParseReference(reference)
	if !ok || ref != digest || source != "/Users/me/Documents/contract.pdf" {
		t.Fatalf("round trip = %s %q %v", ref, source, ok)
	}
	if got := cas.SourcePath("/Users/me/plain.pdf"); got != "/Users/me/plain.pdf" {
		t.Fatalf("plain path was rewritten: %q", got)
	}
	if _, _, ok := cas.ParseReference("cas://not-a-digest/x.pdf"); ok {
		t.Fatal("a malformed reference parsed as one")
	}
}
