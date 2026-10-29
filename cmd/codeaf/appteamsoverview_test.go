package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/teams/teamstest"
)

// overviewRig is a real appwire server over an appBackend whose profile's store
// holds the given teams (in the old file's JSON shape), spoken to by its
// registered method table.
type overviewRig struct {
	t       *testing.T
	backend *appBackend
	profile string
	in      io.WriteCloser
	lines   *bufio.Scanner
	id      int
	hello   map[string]any
}

func newOverviewRig(t *testing.T, teamsJSON string) *overviewRig {
	t.Helper()
	backend := newFakeBackend(newAppRecorder(), nil)
	backend.profileDir = t.TempDir()
	rig := &overviewRig{t: t, backend: backend, profile: backend.profileDir}
	if teamsJSON != "" {
		rig.write(teamsJSON)
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := appwire.NewServer(outW)
	done := make(chan struct{})
	go func() {
		_ = server.Serve(context.Background(), inR, appwire.Hello{}, backend)
		outW.Close()
		close(done)
	}()
	t.Cleanup(func() { inW.Close(); <-done })
	rig.in, rig.lines = inW, bufio.NewScanner(outR)
	rig.lines.Buffer(nil, 4<<20)
	var line struct {
		Data map[string]any `json:"data"`
	}
	rig.line(&line)
	rig.hello = line.Data
	return rig
}

func (r *overviewRig) line(into any) {
	r.t.Helper()
	if !r.lines.Scan() {
		r.t.Fatalf("the server's output ended: %v", r.lines.Err())
	}
	if err := json.Unmarshal(r.lines.Bytes(), into); err != nil {
		r.t.Fatalf("not JSON: %q", r.lines.Text())
	}
}

func (r *overviewRig) write(body string) {
	r.t.Helper()
	teamstest.Write(r.t, r.profile, []byte(body))
}

// call sends crews.overview and answers the raw result text and the error.
func (r *overviewRig) call(params string) (string, *appwire.Error) {
	r.t.Helper()
	r.id++
	if _, err := fmt.Fprintf(r.in, `{"id":%d,"method":"crews.overview","params":%s}`+"\n", r.id, params); err != nil {
		r.t.Fatal(err)
	}
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  *appwire.Error  `json:"error"`
	}
	r.line(&reply)
	return string(reply.Result), reply.Error
}

func (r *overviewRig) page(params string) (appwire.Overview, string) {
	r.t.Helper()
	raw, rpcErr := r.call(params)
	if rpcErr != nil {
		r.t.Fatalf("%s answered %v", params, rpcErr)
	}
	var o appwire.Overview
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		r.t.Fatal(err)
	}
	return o, raw
}

func (r *overviewRig) refuses(params, code, words string) {
	r.t.Helper()
	raw, rpcErr := r.call(params)
	if rpcErr == nil || rpcErr.Code != code || !strings.Contains(rpcErr.Message, words) {
		r.t.Fatalf("%s answered %s / %v, want %s %q", params, raw, rpcErr, code, words)
	}
}

func ovMember(key, handle, word string) teamstore.Member {
	return teamstore.Member{Key: key, File: key, Where: "/w", Word: word, Handle: handle}
}

func ovTeams(t *testing.T, teams ...teamstore.Team) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"version": teamstore.Version, "teams": teams})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// ovTree is a root with a manager over two top teams, one nested team and a
// closed one; "shared" belongs to two teams and "dup" is stored twice in one.
// beta stores no manager.
func ovTree(t *testing.T) string {
	made := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	root := teamstore.Team{ID: "root", Name: teamstore.RootName, Root: true, Made: made,
		Members: []teamstore.Member{ovMember("/s/boss/transcript.jsonl", "boss", "boss")}, Manager: "/s/boss/transcript.jsonl"}
	alpha := teamstore.Team{ID: "alpha", Name: "alpha", Parent: "root", Made: made.Add(2 * time.Hour), Manager: "/s/a1/transcript.jsonl",
		Members: []teamstore.Member{ovMember("/s/a1/transcript.jsonl", "ann", "lead"), ovMember("/s/shared/transcript.jsonl", "sam", "web"),
			ovMember("/s/dup/transcript.jsonl", "d", "x"), ovMember("/s/dup/transcript.jsonl", "d2", "x")}}
	alpha.SetHue(teamstore.HueSpec{Hue: 0, Tier: 1})
	beta := teamstore.Team{ID: "beta", Name: "beta", Parent: "root", Made: made.Add(time.Hour),
		Members: []teamstore.Member{ovMember("/s/shared/transcript.jsonl", "sam", "web")}}
	nested := teamstore.Team{ID: "nested", Name: "nested", Parent: "alpha", Made: made}
	gone := teamstore.Team{ID: "gone", Name: "gone", Parent: "alpha", Made: made, State: teamstore.TeamClosed, ClosedAt: made}
	return ovTeams(t, root, alpha, beta, nested, gone)
}

// THE REGISTERED METHOD, END TO END: hello names the feature, the root is the
// file's real root with its real manager, its children carry the root's id as
// their parent, counts are direct and unique, and nothing unknown is said.
func TestCrewsOverviewServesTheRootAndItsTopTeamsOverTheWire(t *testing.T) {
	rig := newOverviewRig(t, ovTree(t))
	features, _ := rig.hello["features"].([]any)
	if !strings.Contains(fmt.Sprint(features), appwire.FeatureCrewsOverview) {
		t.Fatalf("hello features = %v", features)
	}
	o, raw := rig.page(`{}`)
	if o.Kind != "teams" || o.Parent != "root" || o.Total != 2 || o.Next != "" || len(o.Members) != 0 || o.Revision == "" || o.Revision == teamstore.MissingStamp {
		t.Fatalf("overview = %+v", o)
	}
	if o.Root == nil || !o.Root.Root || o.Root.Manager == nil || o.Root.Manager.Handle != "boss" || o.Root.ChildCount != 2 || o.Root.MemberCount != 1 {
		t.Fatalf("root = %+v", o.Root)
	}
	if o.Teams[0].ID != "alpha" || o.Teams[1].ID != "beta" || o.Teams[0].Parent != "root" {
		t.Fatalf("teams = %+v", o.Teams)
	}
	alpha, beta := o.Teams[0], o.Teams[1]
	if alpha.MemberCount != 3 || alpha.ChildCount != 1 || alpha.Manager == nil || alpha.Manager.Handle != "ann" || alpha.Manager.Title != "lead" || alpha.Hue == nil {
		t.Fatalf("alpha = %+v", alpha)
	}
	if beta.MemberCount != 1 || beta.ChildCount != 0 || beta.Manager != nil || beta.Hue != nil {
		t.Fatalf("beta = %+v (a team with no stored manager or colour says nothing)", beta)
	}
	// The face is the manager first, then distinct stored members, at most 3:
	// alpha's duplicate key is one face, beta's lone member is its face, and
	// the root's face is its manager.
	faces := func(s appwire.TeamSummary) (out []string) {
		for _, m := range s.FaceMembers {
			out = append(out, m.Handle)
		}
		return out
	}
	if got := faces(alpha); fmt.Sprint(got) != "[ann sam d]" || alpha.FaceMembers[0].Agent != alpha.Manager.Agent {
		t.Fatalf("alpha face = %v", got)
	}
	if got := faces(beta); fmt.Sprint(got) != "[sam]" || fmt.Sprint(faces(*o.Root)) != "[boss]" {
		t.Fatalf("beta face = %v, root face = %v", got, faces(*o.Root))
	}
	for _, unknown := range []string{"spend", "activity", "latest", "unread", "usd"} {
		if strings.Contains(raw, unknown) {
			t.Fatalf("the answer says %q, which the file does not hold: %s", unknown, raw)
		}
	}
	if beta.MadeAt == "" || strings.Contains(raw, `"teams":null`) || strings.Contains(raw, `"members":null`) {
		t.Fatalf("raw = %s", raw)
	}
	nestedPage, _ := rig.page(`{"parent":"alpha"}`)
	if nestedPage.Parent != "alpha" || nestedPage.Total != 1 || nestedPage.Teams[0].ID != "nested" || nestedPage.Root == nil {
		t.Fatalf("nested = %+v", nestedPage)
	}
	noCrewRun(t, rig.backend)
}

// noCrewRun fails when a read started the crews' watch.
func noCrewRun(t *testing.T, backend *appBackend) {
	t.Helper()
	var run *appCrewRun
	backend.locked(func() { run = backend.crewRun })
	if run != nil {
		t.Fatal("the overview started the crews' watch")
	}
}

// MEMBERS PAGE ONE TEAM'S DIRECT MEMBERS, once each; the root's are not listed
// and an unknown or closed team is refused.
func TestCrewsOverviewMembersAreOneTeamsDirectMembersOnce(t *testing.T) {
	rig := newOverviewRig(t, ovTree(t))
	o, raw := rig.page(`{"kind":"members","parent":"alpha","limit":2}`)
	if o.Kind != "members" || o.Total != 3 || len(o.Members) != 2 || o.Next == "" || len(o.Teams) != 0 || !strings.Contains(raw, `"teams":[]`) {
		t.Fatalf("first page = %s", raw)
	}
	if o.Members[0].Handle != "ann" || o.Members[1].Handle != "sam" || o.Members[0].Agent == "" {
		t.Fatalf("members = %+v", o.Members)
	}
	rest, _ := rig.page(fmt.Sprintf(`{"kind":"members","parent":"alpha","limit":2,"cursor":%q}`, o.Next))
	if len(rest.Members) != 1 || rest.Members[0].Handle != "d" || rest.Next != "" {
		t.Fatalf("second page = %+v", rest)
	}
	// A member two teams share is a member of each, once.
	beta, _ := rig.page(`{"kind":"members","parent":"beta"}`)
	if beta.Total != 1 || beta.Members[0].Agent != o.Members[1].Agent {
		t.Fatalf("beta = %+v", beta)
	}
	rig.refuses(`{"kind":"members","parent":"root"}`, appwire.CodeBadParams, "root's members")
	rig.refuses(`{"kind":"members"}`, appwire.CodeBadParams, "needs the team")
	rig.refuses(`{"kind":"members","parent":"nope"}`, appwire.CodeNotFound, "no team")
	rig.refuses(`{"parent":"gone"}`, appwire.CodeBadParams, "closed")
	noCrewRun(t, rig.backend)
}

// A CURSOR BELONGS TO THE REVISION, KIND AND PARENT IT WAS CUT FROM.
func TestCrewsOverviewCursorIsBoundToTheRevisionTheKindAndTheParent(t *testing.T) {
	rig := newOverviewRig(t, ovTree(t))
	first, _ := rig.page(`{"limit":1}`)
	if first.Next == "" || len(first.Teams) != 1 || first.Total != 2 {
		t.Fatalf("first = %+v", first)
	}
	cursor := fmt.Sprintf(`"cursor":%q`, first.Next)
	second, _ := rig.page(`{"limit":1,` + cursor + `}`)
	if len(second.Teams) != 1 || second.Teams[0].ID != "beta" || second.Next != "" {
		t.Fatalf("second = %+v", second)
	}
	rig.refuses(`{"limit":1,"kind":"members","parent":"alpha",`+cursor+`}`, appwire.CodeBadParams, "another page")
	rig.refuses(`{"limit":1,"parent":"alpha",`+cursor+`}`, appwire.CodeBadParams, "another page")
	rig.refuses(`{"cursor":"%%%"}`, appwire.CodeBadParams, "not one a previous page gave")
	rig.refuses(`{"cursor":"`+strings.Repeat("a", appwire.OverviewCursorMax+1)+`"}`, appwire.CodeBadParams, "cursor")
	// A change between pages is stale, whatever it changed.
	rig.write(strings.Replace(ovTree(t), `"name":"beta"`, `"name":"beta2"`, 1))
	rig.refuses(`{"limit":1,`+cursor+`}`, appwire.CodeStaleCursor, "stale cursor")
	again, _ := rig.page(`{"limit":1}`)
	if again.Revision == first.Revision {
		t.Fatal("the revision did not follow the write")
	}
}

// LIMITS AND KINDS ARE CHECKED BEFORE THE STORE IS READ.
func TestCrewsOverviewChecksLimitKindAndParentShape(t *testing.T) {
	rig := newOverviewRig(t, ovTree(t))
	for _, bad := range []string{`{"limit":0}`, `{"limit":-1}`, `{"limit":101}`, `{"limit":"x"}`} {
		rig.refuses(bad, appwire.CodeBadParams, "limit")
	}
	rig.refuses(`{"kind":"people"}`, appwire.CodeBadParams, "kind")
	rig.refuses(`{"parent":"`+strings.Repeat("p", appwire.OverviewParentMax+1)+`"}`, appwire.CodeBadParams, "parent")
	rig.refuses(`{"parent":"nope"}`, appwire.CodeNotFound, "no team")
	if o, _ := rig.page(`{"limit":100}`); o.Total != 2 {
		t.Fatalf("limit 100 = %+v", o)
	}
}

// WITHOUT A ROOT THE TOP IS PARENT "", THE ROOT IS NULL, AND NO MANAGER IS
// INVENTED: a file with a few top teams and nothing else.
func TestCrewsOverviewWithoutARootHasNullRootAndNoGlobalManager(t *testing.T) {
	made := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	rig := newOverviewRig(t, ovTeams(t,
		teamstore.Team{ID: "a", Name: "a", Made: made},
		teamstore.Team{ID: "b", Name: "b", Made: made}, // same moment: the id decides
		teamstore.Team{ID: "c", Name: "c", Parent: "a", Made: made}))
	o, raw := rig.page(`{}`)
	if o.Root != nil || !strings.Contains(raw, `"root":null`) || o.Parent != "" || o.Total != 2 || o.Teams[0].ID != "a" || o.Teams[1].ID != "b" || o.Teams[0].ChildCount != 1 {
		t.Fatalf("overview = %s", raw)
	}
	if o.Teams[0].Manager != nil || o.Teams[0].MemberCount != 0 || o.Teams[0].Parent != "" || o.Teams[0].FaceMembers != nil || strings.Contains(raw, "face_members") {
		t.Fatalf("a = %+v (a team with no members has no face)", o.Teams[0])
	}
}

// THE STORE IS READ AS IT IS: missing is empty, anything else that is wrong is
// said, the answer carries no path, and nothing caps how large it may be.
func TestCrewsOverviewReadsTheStoreHonestly(t *testing.T) {
	t.Run("missing is empty", func(t *testing.T) {
		rig := newOverviewRig(t, "")
		o, raw := rig.page(`{}`)
		if o.Total != 0 || o.Root != nil || o.Parent != "" || !strings.Contains(raw, `"teams":[]`) || !strings.Contains(raw, `"members":[]`) {
			t.Fatalf("empty = %s", raw)
		}
		if _, err := os.Stat(filepath.Join(rig.profile, teamstore.FileName)); !os.IsNotExist(err) {
			t.Fatalf("the read made the store: %v", err)
		}
	})
	t.Run("not a database", func(t *testing.T) {
		rig := newOverviewRig(t, "")
		if err := os.WriteFile(filepath.Join(rig.profile, teamstore.FileName), []byte(`{"version":2,"teams":[`), 0o600); err != nil {
			t.Fatal(err)
		}
		rig.refuses(`{}`, appwire.CodeEngine, "unreadable")
		rig.refuses(`{"kind":"members","parent":"a"}`, appwire.CodeEngine, "unreadable")
	})
	t.Run("read error is not empty", func(t *testing.T) {
		rig := newOverviewRig(t, "")
		if err := os.Mkdir(filepath.Join(rig.profile, teamstore.FileName), 0o700); err != nil {
			t.Fatal(err)
		}
		raw, rpcErr := rig.call(`{}`)
		if rpcErr == nil || rpcErr.Code != appwire.CodeEngine || strings.Contains(rpcErr.Message, rig.profile) {
			t.Fatalf("a directory in the store's place answered %s / %v", raw, rpcErr)
		}
	})
	// The JSON file this store replaced was refused past 2 MB, which a few
	// factory briefs reached. Briefs are rows of their own now, and a store
	// holding megabytes of them pages like any other.
	t.Run("no size cap", func(t *testing.T) {
		rig := newOverviewRig(t, "")
		big := strings.Repeat("brief words ", 300_000) // ~3.6 MB
		if err := teamstore.Save(rig.profile, []teamstore.Team{
			{ID: "a", Name: "a", Made: time.Now(), Members: []teamstore.Member{{Key: "/s/a/transcript.jsonl", Handle: "aa", FactoryStage: true, FactoryBrief: big}}},
		}); err != nil {
			t.Fatal(err)
		}
		o, raw := rig.page(`{}`)
		if o.Total != 1 || o.Teams[0].MemberCount != 1 || len(raw) > 4096 {
			t.Fatalf("a store with a large brief answered %d bytes: %+v", len(raw), o)
		}
	})
	// There is no migration: a teams.json the store replaced is not read, and
	// the read leaves it and its directory as they were.
	t.Run("a leftover teams.json", func(t *testing.T) {
		rig := newOverviewRig(t, "")
		old := filepath.Join(rig.profile, "teams.json")
		if err := os.WriteFile(old, []byte(`{"version":2,"teams":[{"id":"old","name":"old","made":"2026-01-01T00:00:00Z"}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if o, _ := rig.page(`{}`); o.Total != 0 {
			t.Fatalf("the old file was read: %+v", o)
		}
		if _, err := os.Stat(old); err != nil {
			t.Fatalf("the read moved the old file: %v", err)
		}
		if _, err := os.Stat(filepath.Join(rig.profile, teamstore.FileName)); !os.IsNotExist(err) {
			t.Fatalf("the read made a store: %v", err)
		}
	})
}

// A CANCELLED OR EXPIRED CONTEXT ENDS THE READ AND NOTHING IS ANSWERED.
func TestCrewsOverviewHonoursItsContext(t *testing.T) {
	rig := newOverviewRig(t, ovTree(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if o, err := rig.backend.CrewsOverview(ctx, appwire.OverviewAsk{}); err == nil || o != nil || ctx.Err() == nil {
		t.Fatalf("a cancelled context answered %+v %v", o, err)
	}
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	if _, err := rig.backend.CrewsOverview(expired, appwire.OverviewAsk{}); err == nil {
		t.Fatal("an expired context was answered")
	}
}

// THE READ CHANGES NOTHING: the store's rows, revision and time, and the
// directory's listing, are what they were, and no watch or crew was started.
func TestCrewsOverviewWritesNothingAndStartsNothing(t *testing.T) {
	made := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	// Untidy rows a Load would repair: a manager who is not a member.
	rig := newOverviewRig(t, ovTeams(t, teamstore.Team{ID: "alpha", Name: "alpha", Made: made, Manager: "/s/ghost/transcript.jsonl",
		Members: []teamstore.Member{ovMember("/s/a1/transcript.jsonl", "", "lead")}}))
	path := filepath.Join(rig.profile, teamstore.FileName)
	before, stamp := teamstest.JSON(t, rig.profile), teamstore.Stamp(rig.profile)
	stat, _ := os.Stat(path)
	names := func() string {
		entries, _ := os.ReadDir(rig.profile)
		var out []string
		for _, e := range entries {
			out = append(out, e.Name())
		}
		return strings.Join(out, ",")
	}
	listing := names()
	for _, params := range []string{`{}`, `{"parent":"alpha"}`, `{"kind":"members","parent":"alpha"}`, `{"cursor":"x"}`} {
		rig.call(params)
	}
	if after := teamstest.JSON(t, rig.profile); string(after) != string(before) || teamstore.Stamp(rig.profile) != stamp {
		t.Fatal("the rows changed")
	}
	if now, _ := os.Stat(path); !now.ModTime().Equal(stat.ModTime()) || names() != listing {
		t.Fatalf("the store's time or the directory changed: %s -> %s", listing, names())
	}
	noCrewRun(t, rig.backend)
}
