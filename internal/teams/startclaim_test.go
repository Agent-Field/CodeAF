package teams

import "testing"

// TWO WINDOWS HOLDING ONE MANAGER START ONE MEMBER: the first claim wins, the
// second is refused, and the claim survives a write and a read.
func TestAStartLineIsClaimedOnce(t *testing.T) {
	profile := t.TempDir()
	id := NewID()
	if err := Update(profile, func(f *File) error {
		f.Teams = append(f.Teams, Team{ID: id, Name: "site"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	claim := func() bool {
		won := false
		if err := Update(profile, func(f *File) error { won = f.ClaimStart(id, "e1"); return nil }); err != nil {
			t.Fatal(err)
		}
		return won
	}
	if !claim() {
		t.Fatal("the first claim was refused")
	}
	if claim() {
		t.Fatal("a second window claimed the same start")
	}
	f, err := Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	if team, _ := f.Team(id); len(team.Starts) != 1 {
		t.Fatalf("the claim was not kept: %+v", team.Starts)
	}
}
