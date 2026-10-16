package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// unionKey is a conversation's transcript path; its owner is the directory name.
func unionKey(k string) string {
	if k == "" {
		return ""
	}
	return "/s/" + k + "/t.jsonl"
}

func unionTeam(id, parent, manager string, keys ...string) teamstore.Team {
	t := teamstore.Team{ID: id, Parent: parent, Manager: unionKey(manager)}
	for _, k := range keys {
		t.Members = append(t.Members, teamstore.Member{Key: unionKey(k)})
	}
	return t
}

func unionOf(t *testing.T, teams ...teamstore.Team) *appTeamsUnion {
	t.Helper()
	u, err := buildTeamsUnion(context.Background(), teams)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func ownerOf(key string) string { return key }

// A rootless profile has no root group: nothing is invented, and its own team
// ids are the only filters.
func TestUnionRootlessHasNoRootGroup(t *testing.T) {
	closed := unionTeam("c", "", "", "kc")
	closed.State = teamstore.TeamClosed
	u := unionOf(t, unionTeam("a", "", "ka", "ka", "k1"), unionTeam("a1", "a", "k2", "k2"), closed)
	if u.Root != "" || len(u.Tops) != 1 || !u.Tops["a"] {
		t.Fatalf("tops %v root %q", u.Tops, u.Root)
	}
	if got := u.Owners[ownerOf("k2")]; len(got) != 1 || got[0] != (appwire.DeliverableTeam{Team: "a1", Top: "a"}) {
		t.Fatalf("descendant membership %v", got)
	}
	if _, ok := u.Owners[ownerOf("kc")]; ok {
		t.Fatal("closed team is in the union")
	}
	if _, err := u.sessions("root"); err == nil {
		t.Fatal("a rootless profile answered for a root")
	}
}

// The global manager belongs to the real root and is not lost; a top manager
// seated in the root is not a several-teams row.
func TestUnionKeepsTheRealRootGroupSeparate(t *testing.T) {
	root := unionTeam("root", "", "kg", "kg", "ka", "kb", "kp")
	root.Root = true
	u := unionOf(t, root, unionTeam("a", "root", "ka", "ka", "k1"), unionTeam("b", "root", "kb", "kb"))
	g := u.Owners[ownerOf("kg")]
	if len(g) != 1 || !g[0].Root || g[0].Team != "root" || g[0].Top != "" || appTeamsAmbiguous(g) {
		t.Fatalf("global manager %v", g)
	}
	top := u.Owners[ownerOf("ka")]
	if len(top) != 2 || !top[0].Root || top[1] != (appwire.DeliverableTeam{Team: "a", Top: "a"}) || appTeamsAmbiguous(top) {
		t.Fatalf("seated top manager %v", top)
	}
	if rels := u.Owners[ownerOf("kp")]; len(rels) != 1 || !rels[0].Root {
		t.Fatalf("root-only member %v", rels)
	}
	if len(u.Tops) != 2 || u.Tops["root"] {
		t.Fatalf("root became a top: %v", u.Tops)
	}
	all, _ := u.sessions("")
	inA, _ := u.sessions("a")
	inRoot, err := u.sessions("root")
	if err != nil || !all[ownerOf("kg")] || inA[ownerOf("kg")] || !inRoot[ownerOf("kg")] || !inRoot[ownerOf("ka")] || inRoot[ownerOf("k1")] {
		t.Fatalf("scopes: all %v a %v root %v err %v", all, inA, inRoot, err)
	}
}

// Two real tops are ambiguous and every membership is kept.
func TestUnionTwoTopsAreAmbiguous(t *testing.T) {
	u := unionOf(t, unionTeam("a", "", "", "x"), unionTeam("b", "", "", "x"), unionTeam("a1", "a", "", "x"))
	rels := u.Owners[ownerOf("x")]
	if len(rels) != 3 || !appTeamsAmbiguous(rels) {
		t.Fatalf("%v", rels)
	}
	var d appwire.Deliverable
	d.Agent = ownerOf("x")
	u.decorate(&d)
	if !d.Ambiguous || len(d.Teams) != 3 {
		t.Fatalf("%+v", d)
	}
}

// Revision follows membership, not names; root creation changes it.
func TestUnionRevision(t *testing.T) {
	a := unionOf(t, unionTeam("a", "", "", "x"))
	renamed := unionTeam("a", "", "", "x")
	renamed.Name = "other"
	if b := unionOf(t, renamed); b.Revision != a.Revision {
		t.Fatal("a rename changed the revision")
	}
	if b := unionOf(t, unionTeam("a", "", "", "x", "y")); b.Revision == a.Revision {
		t.Fatal("a join did not change the revision")
	}
	root := unionTeam("r", "", "", "g")
	root.Root = true
	if b := unionOf(t, root, unionTeam("a", "r", "", "x")); b.Revision == a.Revision {
		t.Fatal("a real root did not change the revision")
	}
}

func TestUnionRefusesInconsistency(t *testing.T) {
	root := unionTeam("r", "", "", "g")
	root.Root = true
	closedRoot := root
	closedRoot.State = teamstore.TeamClosed
	cases := map[string][]teamstore.Team{
		"duplicate":   {unionTeam("a", "", "", "x"), unionTeam("a", "", "", "y")},
		"missing":     {unionTeam("a", "zz", "", "x")},
		"cycle":       {unionTeam("a", "b", "", "x"), unionTeam("b", "a", "", "y")},
		"manager":     {unionTeam("a", "", "nobody", "x")},
		"beside root": {root, unionTeam("a", "", "", "x")},
		"closed root": {closedRoot, unionTeam("a", "r", "", "x")},
		"no id":       {unionTeam("", "", "", "x")},
	}
	for name, teams := range cases {
		if _, err := buildTeamsUnion(context.Background(), teams); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestUnionBoundsAndDeadline(t *testing.T) {
	var teams []teamstore.Team
	for i := 0; i < appTeamsOwnersMax+1; i++ {
		teams = append(teams, unionTeam("t"+string(rune('a'+i%26))+string(rune('a'+i/26%26))+string(rune('a'+i/676)), "", "", "k"+strconv.Itoa(i)))
	}
	var wire *appwire.Error
	if _, err := buildTeamsUnion(context.Background(), teams); !errors.As(err, &wire) || wire.Code != appwire.CodeTooLarge {
		t.Fatalf("owner bound: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := buildTeamsUnion(ctx, []teamstore.Team{unionTeam("a", "", "", "x")}); !errors.As(err, &wire) || wire.Code != appwire.CodeBusy {
		t.Fatalf("deadline: %v", err)
	}
}

// A cursor binds membership and team filter and refuses every other.
func TestUnionCursorBindsMembershipAndFilter(t *testing.T) {
	a := unionOf(t, unionTeam("a", "", "", "x"))
	b := unionOf(t, unionTeam("a", "", "", "x", "y"))
	c := a.wrap("a", "d1.inner")
	if got, err := a.unwrap("a", c); err != nil || got != "d1.inner" {
		t.Fatalf("round trip %q %v", got, err)
	}
	for name, bad := range map[string]func() error{
		"other membership": func() error { _, err := b.unwrap("a", c); return err },
		"other filter":     func() error { _, err := a.unwrap("", c); return err },
		"plain index":      func() error { _, err := a.unwrap("a", "d1.inner"); return err },
		"garbage":          func() error { _, err := a.unwrap("a", "t1.!!"); return err },
	} {
		if err := bad(); !errors.Is(err, session.ErrMediaCursor) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if a.wrap("a", "") != "" {
		t.Fatal("a last page got a cursor")
	}
}

// The scope end to end: twelve owners (past the per-owner search bound), a
// root, a loose agent that must stay out, pages of one that reach every row,
// a filter by one top, and a teams file that is byte-for-byte unchanged.
func TestTeamsScopePagesEveryOwnerAndWritesNothing(t *testing.T) {
	backend, _, _ := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })

	var a, b []string
	for i := 0; i < 6; i++ {
		a = append(a, "a"+strconv.Itoa(i))
		b = append(b, "b"+strconv.Itoa(i))
	}
	root := unionTeam("root", "", "g", append([]string{"g"}, "a0", "b0")...)
	root.Root = true
	teams := []teamstore.Team{root, unionTeam("ta", "root", "a0", a...), unionTeam("tb", "root", "b0", b...)}
	profile := t.TempDir()
	backend.profileDir = profile
	raw, err := json.Marshal(map[string]any{"version": teamstore.Version, "teams": teams})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(teamstore.Path(profile), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{}
	for _, owner := range append(append([]string{"g", "loose"}, a...), b...) {
		doc := filepath.Join(t.TempDir(), owner+".md")
		os.WriteFile(doc, []byte("# "+owner+"\n"), 0o600)
		fact := session.DeliverableFact{ID: session.NewDeliverableID(), At: time.Now(), Origin: session.DeliverOriginAgent, Authored: true, Run: "r", Turn: "s#1"}
		if !session.RecordArtifact(index, session.Artifact{Path: doc, Session: owner, Title: owner + ".md", Kind: "markdown", Created: time.Now(), Deliver: &fact}) {
			t.Fatal("record")
		}
		if owner != "loose" {
			want[owner] = true
		}
	}

	walk := func(crew string) (map[string]appwire.Deliverable, string) {
		got := map[string]appwire.Deliverable{}
		cursor, revision := "", ""
		for pages := 0; pages < 40; pages++ {
			page, err := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: appwire.ScopeTeams, Crew: crew, Limit: 1, Cursor: cursor})
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range page.Deliverables {
				got[d.Agent] = d
			}
			if revision != "" && page.Revision != revision {
				t.Fatalf("revision moved within a walk")
			}
			revision = page.Revision
			if page.Next == "" {
				return got, revision
			}
			cursor = page.Next
		}
		t.Fatal("never ended")
		return nil, ""
	}
	got, revision := walk("")
	if len(got) != len(want) {
		t.Fatalf("got %d owners, want %d: %v", len(got), len(want), got)
	}
	for owner := range want {
		if _, ok := got[owner]; !ok {
			t.Errorf("lost %s", owner)
		}
	}
	if _, ok := got["loose"]; ok {
		t.Error("a loose agent is in the union")
	}
	if d := got["g"]; len(d.Teams) != 1 || !d.Teams[0].Root || d.Ambiguous {
		t.Errorf("global manager row %+v", d)
	}
	if d := got["a0"]; len(d.Teams) != 2 || d.Ambiguous {
		t.Errorf("top manager row %+v", d)
	}
	if sub, _ := walk("ta"); len(sub) != 6 || sub["b1"].Agent != "" || sub["g"].Agent != "" {
		t.Errorf("top ta subtree %v", sub)
	}
	count, err := backend.CountDeliverables(appwire.DeliverablesQuery{Scope: appwire.ScopeTeams})
	if err != nil || count.Revision != revision || count.Count != len(want) {
		t.Errorf("count %+v err %v", count, err)
	}
	if _, err := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: appwire.ScopeTeams, Crew: "nope"}); err == nil {
		t.Error("an unknown top answered")
	}
	if after, _ := os.ReadFile(teamstore.Path(profile)); string(after) != string(raw) {
		t.Error("the teams file was changed")
	}
}
