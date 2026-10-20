package teams

import (
	"errors"
	"os"
	"testing"
	"time"
)

// leadOnly is a profile holding one team, `solo`, whose one member is its
// manager, beside a second team that has nothing to do with it.
func leadOnly(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must(t, Update(dir, func(f *File) error {
		f.Teams = append(f.Teams, Team{ID: "aaaaaaaaaaaa", Name: "solo"}, Team{ID: "cccccccccccc", Name: "yard"})
		must(t, f.AddMember("aaaaaaaaaaaa", Member{Key: "lead", File: "lead", Word: "the lead"}))
		must(t, f.AddMember("cccccccccccc", Member{Key: "lead", File: "lead", Word: "the lead"}))
		must(t, f.AddMember("cccccccccccc", Member{Key: "y1", File: "y1", Word: "yard work"}))
		return f.SetManager("aaaaaaaaaaaa", "lead")
	}))
	return dir
}

// A TEAM THAT NEVER HELD ANYONE BUT ITS MANAGER IS FORGOTTEN WHOLE: its record
// and its directory go, and the manager's other memberships stay.
func TestDissolveForgetsATeamThatOnlyEverHeldItsManager(t *testing.T) {
	dir := leadOnly(t)
	must(t, os.MkdirAll(TeamDir(dir, "aaaaaaaaaaaa"), 0o700))
	must(t, Dissolve(dir, "aaaaaaaaaaaa", "lead"))
	f, err := Load(dir)
	must(t, err)
	if _, ok := f.Team("aaaaaaaaaaaa"); ok {
		t.Fatal("the dissolved team is still in the file")
	}
	if len(f.ClosedTeams()) != 0 {
		t.Fatal("dissolving left a disbanded row behind")
	}
	yard, ok := f.Team("cccccccccccc")
	if !ok || !yard.Holds("lead") {
		t.Fatal("dissolving touched the manager's other membership")
	}
	if _, err := os.Stat(TeamDir(dir, "aaaaaaaaaaaa")); !os.IsNotExist(err) {
		t.Fatalf("the team's directory survived: %v", err)
	}
}

// EVERY SIGN THAT THE TEAM WAS USED SHUTS THE DOOR, and a refusal writes
// nothing: a second member, a removed member, a former manager, a team under
// it, a line of Traffic, a disband, and a caller that is not its manager.
func TestDissolveRefusesATeamThatWasUsed(t *testing.T) {
	cases := []struct {
		name    string
		used    func(t *testing.T, dir string)
		manager string
	}{
		{"a second member", func(t *testing.T, dir string) {
			must(t, Update(dir, func(f *File) error { return f.AddMember("aaaaaaaaaaaa", Member{Key: "web", File: "web"}) }))
		}, "lead"},
		{"a member since removed", func(t *testing.T, dir string) {
			must(t, Update(dir, func(f *File) error { return f.AddMember("aaaaaaaaaaaa", Member{Key: "web", File: "web"}) }))
			must(t, Update(dir, func(f *File) error { return f.RemoveMember("aaaaaaaaaaaa", "web") }))
		}, "lead"},
		{"a team under it", func(t *testing.T, dir string) {
			must(t, Update(dir, func(f *File) error {
				f.Teams = append(f.Teams, Team{ID: "bbbbbbbbbbbb", Name: "under", Parent: "aaaaaaaaaaaa"})
				return nil
			}))
		}, "lead"},
		{"a line of traffic", func(t *testing.T, dir string) {
			must(t, AppendTraffic(dir, "aaaaaaaaaaaa", Entry{Kind: KindNote, From: FromManager, To: ToEveryone, Text: "hello"}))
		}, "lead"},
		{"a disband", func(t *testing.T, dir string) {
			must(t, Update(dir, func(f *File) error { return f.Disband("aaaaaaaaaaaa", time.Time{}, "") }))
		}, "lead"},
		{"another conversation asking", func(t *testing.T, dir string) {}, "y1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := leadOnly(t)
			c.used(t, dir)
			before, err := os.ReadFile(Path(dir))
			must(t, err)
			if err := Dissolve(dir, "aaaaaaaaaaaa", c.manager); err == nil {
				t.Fatal("a used team was dissolved")
			}
			after, err := os.ReadFile(Path(dir))
			must(t, err)
			if string(before) != string(after) {
				t.Fatal("a refused dissolve wrote the teams file")
			}
		})
	}
}

// THE ROOT IS NEVER DISSOLVABLE, even holding only its global manager.
func TestTheRootIsNotDissolvable(t *testing.T) {
	f := &File{Teams: []Team{{ID: "rrrrrrrrrrrr", Name: "All teams", Root: true, Manager: "g", Members: []Member{{Key: "g"}}}}}
	if f.Dissolvable("rrrrrrrrrrrr") {
		t.Fatal("the root reads as dissolvable")
	}
}

// A TEAM ANOTHER WRITER FILLED SINCE THE CALLER LOOKED is refused under the
// store's lock, with the error a caller can name.
func TestDissolveSeesAMemberAddedSinceTheCallerLooked(t *testing.T) {
	dir := leadOnly(t)
	f, err := Load(dir)
	must(t, err)
	if !f.Dissolvable("aaaaaaaaaaaa") {
		t.Fatal("the fixture is not dissolvable")
	}
	must(t, Update(dir, func(f *File) error { return f.AddMember("aaaaaaaaaaaa", Member{Key: "web", File: "web"}) }))
	if err := Dissolve(dir, "aaaaaaaaaaaa", "lead"); !errors.Is(err, ErrNotDissolvable) {
		t.Fatalf("dissolve answered %v, want ErrNotDissolvable", err)
	}
}
