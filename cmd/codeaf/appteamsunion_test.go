package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/teams/teamstest"
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
	teamstest.Write(t, profile, raw)
	stamp := teamstore.Stamp(profile)

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
	if teamstore.Stamp(profile) != stamp {
		t.Error("the teams store was changed")
	}
}

// pollCtx is a context that reports itself cancelled from its cancelAt-th look
// on, and counts every look after that one, so a test can see exactly where a
// build noticed cancellation and that it did no more work past it.
type pollCtx struct {
	context.Context
	cancelAt, polls, after int
}

func (c *pollCtx) Err() error {
	c.polls++
	if c.cancelAt > 0 && c.polls >= c.cancelAt {
		if c.polls > c.cancelAt {
			c.after++
		}
		return context.Canceled
	}
	return nil
}

// reverseChain is depth teams, each listed before its parent, so a team's
// ancestors all come after it in the file: the order that made validation
// cubic. Team 0 is the deepest; the last is the top. Every 25th team holds one
// member, which keeps a deep chain inside the owner and pair bounds.
func reverseChain(depth int) []teamstore.Team {
	teams := make([]teamstore.Team, depth)
	for i := range teams {
		parent := ""
		if i+1 < depth {
			parent = "c" + strconv.Itoa(i+1)
		}
		teams[i] = unionTeam("c"+strconv.Itoa(i), parent, "")
		if i%25 == 0 {
			teams[i] = unionTeam("c"+strconv.Itoa(i), parent, "", "k"+strconv.Itoa(i))
		}
	}
	return teams
}

// A deep chain with no cancellation is read whole: every team is reached
// from the one top, and every member is held against it. Validated by asking
// each team's ancestry, 20000 teams took hours.
func TestUnionDeepReverseChainIsReachable(t *testing.T) {
	const depth = 20000
	u := unionOf(t, reverseChain(depth)...)
	top := "c" + strconv.Itoa(depth-1)
	if len(u.Tops) != 1 || !u.Tops[top] || len(u.Owners) != depth/25 {
		t.Fatalf("tops %d owners %d", len(u.Tops), len(u.Owners))
	}
	if rels := u.Owners["k0"]; len(rels) != 1 || rels[0] != (appwire.DeliverableTeam{Team: "c0", Top: top}) {
		t.Fatalf("deepest member %v", rels)
	}
}

// Cancellation that arrives while a deep chain is being validated ends the
// build there, as busy, with no further look at the context. The chain ends in
// a team under a missing parent, which only a validation that runs to the end
// finds: the build that ignored ctx until validation was done answered that
// instead of busy, after all of the work. The cancellation point is half of
// the looks a full validation takes, measured, not a timing guess.
func TestUnionDeepChainStopsWhenCancelled(t *testing.T) {
	teams := append(reverseChain(50000), unionTeam("stray", "gone", "", "ks"))
	var wire *appwire.Error
	full := &pollCtx{Context: context.Background()}
	if _, err := buildTeamsUnion(full, teams); !errors.As(err, &wire) || wire.Code != appwire.CodeEngine {
		t.Fatalf("uncancelled: %v", err)
	}
	if full.polls < 8 {
		t.Fatalf("validation looked at its context %d times over %d teams", full.polls, len(teams))
	}
	cut := &pollCtx{Context: context.Background(), cancelAt: full.polls / 2}
	if _, err := buildTeamsUnion(cut, teams); !errors.As(err, &wire) || wire.Code != appwire.CodeBusy {
		t.Fatalf("cancelled mid-validation: %v", err)
	}
	if cut.polls != cut.cancelAt || cut.after != 0 {
		t.Fatalf("kept working after cancellation: %d looks, cancelled at %d", cut.polls, cut.cancelAt)
	}
}

// The store never keeps an open team under a closed or archived one, so such
// a file is refused, not read as though the open team's people were absent.
// A closed team under a closed one is the store's own shape and is accepted.
func TestUnionOpenTeamUnderClosedAncestorIsInconsistent(t *testing.T) {
	mid := unionTeam("mid", "a", "", "km")
	mid.State = teamstore.TeamClosed
	archived := mid
	archived.ArchivedAt = time.Now()
	for name, parent := range map[string]teamstore.Team{"closed": mid, "archived": archived} {
		_, err := buildTeamsUnion(context.Background(), []teamstore.Team{
			unionTeam("a", "", "ka", "ka"), parent, unionTeam("leaf", "mid", "", "kl"),
		})
		var wire *appwire.Error
		if !errors.As(err, &wire) || wire.Code != appwire.CodeEngine || !strings.Contains(err.Error(), "open team leaf sits under closed team mid") {
			t.Errorf("%s parent: %v", name, err)
		}
	}
	leaf := unionTeam("leaf", "mid", "", "kl")
	leaf.State = teamstore.TeamClosed
	u := unionOf(t, unionTeam("a", "", "ka", "ka"), mid, leaf)
	if len(u.Owners) != 1 || u.Owners["ka"] == nil {
		t.Fatalf("closed subtree: %v", u.Owners)
	}
}

// Every cycle is refused, naming the first team on it in file order (as the
// per-team check did), even when a walk into it starts from a team outside.
func TestUnionCycleAndMissingParentAreRefused(t *testing.T) {
	cases := []struct {
		name  string
		teams []teamstore.Team
		says  string
	}{
		{"self", []teamstore.Team{unionTeam("a", "a", "", "x")}, "team a is under itself"},
		{"tail into a later cycle", []teamstore.Team{
			unionTeam("tail", "p", "", "x"), unionTeam("q1", "q2", "", "y"), unionTeam("q2", "q1", "", "z"),
			unionTeam("p", "pp", "", "w"), unionTeam("pp", "p", "", "v"),
		}, "team q1 is under itself"},
		{"long cycle", append(reverseChain(3000)[:2999], unionTeam("c2999", "c0", "", "k")), "team c0 is under itself"},
		{"missing parent on an appended leaf", append(reverseChain(3000), unionTeam("x", "c1500x", "", "kx")), "team x sits under c1500x, which is not there"},
		{"missing parent partway up a chain", brokenChain(3000, 1500), "team c1500 sits under nowhere, which is not there"},
	}
	for _, c := range cases {
		_, err := buildTeamsUnion(context.Background(), c.teams)
		if err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// brokenChain is a reverse chain whose team at cut names a parent that is not
// there, so every walk from below climbs into the gap.
func brokenChain(depth, cut int) []teamstore.Team {
	teams := reverseChain(depth)
	teams[cut].Parent = "nowhere"
	return teams
}

// The revision's records cannot run into each other: two tops "p" and "q"
// and one top whose id spells their old NUL-and-newline join used to hash
// alike.
func TestUnionRevisionRecordsAreUnambiguous(t *testing.T) {
	two := unionOf(t, teamstore.Team{ID: "p"}, teamstore.Team{ID: "q"})
	one := unionOf(t, teamstore.Team{ID: "p\x00\nt\x00q"})
	if two.Revision == one.Revision {
		t.Fatal("two memberships share a revision")
	}
	owners := unionOf(t, unionTeam("a", "", "", "x\x00y"))
	other := unionOf(t, unionTeam("a", "", "", "x"), teamstore.Team{ID: "y"})
	if owners.Revision == other.Revision {
		t.Fatal("an owner with a NUL collided")
	}
}
