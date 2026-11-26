package pair

import (
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// C2: the permit book is remote control's own file in a home, written on the
// computer that offers a pairing and on the one that joins. It must never make
// that home count as having local work: a computer that was paired, and then
// left alone, still has to be able to join another set of computers, and a home
// that was only started must still look unused after the book is written.
func TestThePermitBookNeverMakesAHomeLookUsed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.EnvVar, dir)
	if _, err := identity.EnsureSolo(dir); err != nil {
		t.Fatal(err)
	}
	// The computer that offers writes the empty legacy snapshot the moment its
	// solo state ends, through the same call the app and the terminal make.
	if err := EndSoloAt(dir, Permits()); err != nil {
		t.Fatal(err)
	}
	id, err := identity.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	// And a choice the person made about one computer, so the book holds rows
	// as well as the snapshot.
	yes := true
	if err := Permits().Set(id.ID(), "another-computer", &yes, &yes, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Permits().path); err != nil {
		t.Fatalf("the permit book was not written, so this proves nothing: %v", err)
	}
	if ok, err := identity.Pristine(dir); err != nil || !ok {
		t.Fatalf("the permit book made a home that was only started look used: pristine=%v err=%v", ok, err)
	}
	if ok, err := identity.Joinable(dir); err != nil || !ok {
		t.Fatalf("the permit book blocked a join: joinable=%v err=%v", ok, err)
	}
}

// The joining computer writes its book after it has kept the grant, under the
// identity it joined. That must not count either: the same home can be removed
// from the set later and join another.
func TestAComputerThatJoinedStillLooksUnusedWithItsBook(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.EnvVar, dir)
	id, err := identity.Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	stampJoin(id.ID())
	if _, err := os.Stat(Permits().path); err != nil {
		t.Fatalf("the join did not write the book, so this proves nothing: %v", err)
	}
	if ok, err := identity.Joinable(dir); err != nil || !ok {
		t.Fatalf("the book a join wrote blocked the next join: joinable=%v err=%v", ok, err)
	}
}
