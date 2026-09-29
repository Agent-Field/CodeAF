package vaultsync

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
)

var ctx = context.Background()

// rig is one person's world: a shared store and directory, and any number of
// machines that hold the same identity.
type rig struct {
	t     *testing.T
	store *blobstore.Memory
	dir   *directory.Memory
	id    identity.Identity
	n     int
}

type machine struct {
	Syncer
	vault *keys.Vault
	home  string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	return &rig{t: t, store: blobstore.NewMemory(), dir: directory.NewMemory(time.Now)}
}

// machine adds a home holding this rig's identity, made on first use.
func (r *rig) machine() *machine {
	r.t.Helper()
	r.n++
	home := filepath.Join(r.t.TempDir(), "home")
	if r.n == 1 {
		id, err := identity.Ensure(home)
		must(r.t, err)
		r.id = id
	} else {
		must(r.t, identity.Save(home, r.id))
	}
	v, err := keys.Open(home)
	must(r.t, err)
	dev := "dev_" + strings.Repeat(string(rune('a'+r.n)), 32)
	s := Syncer{Store: r.store, Dir: r.dir.For(dev), Vault: v, CellKeyID: r.id.CellKeyID()}
	return &machine{Syncer: s, vault: v, home: home}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// put stores a secret and lets the clock tick so a later edit is strictly newer.
func (m *machine) put(t *testing.T, id, name, value string) {
	t.Helper()
	must(t, m.vault.Put(id, keys.Entry{Name: name, Value: value, Scope: "p"}))
	time.Sleep(2 * time.Millisecond)
}

func (m *machine) value(t *testing.T, id string) string {
	t.Helper()
	e, err := m.vault.Get(id)
	must(t, err)
	return e.Value
}

func TestPushPullBetweenTwoHomes(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	a.put(t, "s1", "TOKEN", "abc")
	must(t, a.Push(ctx))
	must(t, b.Pull(ctx))
	if got := b.value(t, "s1"); got != "abc" {
		t.Fatalf("b holds %q", got)
	}
}

// raced runs a hook between a device's List and its SetVault, which is exactly
// where another device's push can slip in.
type raced struct {
	directory.Client
	hook     func()
	lists    int
	setVault int
}

func (c *raced) List(ctx context.Context) (directory.Listing, error) {
	l, err := c.Client.List(ctx)
	if c.lists++; c.lists == 1 && c.hook != nil {
		c.hook()
	}
	return l, err
}

func (c *raced) SetVault(ctx context.Context, old, next string) error {
	c.setVault++
	return c.Client.SetVault(ctx, old, next)
}

func TestConcurrentPushMerges(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	a.put(t, "sa", "A", "1")
	b.put(t, "sb", "B", "2")
	race := &raced{Client: b.Dir, hook: func() { must(t, a.Push(ctx)) }}
	b.Dir = race
	must(t, b.Push(ctx))
	if race.setVault != 2 {
		t.Fatalf("a push that lost the CAS must retry exactly once, SetVault ran %d times", race.setVault)
	}
	must(t, a.Pull(ctx))
	for _, m := range []*machine{a, b} {
		if m.value(t, "sa") != "1" || m.value(t, "sb") != "2" {
			t.Fatal("no secret may be lost")
		}
	}
}

func TestPushGivesUpAfterOneRetry(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	b.put(t, "sb", "B", "2")
	n := 0
	race := &raced{Client: b.Dir}
	race.hook = func() { // every attempt is beaten by a fresh push from a
		n++
		a.put(t, "s"+string(rune('0'+n)), "N", "v")
		must(t, a.Push(ctx))
	}
	b.Dir = &everyList{raced: race}
	err := b.Push(ctx)
	if !errors.Is(err, directory.ErrCAS) || race.setVault != 2 {
		t.Fatalf("want ErrCAS after exactly two attempts, got %v after %d", err, race.setVault)
	}
}

// everyList fires the hook on every List, not just the first.
type everyList struct{ *raced }

func (c *everyList) List(ctx context.Context) (directory.Listing, error) {
	l, err := c.raced.Client.List(ctx)
	c.hook()
	return l, err
}

func TestPullRefusesTamperedObject(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	a.put(t, "s1", "TOKEN", "good")
	b.put(t, "s2", "OTHER", "mine")
	must(t, a.Push(ctx))
	// The store returns other bytes than the ones the directory names.
	rid := currentRID(t, a)
	forged := append(bytes.Clone(magic), []byte("forged")...)
	b.Store = swapped{Store: r.store, rid: rid, obj: forged}
	if err := b.Pull(ctx); !errors.Is(err, ErrTampered) {
		t.Fatalf("want ErrTampered, got %v", err)
	}
	if _, err := b.vault.Get("s1"); !errors.Is(err, keys.ErrNotFound) {
		t.Fatal("the local vault must be untouched by a refused pull")
	}
	if b.value(t, "s2") != "mine" {
		t.Fatal("the local vault must keep its own secrets")
	}
}

func currentRID(t *testing.T, m *machine) string {
	l, err := m.Dir.List(ctx)
	must(t, err)
	return l.Identity.Vault
}

// swapped answers one rid with chosen bytes, as a damaged or hostile store would.
type swapped struct {
	blobstore.Store
	rid string
	obj []byte
}

func (s swapped) Get(ctx context.Context, rid string) ([]byte, error) {
	if rid == s.rid {
		return s.obj, nil
	}
	return s.Store.Get(ctx, rid)
}

func TestPushRetriesWhenStoreDown(t *testing.T) {
	r := newRig(t)
	a := r.machine()
	a.put(t, "s1", "TOKEN", "abc")
	r.store.FailAfter(0, blobstore.ErrUnreachable)
	if err := a.Push(ctx); !errors.Is(err, blobstore.ErrUnreachable) {
		t.Fatalf("want ErrUnreachable, got %v", err)
	}
	if currentRID(t, a) != "" {
		t.Fatal("a failed push must not move the directory")
	}
	must(t, a.Push(ctx)) // the next flush succeeds
	if currentRID(t, a) == "" {
		t.Fatal("the retry must publish")
	}
}

func TestPushOfAnUnchangedVaultStillSettles(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	a.put(t, "s1", "TOKEN", "abc")
	must(t, a.Push(ctx))
	must(t, b.Pull(ctx))
	must(t, b.Push(ctx))
	must(t, a.Push(ctx))
	if a.value(t, "s1") != "abc" {
		t.Fatal("secret lost")
	}
}

// --- Inject ---

func newCell(t *testing.T) cell.Cell {
	t.Helper()
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	must(t, err)
	return c
}

func (m *machine) putIn(t *testing.T, c cell.Cell, name, value string) {
	t.Helper()
	must(t, m.vault.Put("id-"+name, keys.Entry{Name: name, Value: value, Scope: keys.ScopeOf(c)}))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	must(t, err)
	return string(raw)
}

func TestInjectWritesEnvInStableOrderAtMode0600(t *testing.T) {
	m, c := newRig(t).machine(), newCell(t)
	m.putIn(t, c, "ZED", "z")
	m.putIn(t, c, "A0", "a0")
	m.putIn(t, c, "A", "a")
	must(t, m.Inject(ctx, c))
	path := filepath.Join(c.Root, ".env")
	if got, want := readFile(t, path), "A=a\nA0=a0\nZED=z\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}

func TestInjectNeedsTheCellKey(t *testing.T) {
	m, c := newRig(t).machine(), newCell(t)
	m.putIn(t, c, "TOKEN", "abc")
	m.CellKeyID = "" // a machine with no identity
	err := m.Inject(ctx, c)
	if !errors.Is(err, ErrNoIdentity) || strings.Contains(err.Error(), "\n") {
		t.Fatalf("want one clear sentence, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Root, ".env")); !os.IsNotExist(err) {
		t.Fatal("no .env may be written without an identity")
	}
}

func TestInjectNeverOverwritesThePersonsLines(t *testing.T) {
	m, c := newRig(t).machine(), newCell(t)
	m.putIn(t, c, "MINE", "vault-mine")
	m.putIn(t, c, "NEW", "vault-new")
	path := filepath.Join(c.Root, ".env")
	written := "# my file\nMINE=person\nOTHER=1" // no trailing newline
	must(t, os.WriteFile(path, []byte(written), 0o644))
	var said []string
	m.Notify = func(s string) { said = append(said, s) }

	in, err := m.Apply(ctx, c)
	must(t, err)
	if got, want := readFile(t, path), written+"\nNEW=vault-new\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.Join(in.Written, ",") != "NEW" || strings.Join(in.Skipped, ",") != "MINE" {
		t.Fatalf("report %+v", in)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o644 {
		t.Fatal("an existing file keeps its mode")
	}
	must(t, m.Inject(ctx, c))
	if len(said) != 1 || !strings.Contains(said[0], "MINE") || strings.Contains(said[0], "NEW") {
		t.Fatalf("Notify must name the skipped names only: %q", said)
	}
}

func TestInjectLeavesAnInSyncEnvAlone(t *testing.T) {
	m, c := newRig(t).machine(), newCell(t)
	m.putIn(t, c, "TOKEN", "abc")
	m.Notify = func(s string) { t.Fatalf("nothing to report, got %q", s) }
	must(t, m.Inject(ctx, c))
	path := filepath.Join(c.Root, ".env")
	first, _ := os.Stat(path)
	time.Sleep(10 * time.Millisecond)
	must(t, m.Inject(ctx, c))
	if again, _ := os.Stat(path); !again.ModTime().Equal(first.ModTime()) {
		t.Fatal("an .env that already holds every name must not be written")
	}
	if readFile(t, path) != "TOKEN=abc\n" {
		t.Fatal("content changed")
	}
}

func TestInjectSkipsAValueThatCannotSitOnOneLine(t *testing.T) {
	m, c := newRig(t).machine(), newCell(t)
	m.putIn(t, c, "KEY", "line1\nline2")
	m.putIn(t, c, "PAD", " padded ")
	in, err := m.Apply(ctx, c)
	must(t, err)
	if strings.Join(in.Skipped, ",") != "KEY" {
		t.Fatalf("report %+v", in)
	}
	got := keys.DotenvValues(readFile(t, filepath.Join(c.Root, ".env")))
	if got["PAD"] != " padded " {
		t.Fatalf("a padded value must round-trip, got %q", got["PAD"])
	}
}

func TestNothingLogsASecretValue(t *testing.T) {
	const secret = "s3cr3t-value-7f3a"
	r := newRig(t)
	a, b, c := r.machine(), r.machine(), newCell(t)
	var out bytes.Buffer
	log.SetOutput(&out)
	defer log.SetOutput(os.Stderr)
	notify := func(s string) { out.WriteString(s + "\n") }
	a.Notify, b.Notify = notify, notify

	a.putIn(t, c, "TOKEN", secret)
	must(t, os.WriteFile(filepath.Join(c.Root, ".env"), []byte("TOKEN=other\n"), 0o600))
	for _, err := range []error{a.Push(ctx), b.Pull(ctx), a.Inject(ctx, c), b.Inject(ctx, c)} {
		if err != nil {
			out.WriteString(err.Error() + "\n")
		}
	}
	b.CellKeyID = ""
	if err := b.Inject(ctx, c); err != nil {
		out.WriteString(err.Error())
	}
	b.Store = swapped{Store: r.store, rid: currentRID(t, a), obj: append(bytes.Clone(magic), secret...)}
	if err := b.Pull(ctx); err != nil {
		out.WriteString(err.Error())
	}
	if out.Len() == 0 {
		t.Fatal("the capture saw nothing, so it proves nothing")
	}
	if strings.Contains(out.String(), secret) {
		t.Fatalf("a secret value leaked: %q", out.String())
	}
}

func TestDeleteTravelsAndIsNeverInjected(t *testing.T) {
	r := newRig(t)
	a, b, c := r.machine(), r.machine(), newCell(t)
	a.putIn(t, c, "LEAKED", "v")
	must(t, a.Push(ctx))
	must(t, b.Pull(ctx))
	must(t, a.vault.Delete("id-LEAKED"))
	must(t, a.Push(ctx))
	must(t, b.Push(ctx)) // b still held the old copy; its push must not resurrect it
	must(t, a.Pull(ctx))
	for _, m := range []*machine{a, b} {
		must(t, m.Pull(ctx))
		must(t, m.Inject(ctx, c))
	}
	if _, err := os.Stat(filepath.Join(c.Root, ".env")); !os.IsNotExist(err) {
		t.Fatal("a deleted secret must never be written to .env")
	}
}
