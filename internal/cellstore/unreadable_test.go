package cellstore

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/inventory"
)

// skipIfRoot skips a test that needs a file the process cannot read: root reads
// a file with no permissions.
func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads files whatever their mode")
	}
}

// lookAndNote is one seal's screening and the record step that follows it.
func (r *guardRig) lookAndNote(changed ...string) Screened {
	r.t.Helper()
	got := r.look(changed...)
	if err := noteLeftOut(r.cell, r.tree, got, nil); err != nil {
		r.t.Fatal(err)
	}
	return got
}

func (r *guardRig) recorded() []inventory.Withheld {
	r.t.Helper()
	inv, err := inventory.Open(r.cell.Root)
	if err != nil {
		r.t.Fatal(err)
	}
	return inv.Snapshot().Withheld
}

func (r *guardRig) chmod(rel string, mode os.FileMode) {
	r.t.Helper()
	if err := os.Chmod(filepath.Join(r.tree, rel), mode); err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { os.Chmod(filepath.Join(r.tree, rel), 0o755) })
}

func TestUnreadableFileIsSetApartAndNamed(t *testing.T) {
	skipIfRoot(t)
	r := newRig(t)
	r.write("odd.bin", "x")
	r.write("src/app.py", "print('hi')\n")
	r.chmod("odd.bin", 0)

	got := r.lookAndNote()

	if p := r.policy(); !reflect.DeepEqual(p.unreadable, []string{"odd.bin"}) || len(p.withheld) != 0 {
		t.Fatalf("policy unreadable %v withheld %v, want only odd.bin unreadable", p.unreadable, p.withheld)
	}
	if want := []Apart{{"odd.bin", reasonUnreadable}}; !reflect.DeepEqual(got.Apart, want) {
		t.Fatalf("apart %+v, want %+v", got.Apart, want)
	}
	want := []inventory.Withheld{{Path: "odd.bin", Reason: reasonUnreadable}}
	if rec := r.recorded(); !reflect.DeepEqual(rec, want) {
		t.Fatalf("record %+v, want %+v", rec, want)
	}
	if len(r.notice) != 1 || !strings.Contains(r.notice[0], "odd.bin cannot be read here") {
		t.Fatalf("notices %q", r.notice)
	}
	r.lookAndNote()
	if len(r.notice) != 1 {
		t.Fatalf("a path already set apart was announced again: %q", r.notice)
	}
}

func TestFixedModePutsTheFileBackAtTheNextLook(t *testing.T) {
	skipIfRoot(t)
	r := newRig(t)
	r.write("odd.bin", "x")
	r.chmod("odd.bin", 0)
	r.lookAndNote("odd.bin")
	r.chmod("odd.bin", 0o644)

	r.lookAndNote("src/other.go")

	if p := r.policy(); len(p.unreadable) != 0 {
		t.Fatalf("still unreadable after the mode was fixed: %v", p.unreadable)
	}
	if rec := r.recorded(); len(rec) != 0 {
		t.Fatalf("record still names %+v", rec)
	}
}

func TestUnreadableFolderIsNamedByItsOwnPath(t *testing.T) {
	skipIfRoot(t)
	r := newRig(t)
	r.write("locked/a.txt", "a")
	r.write("open/b.txt", "b")
	r.chmod("locked", 0)

	r.lookAndNote()

	if got := r.policy().unreadable; !reflect.DeepEqual(got, []string{"locked"}) {
		t.Fatalf("unreadable %v, want [locked]", got)
	}
}

func TestSecretAndUnreadableKeepTheirOwnBlocks(t *testing.T) {
	skipIfRoot(t)
	r := newRig(t)
	r.write(".env", "K="+fakeKey+"\n")
	r.write("odd.bin", "x")
	r.chmod("odd.bin", 0)
	r.lookAndNote()
	p := r.policy()
	if !reflect.DeepEqual(p.withheld, []string{".env"}) || !reflect.DeepEqual(p.unreadable, []string{"odd.bin"}) {
		t.Fatalf("withheld %v unreadable %v", p.withheld, p.unreadable)
	}
}

// answering is a Transport that answers materialize with one fixed body.
type answering string

func (a answering) Do(context.Context, Target, Op) ([]byte, error) { return []byte(a), nil }

func TestMaterializeRecordsHeldNamesAndLaterLooksKeepThem(t *testing.T) {
	r := newRig(t)
	e := syncEngineOver(answering(`{"snapshot":"s","held":[{"path":"Readme.md","reason":"same name as README.md here"}]}`))
	e.Target = func(c cell.Cell) Target { return Target{Tree: r.tree, CellDir: r.state()} }
	if err := e.Materialize(context.Background(), r.cell, "h"); err != nil {
		t.Fatal(err)
	}
	if got := r.policy().held; !reflect.DeepEqual(got, []string{"Readme.md"}) {
		t.Fatalf("held block %v", got)
	}
	want := []inventory.Withheld{{Path: "Readme.md", Reason: "same name as README.md here"}}
	if rec := r.recorded(); !reflect.DeepEqual(rec, want) {
		t.Fatalf("record %+v, want %+v", rec, want)
	}

	r.write("README.md", "x")
	got := r.lookAndNote()

	if !reflect.DeepEqual(r.policy().held, []string{"Readme.md"}) {
		t.Fatalf("a seal changed the held block: %v", r.policy().held)
	}
	if rec := r.recorded(); !reflect.DeepEqual(rec, want) {
		t.Fatalf("the specific reason was lost: %+v", rec)
	}
	if len(got.Apart) != 1 || got.Apart[0].Path != "Readme.md" {
		t.Fatalf("apart %+v", got.Apart)
	}
}

func TestMaterializeWithoutHeldNamesWritesNothing(t *testing.T) {
	r := newRig(t)
	e := syncEngineOver(answering(`{"snapshot":"s"}`))
	e.Target = func(c cell.Cell) Target { return Target{Tree: r.tree, CellDir: r.state()} }
	if err := e.Materialize(context.Background(), r.cell, "h"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.state(), policyName)); !os.IsNotExist(err) {
		t.Fatalf("a policy file appeared: %v", err)
	}
}

func TestPolicyRoundTripsEveryBlock(t *testing.T) {
	p := policyFile{
		user:       []string{"exclude mine"},
		withheld:   []string{".env"},
		rebuilt:    []string{"node_modules"},
		unreadable: []string{"odd.bin"},
		held:       []string{"Readme.md"},
	}
	got := parsePolicy(p.String())
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("round trip %+v, want %+v\n%s", got, p, p.String())
	}
	if again := p.holding([]string{"Zed", "Readme.md"}); !reflect.DeepEqual(again.held, []string{"Readme.md", "Zed"}) {
		t.Fatalf("holding %v", again.held)
	}
}

// wiping is a Transport that does what a real restore does to the cell's own
// folder: the sender's policy file and record replace this machine's.
type wiping struct {
	r    *guardRig
	body string
}

func (w wiping) Do(context.Context, Target, Op) ([]byte, error) {
	_ = os.Remove(filepath.Join(w.r.state(), policyName))
	_ = inventory.Record(w.r.cell.Root, func(inv *inventory.Inventory) { inv.Withheld = nil })
	return []byte(w.body), nil
}

func TestAWarmTakeKeepsTheNamesAnEarlierTakeSetApart(t *testing.T) {
	r := newRig(t)
	target := func(c cell.Cell) Target { return Target{Tree: r.tree, CellDir: r.state()} }
	first := syncEngineOver(answering(`{"snapshot":"s","held":[{"path":"Readme.md","reason":"same name as README.md here"}]}`))
	first.Target = target
	if err := first.Materialize(context.Background(), r.cell, "h1"); err != nil {
		t.Fatal(err)
	}

	warm := syncEngineOver(wiping{r, `{"snapshot":"s2"}`})
	warm.Target = target
	if err := warm.Materialize(context.Background(), r.cell, "h2"); err != nil {
		t.Fatal(err)
	}

	if got := r.policy().held; !reflect.DeepEqual(got, []string{"Readme.md"}) {
		t.Fatalf("the earlier name was forgotten: %v", got)
	}
	want := []inventory.Withheld{{Path: "Readme.md", Reason: "same name as README.md here"}}
	if rec := r.recorded(); !reflect.DeepEqual(rec, want) {
		t.Fatalf("record %+v, want %+v", rec, want)
	}
}
