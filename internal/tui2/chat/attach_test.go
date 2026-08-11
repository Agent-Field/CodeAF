package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/cas"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
)

// JOURNEY 17, at the seam. The gate script drives the real binary; these pin
// the three facts it checks, so a regression is caught by `go test` rather than
// by a twenty-five second tmux run: the file is named and captured, the journal
// row carries a cas:// reference, and the copy outlives its source.

// -- the keeper fakes ----------------------------------------------------------

// keepingCommander is a commander that really does copy into a real CAS, the
// way the engine cmd/aforge builds does. It is the same seam v1's own test uses
// (internal/tui/attachments_test.go), pointed at internal/exec's door rather
// than at a hand-rolled imitation of it — a fake that minted its own reference
// shape would prove this surface agrees with itself and nothing else.
type keepingCommander struct {
	fakeCommander
	root string
	// asked records every path handed over, so a test can prove the surface
	// went through the engine rather than around it.
	asked []string
}

func (c *keepingCommander) KeepAttachment(path string) (string, error) {
	c.asked = append(c.asked, path)
	return exec.KeepAttachment(c.root, path)
}

// refusingCommander is a keeper that cannot keep. It exists for the one law
// that matters more than the CAS: what the reader showed us is never lost.
type refusingCommander struct {
	fakeCommander
}

func (c *refusingCommander) KeepAttachment(string) (string, error) {
	return "", os.ErrPermission
}

// dot writes the smallest honest PNG there is and hands back its path.
func dot(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	// An 8-byte PNG signature is enough: nothing in this path reads the pixels,
	// and a test that embedded a real image would be asserting about base64.
	if err := os.WriteFile(path, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// -- what may be attached ------------------------------------------------------

func TestAttachmentCandidateTakesRealMediaAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	image := dot(t, dir, "dot.png")
	document := filepath.Join(dir, "contract.pdf")
	if err := os.WriteFile(document, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	sheet := filepath.Join(dir, "numbers.xlsx")
	if err := os.WriteFile(sheet, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(dir, "shots.png")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name  string
		token string
		want  bool
	}{
		{"an image on disk", image, true},
		{"a document on disk", document, true},
		{"a name with no file behind it", filepath.Join(dir, "missing.png"), false},
		{"a kind we do not carry", sheet, false},
		{"a directory wearing an extension", folder, false},
		{"an ordinary word", "hello", false},
		{"a word with a dot in it", "v1.2", false},
		{"nothing at all", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, ok := attachmentCandidate(c.token)
			if ok != c.want {
				t.Fatalf("attachmentCandidate(%q) ok = %v, want %v", c.token, ok, c.want)
			}
			if !ok {
				return
			}
			if got.Path != c.token {
				t.Fatalf("Path = %q, want the absolute path %q", got.Path, c.token)
			}
			if got.Name != filepath.Base(c.token) {
				t.Fatalf("Name = %q, want %q", got.Name, filepath.Base(c.token))
			}
			if got.Bytes <= 0 {
				t.Fatalf("Bytes = %d, want the file's real size", got.Bytes)
			}
		})
	}
}

func TestAttachmentCandidateExpandsTheShellsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dot(t, home, "dot.png")

	got, ok := attachmentCandidate("~/dot.png")
	if !ok {
		t.Fatal("~/dot.png was not attachable — `~` is the path a person types")
	}
	if got.Path != filepath.Join(home, "dot.png") {
		t.Fatalf("Path = %q, want it expanded to %q", got.Path, filepath.Join(home, "dot.png"))
	}
}

// -- the reference the journal holds -------------------------------------------

func TestKeptAttachmentsBecomeCASReferences(t *testing.T) {
	dir := t.TempDir()
	image := dot(t, dir, "dot.png")
	keeper := &keepingCommander{root: filepath.Join(dir, "cas")}

	got := keepAttachments(keeper, []composer.Attachment{{Path: image}})
	if len(got) != 1 {
		t.Fatalf("keepAttachments = %v, want one reference", got)
	}
	ref, source, ok := cas.ParseReference(got[0])
	if !ok {
		t.Fatalf("journaled %q, want a cas:// reference", got[0])
	}
	if source != image {
		t.Fatalf("reference names %q, want the person's own path %q", source, image)
	}

	// The whole point of the copy: the record survives the original.
	if err := os.Remove(image); err != nil {
		t.Fatal(err)
	}
	blobs, err := cas.New(keeper.root)
	if err != nil {
		t.Fatal(err)
	}
	if err := blobs.Verify(ref); err != nil {
		t.Fatalf("the CAS object did not outlive its source: %v", err)
	}
}

func TestAKeeperThatCannotKeepStillJournalsTheFile(t *testing.T) {
	dir := t.TempDir()
	image := dot(t, dir, "dot.png")

	for _, c := range []struct {
		name   string
		keeper AttachmentKeeper
	}{
		{"no engine behind the window", nil},
		{"an engine that refused", &refusingCommander{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := keepAttachments(c.keeper, []composer.Attachment{{Path: image}})
			if len(got) != 1 || got[0] != image {
				t.Fatalf("keepAttachments = %v, want the person's own path — a weaker record, never none", got)
			}
		})
	}
}

func TestAttachmentBodySaysWhatRodeAlong(t *testing.T) {
	for _, c := range []struct {
		name string
		list []composer.Attachment
		want string
	}{
		{"one image", []composer.Attachment{{Path: "/x/a.png"}}, "Image attached."},
		{"one document", []composer.Attachment{{Path: "/x/a.pdf"}}, "Document attached."},
		{"both", []composer.Attachment{{Path: "/x/a.png"}, {Path: "/x/b.pdf"}}, "Attachments added."},
		{"nothing", nil, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := attachmentBody(c.list); got != c.want {
				t.Fatalf("attachmentBody = %q, want %q", got, c.want)
			}
		})
	}
}

// -- end to end, through the real ladder ----------------------------------------

// typeInto drives a draft in one keystroke at a time, through the app's own key
// ladder, which is the only way the capture path a person exercises is the path
// under test.
func typeInto(app *App, text string) {
	for _, r := range text {
		app.key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestANamedFileIsCapturedCopiedAndJournaled(t *testing.T) {
	dir := t.TempDir()
	image := dot(t, dir, "dot.png")
	backend := &fakeBackend{}
	keeper := &keepingCommander{root: filepath.Join(dir, "cas")}
	app := newTestApp(backend, keeper, nil)

	typeInto(app, "what colour is this: "+image)

	// The chip exists before the send, and the path has left the draft.
	if draft := app.composer.Draft(); draft != "what colour is this:" {
		t.Fatalf("draft = %q, want the sentence without the path", draft)
	}
	if got := ansi.Strip(frame(app)); !strings.Contains(got, "dot.png") {
		t.Fatalf("frame does not show the chip:\n%s", got)
	}

	runAll(t, app.drain(app.key(tea.KeyPressMsg{Code: tea.KeyEnter})))

	if len(backend.posted) != 1 {
		t.Fatalf("posted %d messages, want 1", len(backend.posted))
	}
	posted := backend.posted[0]
	if posted.Role != store.RoleUser {
		t.Fatalf("role = %q, want a user turn", posted.Role)
	}
	if posted.Body != "what colour is this:" {
		t.Fatalf("body = %q, want the words the person typed", posted.Body)
	}
	if len(posted.Attachments) != 1 {
		t.Fatalf("attachments = %v, want exactly one — this is J17's whole assertion",
			posted.Attachments)
	}
	ref, source, ok := cas.ParseReference(posted.Attachments[0])
	if !ok {
		t.Fatalf("journaled %q, want a cas:// reference like v1 writes", posted.Attachments[0])
	}
	if source != image {
		t.Fatalf("reference names %q, want %q", source, image)
	}
	if len(keeper.asked) != 1 || keeper.asked[0] != image {
		t.Fatalf("engine was asked for %v, want [%s]", keeper.asked, image)
	}

	// And the CAS really gained the object, which is the check the gate makes
	// by counting files under the store root.
	blobs, err := cas.New(keeper.root)
	if err != nil {
		t.Fatal(err)
	}
	if size, exists, err := blobs.Stat(ref); err != nil || !exists || size == 0 {
		t.Fatalf("Stat(%s) = %d, %v, %v — want a real object", ref, size, exists, err)
	}
}

func TestAFileWithNoWordsGetsItsOwnBody(t *testing.T) {
	dir := t.TempDir()
	image := dot(t, dir, "dot.png")
	backend := &fakeBackend{}
	app := newTestApp(backend, &keepingCommander{root: filepath.Join(dir, "cas")}, nil)

	typeInto(app, image)
	runAll(t, app.drain(app.key(tea.KeyPressMsg{Code: tea.KeyEnter})))

	if len(backend.posted) != 1 {
		t.Fatalf("posted %d messages, want 1 — attaching a file is speech", len(backend.posted))
	}
	if got := backend.posted[0].Body; got != "Image attached." {
		t.Fatalf("body = %q, want v1's own sentence", got)
	}
	if len(backend.posted[0].Attachments) != 1 {
		t.Fatalf("attachments = %v, want one", backend.posted[0].Attachments)
	}
}

func TestAWindowWithNoEngineStillSends(t *testing.T) {
	dir := t.TempDir()
	image := dot(t, dir, "dot.png")
	backend := &fakeBackend{}
	app := newTestApp(backend, nil, nil)

	typeInto(app, "look "+image)
	runAll(t, app.drain(app.key(tea.KeyPressMsg{Code: tea.KeyEnter})))

	if len(backend.posted) != 1 {
		t.Fatalf("posted %d messages, want 1", len(backend.posted))
	}
	if got := backend.posted[0].Attachments; len(got) != 1 || got[0] != image {
		t.Fatalf("attachments = %v, want the raw path — the record degrades, the file is not dropped", got)
	}
}

// -- rendering ------------------------------------------------------------------

func TestAJournaledAttachmentRendersAsAReferenceNotAsBytes(t *testing.T) {
	backend := &fakeBackend{}
	reference := cas.Reference(cas.DigestBytes([]byte("pixels")), "/home/someone/shots/dot.png")
	backend.add(store.Message{
		SessionID:   testSession,
		Role:        store.RoleUser,
		Body:        "what colour is this:",
		Attachments: []string{reference},
	})
	app := newTestApp(backend, nil, nil)
	poll(t, app)

	got := ansi.Strip(frame(app))
	if !strings.Contains(got, "/home/someone/shots/dot.png") {
		t.Fatalf("frame does not reference the file by the path a person would recognize:\n%s", got)
	}
	if strings.Contains(got, "cas://") {
		t.Fatalf("frame shows the digest, which is the record's business and not the reader's:\n%s", got)
	}
	// 12.5.1: referenced, never inlined. The row is one line, not the file.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "dot.png") && strings.Contains(line, "what colour") {
			t.Fatalf("the reference was flattened into the sentence: %q", line)
		}
	}
}

func TestAPlainPathAttachmentRendersTheSameWay(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{
		SessionID:   testSession,
		Role:        store.RoleUser,
		Body:        "look at this",
		Attachments: []string{"/home/someone/shots/dot.png"},
	})
	app := newTestApp(backend, nil, nil)
	poll(t, app)

	if got := ansi.Strip(frame(app)); !strings.Contains(got, "/home/someone/shots/dot.png") {
		t.Fatalf("a pre-CAS record renders differently from a kept one:\n%s", got)
	}
}
