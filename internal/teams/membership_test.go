package teams

import "testing"

func TestRemovingTheReportingMembershipPreservesIndependenceUntilExplicitReassignment(t *testing.T) {
	dir := t.TempDir()
	f := managed()
	must(t, f.AddMember("aaaaaaaaaaaa", Member{Key: "worker", Word: "A worker"}))
	must(t, Save(dir, f.Teams))
	f, err := Load(dir)
	must(t, err)
	must(t, f.AddMember("dddddddddddd", Member{Key: "worker", Word: "A worker"}))
	must(t, Save(dir, f.Teams))
	must(t, f.RemoveMember("aaaaaaaaaaaa", "worker"))
	must(t, Save(dir, f.Teams))
	f, err = Load(dir)
	must(t, err)
	if _, ok := f.Home("worker"); ok {
		t.Fatal("removal silently reassigned reporting authority")
	}
	if !f.Teams[3].Holds("worker") {
		t.Fatal("removal erased the other membership")
	}
	must(t, f.AddMember("aaaaaaaaaaaa", Member{Key: "worker", Word: "A worker"}))
	must(t, Save(dir, f.Teams))
	f, err = Load(dir)
	must(t, err)
	if _, ok := f.Home("worker"); ok {
		t.Fatal("joining revived reporting authority")
	}
	must(t, f.SetHome("worker", "dddddddddddd"))
	must(t, Save(dir, f.Teams))
	f, err = Load(dir)
	must(t, err)
	if home, ok := f.Home("worker"); !ok || home.Team != "dddddddddddd" {
		t.Fatal("explicit reassignment did not survive reload")
	}
}
