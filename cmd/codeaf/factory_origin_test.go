package main

// A team's factory origin is a stamped fact, never a guess from its name: the
// factory's own doors stamp it, an ordinary crew called `factory` does not
// carry it, a stage's brief is the one it was really started on, and that
// brief is the job's to change, not the person's.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

func TestFactoryDoorsStampTheirTeamsAndNameAloneNeverDoes(t *testing.T) {
	var f teamstore.File
	f.Teams = []teamstore.Team{{ID: "ordinary", Name: "#7 · fix", Parent: "other", Made: time.Now()}}
	parent := factoryParentTeam(&f, time.Now())
	item := itemTeamIn(&f, "#12 · review", time.Now())
	byID := map[string]teamstore.Team{}
	for _, tm := range f.Teams {
		byID[tm.ID] = tm
	}
	if !byID[parent].FactoryManaged || !byID[item].FactoryManaged {
		t.Fatalf("the factory's own teams are unmarked: %+v", f.Teams)
	}
	if byID["ordinary"].FactoryManaged {
		t.Fatal("a team was marked by nothing but a name")
	}
	// Found again, not made again, and still marked.
	if again := itemTeamIn(&f, "#12 · review", time.Now()); again != item || len(f.Teams) != 3 {
		t.Fatalf("second find made %q among %d teams", again, len(f.Teams))
	}
}

func TestAnOrdinaryCrewNamedFactoryReadsUnmarkedOverTheWire(t *testing.T) {
	backend, id, _ := startedWeb(t, &fakeConversation{}, "Own the web client.")
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams[0].Name = factoryTeamName
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	crew, err := backend.CrewGet(id)
	if err != nil || crew.Name != factoryTeamName || crew.FactoryManaged {
		t.Fatalf("crew = %+v, %v", crew, err)
	}
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams[0].FactoryManaged = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if crew, _ := backend.CrewGet(id); !crew.FactoryManaged {
		t.Fatalf("a stamped team reads unmarked: %+v", crew)
	}
}

func TestLegacyRoomReferenceStampsItsTeamAndFactoryParentOnly(t *testing.T) {
	profile := t.TempDir()
	if err := teamstore.Update(profile, func(f *teamstore.File) error {
		f.Teams = []teamstore.Team{
			{ID: "p", Name: factoryTeamName},
			{ID: "room", Name: "#3 · old", Parent: "p"},
			{ID: "lookalike", Name: "#4 · old", Parent: "p"},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := factoryStampRoom(profile, "room"); err != nil {
		t.Fatal(err)
	}
	f, err := teamstore.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, tm := range f.Teams {
		if want := tm.ID != "lookalike"; tm.FactoryManaged != want {
			t.Fatalf("%s managed = %v, want %v", tm.ID, tm.FactoryManaged, want)
		}
	}
}

func TestStageBriefIsTheRecordedOneRedactedAndTheJobOwnsIt(t *testing.T) {
	backend, id, web := startedWeb(t, &fakeConversation{}, "Own the web client.")
	var key string
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		for _, m := range f.Teams[0].Members {
			if m.Handle == "web" {
				key = m.Key
			}
		}
		return nil
	}); err != nil || key == "" {
		t.Fatalf("no web member: %v", err)
	}
	const secret = "sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	brief := "Review the diff. token=" + secret
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		m := &f.Teams[0].Members[0]
		for i := range f.Teams[0].Members {
			if f.Teams[0].Members[i].Key == key {
				m = &f.Teams[0].Members[i]
			}
		}
		m.FactoryStage, m.FactoryBrief = true, brief
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := backend.MemberBrief(id, web)
	if err != nil || !got.FactoryStage || got.From != "" || got.At != nil || len(got.Revisions) != 0 {
		t.Fatalf("stage brief = %+v, %v", got, err)
	}
	if !strings.HasPrefix(got.Text, "Review the diff.") || strings.Contains(got.Text, secret) {
		t.Fatalf("stage brief text = %q", got.Text)
	}
	// Refused before any write, then and after the stage's team closed.
	for round := 0; round < 2; round++ {
		if _, err := backend.MemberBriefSet(id, web, "something else", nil); wireCode(err) != appwire.CodeBadParams {
			t.Fatalf("round %d: set = %v", round, err)
		}
		if after, _ := backend.MemberBrief(id, web); after.Text != got.Text || len(after.Revisions) != 0 {
			t.Fatalf("round %d: a refused edit changed the brief: %+v", round, after)
		}
		if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
			f.Teams[0].State = teamstore.TeamClosed
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStoredRoomsStampOnlyTheTeamsAnItemRecords(t *testing.T) {
	profile, root := t.TempDir(), filepath.Join(t.TempDir(), "factory")
	// No store yet: nothing is made and nothing is stamped.
	factoryStampStoredRooms(profile, root)
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("a missing store was created: %v", err)
	}
	if err := teamstore.Update(profile, func(f *teamstore.File) error {
		f.Teams = []teamstore.Team{
			{ID: "p", Name: factoryTeamName},
			{ID: "room", Name: "#3 · old", Parent: "p", Manager: "/tmp/legacy-talk.jsonl", Members: []teamstore.Member{{File: "/tmp/legacy-talk.jsonl", Key: "/tmp/legacy-talk.jsonl"}, {File: "/tmp/legacy-stage.jsonl", Key: "/tmp/legacy-stage.jsonl"}}},
			{ID: "named", Name: factoryTeamName, Root: true},
			{ID: "other", Name: "#4 · other"},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Repo: "work", Kind: factory.KindIssue, Title: "old", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(it *factory.Item) error {
		it.Talk = "/tmp/legacy-talk.jsonl"
		it.Stream = &factory.Stream{Room: "room", Phases: []factory.Phase{{Chat: "/tmp/legacy-stage.jsonl"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	factoryStampStoredRooms(profile, root)
	f, err := teamstore.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, tm := range f.Teams {
		if want := tm.ID == "room" || tm.ID == "p"; tm.FactoryManaged != want {
			t.Fatalf("%s managed = %v, want %v", tm.ID, tm.FactoryManaged, want)
		}
	}
}

// A resumed stage goes through the real maker: it is not joined a second time,
// and the brief on record stays the opening one, not the carry-on note.
func TestResumedStageKeepsItsOpeningBriefThroughTheRealMaker(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	model := newStageModel(t)
	t.Setenv("CODEAF_BASE_URL", model.server.URL)
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workspace)
	freshEngineProcess(t)
	stageSettle = 20 * time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })
	proc, err := openEngineProcess()
	if err != nil {
		t.Fatal(err)
	}
	launch, err := openV3Launch(proc, engineLaunchOptions(remote.Hello{Version: remote.Version, Workspace: workspace}, workspace, ""))
	if err != nil {
		t.Fatal(err)
	}
	var team string
	if err := teamstore.Update("", func(f *teamstore.File) error {
		team = itemTeamIn(f, "#1 · review", time.Now())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	maker := factoryStageMaker(nil, workspace, "", launch.Config)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	spec := factoryrun.ConversationSpec{Name: "#1 · review", Team: team, Brief: "Look it over. End by calling stage_result once.", Dir: workspace}
	first, err := maker.Open(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	chat := first.ID()
	first.Close()
	spec.Resume, spec.Brief = chat, "Pick up where you stopped."
	second, err := maker.Open(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	second.Close()
	f, err := teamstore.Load("")
	if err != nil {
		t.Fatal(err)
	}
	tm, _ := f.Team(team)
	if len(tm.Members) != 1 || !tm.Members[0].FactoryStage || tm.Members[0].FactoryBrief != "Look it over. End by calling stage_result once." {
		t.Fatalf("members = %+v", tm.Members)
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	last := model.briefs[len(model.briefs)-1]
	if !strings.Contains(last, "[carry on] Pick up where you stopped.") {
		t.Fatalf("the resume did not go through CarryOn: %.400s", last)
	}
}

// Phase.Chat is the stage's transcript path, which is what the stage stored as
// its member's File and (resolved) Key. A legacy member is stamped only when a
// stored item's Room team holds that exact path; its brief stays unknown.
func TestStoredPhaseChatStampsOnlyTheRecordedStageMembers(t *testing.T) {
	backend, id, web := startedWeb(t, &fakeConversation{}, "Own the web client.")
	profile, root := backend.profileDir, filepath.Join(t.TempDir(), "factory")
	dir := resolvedTempDir(t)
	file := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	stage, unknown, kept := file("stage.jsonl"), file("unknown.jsonl"), file("kept.jsonl")
	if err := teamstore.Update(profile, func(f *teamstore.File) error {
		room := &f.Teams[0]
		room.FactoryManaged = true // Crew ownership is known; only the legacy stage-member flags are missing.
		for i := range room.Members {
			if room.Members[i].Handle == web {
				// The plain collider: same name, a path the record never names.
				room.Members[i].File, room.Members[i].Key = unknown, unknown
			}
		}
		room.Members = append(room.Members,
			// The stage as an old build wrote it: the link spelled in File, the real path as Key.
			teamstore.Member{Key: stage, File: filepath.Join(link, "stage.jsonl"), Handle: "legacy"},
			teamstore.Member{Key: kept, File: filepath.Join(link, "kept.jsonl"), Handle: "kept", FactoryStage: true, FactoryBrief: "the real brief"},
		)
		f.Teams = append(f.Teams, teamstore.Team{ID: "elsewhere", Name: "#9 · else", Members: []teamstore.Member{{Key: stage, File: stage, Handle: "legacy"}}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Repo: "work", Kind: factory.KindIssue, Title: "old", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(it *factory.Item) error {
		it.Stream = &factory.Stream{Room: id, Phases: []factory.Phase{
			{Name: "build", Chat: filepath.Join(link, "stage.jsonl")}, {Name: "again", Chat: filepath.Join(link, "kept.jsonl")}, {Name: "none"},
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	factoryStampStoredRooms(profile, root)
	f, err := teamstore.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]teamstore.Member{}
	for _, tm := range f.Teams {
		for _, m := range tm.Members {
			got[tm.ID+"/"+m.Handle] = m
		}
	}
	if m := got[id+"/legacy"]; !m.FactoryStage || m.FactoryBrief != "" {
		t.Fatalf("recorded stage = %+v, want stamped with an empty brief", m)
	}
	if m := got[id+"/kept"]; !m.FactoryStage || m.FactoryBrief != "the real brief" {
		t.Fatalf("stamped brief overwritten: %+v", m)
	}
	if m := got[id+"/"+web]; m.FactoryStage {
		t.Fatalf("same-name member with an unknown path was stamped: %+v", m)
	}
	if m := got["elsewhere/legacy"]; m.FactoryStage {
		t.Fatalf("another room's member was stamped: %+v", m)
	}
	// The stamp is what closes the edit door, and the unknown brief reads empty.
	if _, err := backend.MemberBriefSet(id, appMemberAgent(got[id+"/legacy"]), "invented", nil); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("set = %v", err)
	}
	if b, err := backend.MemberBrief(id, appMemberAgent(got[id+"/legacy"])); err != nil || !b.FactoryStage || b.Text != "" {
		t.Fatalf("brief = %+v, %v", b, err)
	}
}

func TestFactoryStageBriefRedactsBeforeStorage(t *testing.T) {
	profile := t.TempDir()
	var room string
	if err := teamstore.Update(profile, func(f *teamstore.File) error { room = itemTeamIn(f, "#1 · review", time.Now()); return nil }); err != nil {
		t.Fatal(err)
	}
	const secret = "sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := stageJoinTeam(profile, room, "/tmp/stage-redaction.jsonl", "/w", "review", "Review the diff. token="+secret); err != nil {
		t.Fatal(err)
	}
	f, err := teamstore.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	tm, _ := f.Team(room)
	if len(tm.Members) != 1 || !strings.HasPrefix(tm.Members[0].FactoryBrief, "Review the diff.") || strings.Contains(tm.Members[0].FactoryBrief, secret) {
		t.Fatal("opening brief missing or secret persisted in team storage")
	}
}
