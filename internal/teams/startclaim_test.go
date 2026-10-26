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

func TestAppendClaimedStartKeepsTrafficAndClaimTogether(t *testing.T) {
	profile := t.TempDir()
	must(t, Update(profile, func(f *File) error {
		f.Teams = append(f.Teams, Team{ID: "t1", Name: "site"})
		return nil
	}))
	id, err := AppendClaimedStart(profile, "t1", Entry{Kind: KindStart, From: FromManager, To: "scout", Text: "check the work"})
	must(t, err)
	entries, err := ReadTraffic(profile, "t1", "", 10)
	must(t, err)
	if id == "" || len(entries) != 1 || entries[0].ID != id || entries[0].Text != "check the work" {
		t.Fatalf("returned start does not match traffic: id=%q entries=%+v", id, entries)
	}
	f, err := Load(profile)
	must(t, err)
	if f.ClaimStart("t1", id) {
		t.Fatal("another window claimed the appended start")
	}
}

func TestAppendClaimedStartRejectsInvalidEntriesWithoutTraffic(t *testing.T) {
	for _, entry := range []Entry{
		{Kind: KindNote, From: FromManager, To: "scout"},
		{Kind: KindStart, From: "scout", To: "helper"},
		{Kind: KindStart, From: FromManager, To: FromManager},
		{Kind: KindStart, From: FromManager, To: "bad handle"},
	} {
		t.Run(entry.Kind+"/"+entry.From+"/"+entry.To, func(t *testing.T) {
			profile := t.TempDir()
			must(t, Update(profile, func(f *File) error {
				f.Teams = append(f.Teams, Team{ID: "t1", Name: "site"})
				return nil
			}))
			if id, err := AppendClaimedStart(profile, "t1", entry); err == nil || id != "" {
				t.Fatalf("invalid start accepted: id=%q err=%v", id, err)
			}
			entries, err := ReadTraffic(profile, "t1", "", 10)
			must(t, err)
			if len(entries) != 0 {
				t.Fatalf("invalid start wrote traffic: %+v", entries)
			}
			f, err := Load(profile)
			must(t, err)
			team, _ := f.Team("t1")
			if len(team.Starts) != 0 {
				t.Fatalf("invalid start wrote claims: %+v", team.Starts)
			}
		})
	}
}
