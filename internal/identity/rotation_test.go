package identity

import "testing"

// A minted identity shares no secret and no id with another, and it is never
// built from a vault key lying in a home.
func TestMintIsAllNew(t *testing.T) {
	a, err := Mint()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Mint()
	if a.ID() == b.ID() || a.CellKeyID() == b.CellKeyID() || string(a.DedupSecret()) == string(b.DedupSecret()) {
		t.Fatal("two minted identities share a secret")
	}
}

// Install makes the identity and its device the machine's, repeatably, and
// refuses a device that another identity certified.
func TestInstallIsRepeatableAndChecksTheCert(t *testing.T) {
	home := t.TempDir()
	id, _ := Mint()
	dev, err := NewDevice(id)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Install(home, id, dev); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Device(home)
	if err != nil || got.ID() != dev.ID() {
		t.Fatalf("device after install = %v, %v", got.ID(), err)
	}
	other, _ := Mint()
	if err := Install(t.TempDir(), other, dev); err == nil {
		t.Fatal("installed a device another identity certified")
	}
}

// A device survives a journal: marshalled and read back it signs the same.
func TestDeviceMarshalRoundTrip(t *testing.T) {
	id, _ := Mint()
	dev, _ := NewDevice(id)
	raw, err := dev.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := UnmarshalDev(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID() != dev.ID() || string(back.Sign([]byte("m"))) != string(dev.Sign([]byte("m"))) {
		t.Fatal("the device changed in the journal")
	}
}

// The list of replaced identities keeps order, takes each once and is bounded.
func TestPredecessorsAreRecordedOnceAndBounded(t *testing.T) {
	home := t.TempDir()
	var made []string
	for range MaxPredecessors + 2 {
		id, _ := Mint()
		made = append(made, id.ID())
		for range 2 {
			if err := RecordPredecessor(home, id.ID()); err != nil {
				t.Fatal(err)
			}
		}
	}
	got := Predecessors(home)
	if len(got) != MaxPredecessors || got[0] != made[2] || got[len(got)-1] != made[len(made)-1] {
		t.Fatalf("predecessors = %v", got)
	}
	if RecordPredecessor(home, "../etc") == nil {
		t.Fatal("recorded something that is not an identity id")
	}
}
