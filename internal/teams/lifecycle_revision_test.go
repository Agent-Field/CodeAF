package teams

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestDisbandPreservesOtherMembershipsAndEndsReportingRecursively(t *testing.T) {
	f := &File{Teams: []Team{
		{ID: "aaaaaaaaaaaa", Name: "parent", Manager: "manager", Members: []Member{{Key: "manager"}, {Key: "worker", Home: true}}},
		{ID: "bbbbbbbbbbbb", Name: "child", Parent: "aaaaaaaaaaaa", Manager: "child-manager", Members: []Member{{Key: "child-manager"}, {Key: "worker"}}},
		{ID: "cccccccccccc", Name: "other", Manager: "other-manager", Members: []Member{{Key: "other-manager"}, {Key: "worker"}}},
	}}
	must(t, f.Disband("aaaaaaaaaaaa", time.Unix(100, 0), "report"))
	parent, _ := f.Team("aaaaaaaaaaaa")
	child, _ := f.Team("bbbbbbbbbbbb")
	other, _ := f.Team("cccccccccccc")
	if !parent.Closed() || !child.Closed() || other.Closed() || !other.Holds("worker") || parent.Report != "report" {
		t.Fatalf("incorrect cascade: %+v", f.Teams)
	}
	if _, ok := f.Home("worker"); ok {
		t.Fatal("lost reporting manager was implicitly replaced")
	}
}

func TestConversationDeleteChoicesAreExplicitAndReviewedScopeIsStable(t *testing.T) {
	f := &File{Teams: []Team{
		{ID: "aaaaaaaaaaaa", Name: "parent", Manager: "manager", Members: []Member{{Key: "manager"}, {Key: "replacement"}}},
		{ID: "bbbbbbbbbbbb", Name: "child", Parent: "aaaaaaaaaaaa", Manager: "manager", Members: []Member{{Key: "manager"}}},
	}}
	if f.RemoveConversation("manager", nil, time.Now()) == nil {
		t.Fatal("manager deletion needs explicit choices")
	}
	choices := map[string]string{"aaaaaaaaaaaa": "", "bbbbbbbbbbbb": ""}
	if f.RemoveConversation("manager", choices, time.Now(), map[string][]string{"aaaaaaaaaaaa": {"aaaaaaaaaaaa"}, "bbbbbbbbbbbb": {"bbbbbbbbbbbb"}}) == nil {
		t.Fatal("unreviewed child was disbanded")
	}
	must(t, f.RemoveConversation("manager", choices, time.Now(), map[string][]string{"aaaaaaaaaaaa": {"aaaaaaaaaaaa", "bbbbbbbbbbbb"}, "bbbbbbbbbbbb": {"bbbbbbbbbbbb"}}))
	for _, tm := range f.Teams {
		if !tm.Closed() || tm.Holds("manager") {
			t.Fatalf("cascade did not release manager: %+v", tm)
		}
	}
}

func TestDisbandHistoryRejectsDecisionAndTrafficWrites(t *testing.T) {
	dir := packetTeams(t)
	p, err := Raise(dir, conflict())
	must(t, err)
	before, _ := os.ReadFile(DecisionsPath(dir, p.Origin))
	must(t, Update(dir, func(f *File) error { return f.Disband(p.Origin, time.Now(), "") }))
	if _, err = Decide(dir, p.ID, Person, "1", "late"); !errors.Is(err, ErrClosed) && !errors.Is(err, ErrNotDecider) {
		t.Fatalf("closed decision: %v", err)
	}
	if err = AppendTraffic(dir, p.Origin, Entry{Kind: KindNote, From: FromYou, To: ToRoom, Text: "late"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed traffic: %v", err)
	}
	after, _ := os.ReadFile(DecisionsPath(dir, p.Origin))
	if string(before) != string(after) {
		t.Fatal("decision history changed")
	}
}

func TestConversationDeleteReplacesManagerAndKeepsHistoricalIdentity(t *testing.T) {
	dir := packetTeams(t)
	must(t, Update(dir, func(f *File) error {
		return f.RemoveConversation("dm", map[string]string{"bbbbbbbbbbbb": "w1"}, time.Now())
	}))
	f, err := Load(dir)
	must(t, err)
	child, _ := f.Team("bbbbbbbbbbbb")
	parent, _ := f.Team("aaaaaaaaaaaa")
	if child.Manager != "w1" || child.Closed() || child.Holds("dm") || parent.Holds("dm") {
		t.Fatalf("replacement/removal: %+v", f.Teams)
	}
	if len(child.FormerMembers) != 1 || child.FormerMembers[0].Handle != "lead" {
		t.Fatal("history lost the removed alias")
	}
}

func TestDisbandDeletionAcceptsActiveTeamsAndRejectsChangedScope(t *testing.T) {
	dir := packetTeams(t)
	if _, err := Delete(dir, "aaaaaaaaaaaa", []string{"aaaaaaaaaaaa"}); err == nil {
		t.Fatal("changed scope accepted")
	}
	f, _ := Load(dir)
	if len(f.Teams) != 2 {
		t.Fatal("refusal mutated teams")
	}
	gone, err := Delete(dir, "aaaaaaaaaaaa", []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"})
	must(t, err)
	f, _ = Load(dir)
	if len(gone) != 2 || len(f.Teams) != 0 {
		t.Fatal("active cascade deletion failed")
	}
}

func TestTeamsTrafficRecordsStableIdentitiesBeforeManagerReplacement(t *testing.T) {
	dir := packetTeams(t)
	must(t, AppendTraffic(dir, "bbbbbbbbbbbb", Entry{Kind: KindNote, From: FromManager, To: "web", Text: "before"}))
	must(t, Update(dir, func(f *File) error { return f.SetManager("bbbbbbbbbbbb", "w2") }))
	log, err := ReadTraffic(dir, "bbbbbbbbbbbb", "", 0)
	must(t, err)
	if len(log) != 1 || log[0].FromKey != "dm" || log[0].ToKey != "w1" {
		t.Fatalf("history identities: %+v", log)
	}
}

func TestTeamsClearingManagerKeepsOldHistoryIdentityAmbiguous(t *testing.T) {
	f := &File{Teams: []Team{{ID: "aaaaaaaaaaaa", Manager: "old", Members: []Member{{Key: "old"}, {Key: "new"}}}}}
	must(t, f.ClearManager("aaaaaaaaaaaa"))
	must(t, f.SetManager("aaaaaaaaaaaa", "new"))
	tm, _ := f.Team("aaaaaaaaaaaa")
	if tm.FormerManager != "old" {
		t.Fatal("clearing manager forgot legacy history identity")
	}
}
