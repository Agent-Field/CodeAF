package filesdoor

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

// The secret files of a chat travel through the door beside its objects: one
// request, answered a bounded page at a time, sealed under the person's key
// inside the tunnel. These tests are written from what the two computers see.

type asked struct {
	chat, after string
	budget      int
}

// secretsDoor serves a fixed set of files for one chat and records what it was asked.
type secretsDoor struct {
	mu    sync.Mutex
	chat  string
	files []SecretFile
	asked []asked
	fail  error
}

func (d *secretsDoor) provide(_ context.Context, chat, after string, budget int) ([]SecretFile, bool, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.asked = append(d.asked, asked{chat, after, budget})
	if d.fail != nil {
		return nil, false, false, d.fail
	}
	if chat != d.chat {
		return nil, false, false, nil
	}
	var page []SecretFile
	size := 0
	for _, f := range d.files {
		if f.Path <= after {
			continue
		}
		if len(page) > 0 && size+len(f.Data) > budget {
			return page, true, true, nil
		}
		page = append(page, f)
		size += len(f.Data)
	}
	return page, false, true, nil
}

func (d *secretsDoor) requests() []asked {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]asked(nil), d.asked...)
}

func someSecrets() []SecretFile {
	at := time.Date(2026, 11, 19, 9, 30, 15, 123456789, time.UTC)
	return []SecretFile{
		{Path: ".env", Mode: 0o600, ModTime: at, Data: []byte("API_KEY=PLAINTEXT-MARKER-1\n")},
		{Path: "web/.env.production", Mode: 0o640, ModTime: at.Add(time.Hour), Data: []byte("TOKEN=PLAINTEXT-MARKER-2\n")},
		{Path: "zeros.bin", Mode: 0o644, ModTime: at.Add(-time.Hour), Data: make([]byte, 3000)},
	}
}

const aChat = "01M7T60C9QSY5VNAB64T4FXH4C"

// A request for a chat's secret files is answered with every file, with its
// bytes, its permissions and its save time exactly, and the door is told the
// chat and where to start, and what one page may hold.
func TestSecretsAnswerHoldsEveryFileExactly(t *testing.T) {
	w := newWorld(t)
	d := &secretsDoor{chat: aChat, files: someSecrets()}
	cfg := w.config(wrap(t, newDisk(t)))
	cfg.Secrets = d.provide
	w.serve(cfg)
	src := w.source()

	page, err := src.Secrets(context.Background(), aChat, "")
	if err != nil || !page.Found || page.More {
		t.Fatalf("Secrets = %+v, %v; want every file in one page", page, err)
	}
	want := someSecrets()
	if len(page.Files) != len(want) {
		t.Fatalf("got %d files; want %d", len(page.Files), len(want))
	}
	for i, f := range page.Files {
		if f.Path != want[i].Path || f.Mode != want[i].Mode || !f.ModTime.Equal(want[i].ModTime) || !bytes.Equal(f.Data, want[i].Data) {
			t.Errorf("file %d came back as %q mode %o time %v (%d bytes); want %q mode %o time %v (%d bytes)",
				i, f.Path, f.Mode, f.ModTime, len(f.Data), want[i].Path, want[i].Mode, want[i].ModTime, len(want[i].Data))
		}
	}
	got := d.requests()
	if len(got) != 1 || got[0].chat != aChat || got[0].after != "" || got[0].budget != DefaultSecretsPage {
		t.Fatalf("the door was asked %+v; want the chat, no cursor and a page of %d bytes", got, DefaultSecretsPage)
	}
}

// The door, not the asker, says how big a page is, and the asker walks the
// pages by the last path it has: a page of one file at a time reaches them all,
// in order, without repeating one.
func TestSecretsArriveInPagesTheDoorSizes(t *testing.T) {
	w := newWorld(t)
	d := &secretsDoor{chat: aChat, files: someSecrets()}
	cfg := w.config(wrap(t, newDisk(t)))
	cfg.Secrets, cfg.SecretsPage = d.provide, 1
	w.serve(cfg)
	src := w.source()

	var paths []string
	after := ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("more than ten pages for three files")
		}
		page, err := src.Secrets(context.Background(), aChat, after)
		if err != nil || !page.Found {
			t.Fatalf("page %d = %+v, %v", pages, page, err)
		}
		if len(page.Files) != 1 {
			t.Fatalf("page %d holds %d files; want one", pages, len(page.Files))
		}
		paths = append(paths, page.Files[0].Path)
		after = page.Files[0].Path
		if !page.More {
			break
		}
	}
	if strings.Join(paths, ",") != ".env,web/.env.production,zeros.bin" {
		t.Fatalf("the pages held %v", paths)
	}
}

// A chat the computer does not have is answered with a page that says so, and
// the tunnel is still good for the next request.
func TestSecretsForAChatThatIsNotThereIsAnEmptyAnswer(t *testing.T) {
	w := newWorld(t)
	d := &secretsDoor{chat: aChat, files: someSecrets()}
	cfg := w.config(wrap(t, newDisk(t)))
	cfg.Secrets = d.provide
	w.serve(cfg)
	src := w.source()

	page, err := src.Secrets(context.Background(), "01M7T60C9QSY5VNAB64T4FXH4D", "")
	if err != nil || page.Found || len(page.Files) != 0 {
		t.Fatalf("Secrets of another chat = %+v, %v; want nothing found", page, err)
	}
	if _, err := src.Has(context.Background(), []string{strings.Repeat("a", 64)}); err != nil {
		t.Fatalf("the tunnel was not good for another request: %v", err)
	}
}

// A door that was given no way to answer says there is nothing, not an error:
// a computer that cannot find the chat has no secret files to give for it.
func TestSecretsFromADoorWithoutAnswererAreNone(t *testing.T) {
	w := newWorld(t)
	w.serve(w.config(wrap(t, newDisk(t))))
	page, err := w.source().Secrets(context.Background(), aChat, "")
	if err != nil || page.Found || len(page.Files) != 0 {
		t.Fatalf("Secrets = %+v, %v; want nothing found", page, err)
	}
}

// A refusal to answer is an error for that request and never a secret in a
// sentence; the tunnel stays good.
func TestSecretsThatCannotBeReadAreAnAnsweredError(t *testing.T) {
	w := newWorld(t)
	d := &secretsDoor{chat: aChat, fail: errors.New("disk said PLAINTEXT-MARKER-3")}
	cfg := w.config(wrap(t, newDisk(t)))
	cfg.Secrets = d.provide
	w.serve(cfg)
	src := w.source()

	_, err := src.Secrets(context.Background(), aChat, "")
	if !errors.Is(err, blobstore.ErrAnswered) {
		t.Fatalf("Secrets = %v; want the door's answered error", err)
	}
	if strings.Contains(err.Error(), "PLAINTEXT-MARKER-3") {
		t.Fatalf("the provider's own words crossed the wire: %v", err)
	}
	if _, err := src.Has(context.Background(), []string{strings.Repeat("a", 64)}); err != nil {
		t.Fatalf("the tunnel was not good for another request: %v", err)
	}
}

// A computer removed from the person's devices is refused before the door
// touches anything: its request never reaches the provider.
func TestSecretsAreNeverGivenToARemovedComputer(t *testing.T) {
	w := newWorld(t)
	d := &secretsDoor{chat: aChat, files: someSecrets()}
	cfg := w.config(wrap(t, newDisk(t)))
	cfg.Secrets, cfg.RosterFor = d.provide, 10*time.Millisecond
	w.serve(cfg)
	src := w.source()

	if _, err := src.Secrets(context.Background(), aChat, ""); err != nil {
		t.Fatal(err)
	}
	before := len(d.requests())
	w.allow(w.b.ID(), false)
	time.Sleep(50 * time.Millisecond)
	_, err := src.Secrets(context.Background(), aChat, "")
	var refused *RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("Secrets after removal = %v; want a refusal", err)
	}
	if after := len(d.requests()); after != before {
		t.Fatalf("the provider was asked %d more times after the computer was removed", after-before)
	}
}

// A provider that hands over more than a page, or a file bigger than any file
// may be, is a fault of this computer's and never goes on the wire.
func TestSecretsOverTheBoundsAreNeverSent(t *testing.T) {
	w := newWorld(t)
	huge := &secretsDoor{chat: aChat, files: []SecretFile{{Path: "big", Mode: 0o600, ModTime: time.Now(), Data: make([]byte, MaxSecretFile+1)}}}
	cfg := w.config(wrap(t, newDisk(t)))
	cfg.Secrets = huge.provide
	w.serve(cfg)
	if _, err := w.source().Secrets(context.Background(), aChat, ""); !errors.Is(err, blobstore.ErrAnswered) {
		t.Fatalf("a file over the limit = %v; want the door's answered error", err)
	}
}

// What the door puts into the tunnel is sealed under the person's own key
// first: no byte of a file, a path or a time is in it, and it opens only for the
// chat and the cursor it answers, so one page cannot be passed off as another.
func TestSealedSecretsHoldNothingReadableAndOpenOnlyForTheirRequest(t *testing.T) {
	w := newWorld(t)
	page := SecretPage{Found: true, More: true, Files: someSecrets()}
	sealed, err := sealSecrets(w.id, aChat, "after-this", page)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range page.Files {
		for _, needle := range [][]byte{f.Data, []byte(f.Path)} {
			if len(needle) > 3 && bytes.Contains(sealed, needle) {
				t.Fatalf("the sealed page holds %q in the clear", needle)
			}
		}
	}
	got, err := openSecrets(w.id, aChat, "after-this", sealed)
	if err != nil || !got.Found || !got.More || len(got.Files) != 3 {
		t.Fatalf("a page opened for its own request = %+v, %v", got, err)
	}
	for _, c := range []struct{ chat, after string }{{"01M7T60C9QSY5VNAB64T4FXH4D", "after-this"}, {aChat, ""}} {
		if _, err := openSecrets(w.id, c.chat, c.after, sealed); err == nil {
			t.Fatalf("a page opened for the request (%s, %q) it does not answer", c.chat, c.after)
		}
	}
	other := newWorld(t)
	if _, err := openSecrets(other.id, aChat, "after-this", sealed); err == nil {
		t.Fatal("a page opened under another person's key")
	}
	tampered := bytes.Clone(sealed)
	tampered[len(tampered)/2] ^= 1
	if _, err := openSecrets(w.id, aChat, "after-this", tampered); err == nil {
		t.Fatal("a page with a changed byte opened")
	}
}
