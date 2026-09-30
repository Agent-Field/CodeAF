package syncsetup

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/rotate"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// pairScreen answers yes to everything and remembers the code it was shown.
type pairScreen struct{ codes chan *pair.Code }

func (p pairScreen) Show(c *pair.Code, _ string)              { p.codes <- c }
func (p pairScreen) Burned()                                  {}
func (p pairScreen) Waiting(string)                           {}
func (p pairScreen) Ask(context.Context, string, string) bool { return true }

// TestRotationEndToEnd is the whole story on the real engine, relay and pairing
// mailbox: two computers of one person share a chat; one rotates; the old
// identity refuses writes with 410; the other computer follows by pairing,
// opens the chat and has its secret; and after the grace the relay has deleted
// the old identity.
func TestRotationEndToEnd(t *testing.T) {
	h := newTwoHomes(t)
	ctx := context.Background()
	chat := h.openA()
	chat.mustSay("first")
	chat.mustSay("second")
	h.durable(h.cell.ID, h.cell)
	if err := chat.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	vault, err := keys.Open(h.homeA)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Put("p/OPENROUTER_API_KEY", keys.Entry{Name: "OPENROUTER_API_KEY", Value: "sk-kept", Scope: "p"}); err != nil {
		t.Fatal(err)
	}
	oldID, oldDev := h.a.Identity, h.a.Device

	// A rotates, with a grace the test can pass.
	base := h.engine
	base.Workspace = ""
	held := func(id string) (cell.Cell, bool) { return h.cell, id == h.cell.ID }
	env := h.a.Rotation(base, held, nameA, func(line string) { t.Log("rotate:", line) })
	env.Grace = 2 * time.Second
	plan, err := env.Plan(ctx)
	if err != nil || plan.Chats != 1 || plan.Missing != 0 {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	res, err := env.Rotate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.a.Tidy(); err != nil {
		t.Fatal(err)
	}
	now, err := identity.Load(h.homeA)
	if err != nil || now.ID() != res.NewID || now.ID() == oldID.ID() {
		t.Fatalf("A's identity = %v (%v), new %s, old %s", now.ID(), err, res.NewID, oldID.ID())
	}

	// The old identity takes no write on either wire, and says 410.
	old := oldSide(h, oldID, oldDev)
	if _, err := old.dir.Create(ctx, "01J0000000000000000000000Z", directory.CellInit{Head: h.head()}); !errors.Is(err, directory.ErrRotated) {
		t.Fatalf("a write to the old identity: %v, want ErrRotated", err)
	}
	frame, _ := blobFrame(t)
	if _, err := old.store.PutFrame(ctx, frame); !errors.Is(err, wireauth.ErrRotated) {
		t.Fatalf("a frame put to the old identity: %v, want ErrRotated", err)
	}
	if status := old.status(t, "/v1/dir/cells/"+h.cell.ID+"/archive"); status != http.StatusGone {
		t.Fatalf("status of a write to the old identity = %d, want 410", status)
	}

	// B, still on the old identity, follows by pairing with A's new identity.
	a2 := openSync(t, h.homeA)
	if a2.Identity.ID() != res.NewID {
		t.Fatal("A did not open under the new identity")
	}
	joinB(t, h, a2, res)
	b2 := openSync(t, h.homeB)
	if b2.Identity.ID() != res.NewID {
		t.Fatalf("B follows %s, want %s", b2.Identity.ID(), res.NewID)
	}
	h.b = b2

	// B opens the chat from the relay alone and has A's two turns.
	rows, err := b2.Rows(ctx)
	if err != nil || len(rows) != 1 || rows[0].Cell != h.cell.ID {
		t.Fatalf("B's list under the new identity = %+v, %v", rows, err)
	}
	if rows[0].Title != "two homes" {
		t.Fatalf("B reads the chat's title as %q", rows[0].Title)
	}
	got, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatalf("B could not take the chat: %v", err)
	}
	assertSameTree(t, h.work, workspaceOf(got.Taken.Cell.Root))
	// The vault arrives with the chat, sealed for the new identity and opened by it.
	if e, err := keyOf(h.homeB); err != nil || e != "sk-kept" {
		t.Fatalf("B's secret after taking the chat = %q, %v", e, err)
	}

	// After the grace the relay deletes the old identity and answers gone.
	h.clock.advance(3 * time.Second)
	if err := h.svc.Sweep(); err != nil {
		t.Fatal(err)
	}
	if _, err := old.dir.List(ctx); !errors.Is(err, wireauth.ErrGone) {
		t.Fatalf("the old identity after the grace: %v, want ErrGone", err)
	}
	if _, err := os.Stat(filepath.Join(h.store, "directory", oldID.ID()+".db")); err == nil {
		t.Fatal("the old directory file survived the sweep")
	}
	if _, err := os.Stat(filepath.Join(h.store, "blobs", oldID.ID())); err == nil {
		t.Fatal("the old identity's blobs survived the sweep")
	}
	if l, err := a2.Dir.List(ctx); err != nil || len(l.Cells) != 1 {
		t.Fatalf("the new identity after the sweep: %v, %v", l.Cells, err)
	}
	if chatlist.Replaced == "" {
		t.Fatal("no sentence for a replaced identity")
	}
}

// oldClients are the old identity's device talking to the relay as it did before.
type oldClients struct {
	dir   *directory.HTTP
	store *blobstore.HTTP
	sign  func(*http.Request, []byte)
	url   string
}

func oldSide(h *twoHomes, id identity.Identity, dev identity.Dev) oldClients {
	sign := reqsign.SignFor(deviceSigner{id, dev}, time.Now)
	return oldClients{
		dir:   directory.NewHTTP(h.url, sign, &http.Client{Timeout: requestTimeout}),
		store: blobstore.NewHTTP(h.url, sign, &http.Client{}),
		sign:  sign, url: h.url,
	}
}

// status sends one signed write and answers the HTTP status.
func (o oldClients) status(t *testing.T, path string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, o.url+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	o.sign(req, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func blobFrame(t *testing.T) ([]byte, string) {
	t.Helper()
	rid := "ab" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd"
	frame, err := blobstore.Encode("00000000000000000000000000000000", []blobstore.Object{{RID: rid, Bytes: []byte("AGEO\x01x")}})
	if err != nil {
		t.Fatal(err)
	}
	return frame, rid
}

// joinB pairs B with A's rotated identity through the relay's own mailbox.
func joinB(t *testing.T, h *twoHomes, a *Sync, res rotate.Result) {
	t.Helper()
	route := pair.Mailbox{Box: pairbox.NewHTTP(h.url, &http.Client{Timeout: time.Minute}), URL: h.url, Host: "relay.test"}
	screen := pairScreen{codes: make(chan *pair.Code, 1)}
	ctx, stop := context.WithTimeout(context.Background(), time.Minute)
	defer stop()
	done := make(chan error, 1)
	go func() {
		_, err := pair.Offer(ctx, route, pair.Grant{Identity: a.Identity, Replaces: identity.Predecessors(h.homeA)}, screen)
		done <- err
	}()
	code := <-screen.codes
	joined, err := pair.Join(ctx, route, pair.Joining{Home: h.homeB, Label: nameB}, code.Shown(), screen)
	if err != nil || !joined.Successor {
		t.Fatalf("B's join = %+v, %v", joined, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("A's offer: %v", err)
	}
	_ = res
}

func keyOf(home string) (string, error) {
	v, err := keys.Open(home)
	if err != nil {
		return "", err
	}
	e, err := v.Get("p/OPENROUTER_API_KEY")
	return e.Value, err
}
