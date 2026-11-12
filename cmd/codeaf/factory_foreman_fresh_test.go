package main

// THE FOREMAN OUT OF THE CODEAF HOME, STARTED OVER, AND TAKEN OUT OF CLOSED.
//
// The Factory manager the desk made on one Mac worked in the codeaf home
// itself: every save swept nineteen gigabytes and its tool calls waited behind
// a capture that never finished, so it answered no question at all. It could
// also be archived, after which its one door opened a chat nobody could write
// to. These pin the three repairs: a foreman in a refused folder is made again
// in its own; factory.foreman_fresh starts one over; and the door, which is
// the person's own open, takes a put-away manager back out.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// THE APP'S STOP IS BOUNDED ONLY WHILE THE REAL CONVERSATION CAN BE: the app
// reaches a conversation through a remote agent, and a rename of the method
// on either side would restore the stop that did nothing, with every test on
// a fake still passing ([appStopByPerson]).
var _ appBoundedStop = (*remote.Agent)(nil)

// foremanEventually waits for a thing the engine does off the call (putting
// the replaced foreman away, settling the foreman as the factory starts).
func foremanEventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("%s never happened", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// homeForeman makes a foreman the way the older build made it: working in dir
// (the codeaf home), named by the store, in the `factory` team, and WITHOUT
// the foreman's mark, which did not exist then.
func homeForeman(t *testing.T, floor *store.Store, profile, dir string) string {
	t.Helper()
	bucket, err := v3ProjectDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	place, err := v3MintSession(bucket, dir, v3StampLaunchDir(v3LaunchDir(), dir), false)
	if err != nil {
		t.Fatal(err)
	}
	old := place.Transcript()
	if err := session.SeedConversation(old, dir, foremanTitle, "the older brief"); err != nil {
		t.Fatal(err)
	}
	if err := foremanJoinTeam(profile, old, dir); err != nil {
		t.Fatal(err)
	}
	if err := floor.SetForeman(old); err != nil {
		t.Fatal(err)
	}
	return old
}

// foremanHandleHolder is the conversation key holding `@foreman` in the
// profile's teams, "" for none, and how many members hold it.
func foremanHandleHolder(t *testing.T, profile string) (key string, holders int) {
	t.Helper()
	f, err := teams.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, team := range f.Teams {
		for _, m := range team.Members {
			if m.Handle == foremanHandle {
				key, holders = m.Key, holders+1
			}
		}
	}
	return key, holders
}

func teamKey(transcript string) string {
	key := transcript
	if real, err := filepath.EvalSymlinks(transcript); err == nil {
		key = real
	}
	return filepath.Clean(key)
}

func TestAForemanWorkingInTheCodeafHomeIsMadeAgainInAFolderOfItsOwn(t *testing.T) {
	state, floor := chatFolderHome(t)
	profile := t.TempDir()

	// The foreman as the older build made it: in the engine's own folder,
	// which under the desk is the codeaf home.
	old := homeForeman(t, floor, profile, state)

	maker := foremanMaker(floor, factoryHostWorkspace(state), profile, nil)
	made, err := maker(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sameSessionFile(made, old) {
		t.Fatal("the foreman was opened where it was, in the codeaf home")
	}
	meta, err := session.LoadMetaIn(session.DirOf(made), "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(floor.Root(), "chats", "foreman"); meta.Workspace != want {
		t.Fatalf("the foreman made again works in %q; want %q", meta.Workspace, want)
	}
	if named, _ := floor.Foreman(); !sameSessionFile(named, made) {
		t.Fatalf("the store still names %q as its foreman", named)
	}
	if key, holders := foremanHandleHolder(t, profile); holders != 1 || key != teamKey(made) {
		t.Fatalf("@foreman is held by %q (%d holders); want the new conversation alone", key, holders)
	}
	// ITS HISTORY IS NOT DELETED: the owner of the conversation puts it away.
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("the conversation it replaced is gone from disk: %v", err)
	}
	// THE MARK FOLLOWS THE FOREMAN: the new one is the factory's on disk, so a
	// relaunch reads it back as the factory's, and the old one is the
	// person's own chat again.
	if !factoryOwnedConversation(made) {
		t.Error("the foreman made again is not marked the factory's on disk")
	}
	if factoryOwnedConversation(old) {
		t.Error("the conversation it replaced is still marked the factory's")
	}
	// AND IT NO LONGER READS AS THE MANAGER: a tab the app brings back may
	// still hold it, and under the manager's name a person wrote to it there.
	if was, err := session.LoadMeta(session.DirOf(old)); err != nil || was.Title != foremanEarlierTitle {
		t.Errorf("the conversation it replaced is titled %q (%v); want %q", was.Title, err, foremanEarlierTitle)
	}
	if now, err := session.LoadMeta(session.DirOf(made)); err != nil || now.Title != foremanTitle {
		t.Errorf("the foreman made again is titled %q (%v)", now.Title, err)
	}
	// AND THE NEXT ASK OPENS THE SAME ONE, as every ask after a making does.
	if again, err := maker(context.Background()); err != nil || !sameSessionFile(again, made) {
		t.Fatalf("the next open answered %q, %v; want %q", again, err, made)
	}
}

func TestForemanFreshStartsTheManagerOver(t *testing.T) {
	st, web, profile := talkLab(t)
	first, err := foremanMaker(st, web, profile, nil)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := foremanFresh(st, web, profile, nil)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sameSessionFile(first, fresh) {
		t.Fatal("a fresh foreman is the conversation it was")
	}
	data, err := os.ReadFile(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), foremanOpening) {
		t.Errorf("the fresh foreman lacks its brief:\n%s", data)
	}
	if key, holders := foremanHandleHolder(t, profile); holders != 1 || key != teamKey(fresh) {
		t.Fatalf("@foreman is held by %q (%d holders); want the fresh conversation alone", key, holders)
	}
	// THE FLOOR IS THE FRESH ONE'S AND NO LONGER THE OLD ONE'S: the tool
	// follows the store's mark, so the conversation put away reads nothing.
	if foremanFloorDoor(st, web, profile, fresh) == nil {
		t.Error("the fresh foreman has no factory_floor")
	}
	if foremanFloorDoor(st, web, profile, first) != nil {
		t.Error("the replaced foreman still carries factory_floor")
	}
	if !factoryOwnedConversation(fresh) || factoryOwnedConversation(first) {
		t.Errorf("the mark did not follow the foreman: fresh %v, replaced %v", factoryOwnedConversation(fresh), factoryOwnedConversation(first))
	}
	if again, err := foremanMaker(st, web, profile, nil)(context.Background()); err != nil || !sameSessionFile(again, fresh) {
		t.Fatalf("the open after a fresh start answered %q, %v; want the fresh one", again, err)
	}
}

// A MAKING CUT SHORT LEAVES NO CONVERSATION HIDDEN FOR GOOD. Between the
// store naming the new foreman and the old one being retired there is a
// moment a crash can fall into: the old one then still carries the mark, so
// every reading calls it the factory's, no list shows it and no door opens
// it. The sweep finds it where a replaced foreman is always left and retires
// it, and never touches the one the store names.
func TestAForemanLeftMarkedByAMakingCutShortIsRetired(t *testing.T) {
	st, web, profile := talkLab(t)
	first, err := foremanMaker(st, web, profile, nil)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := foremanFresh(st, web, profile, nil)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := foremanSweep(st, profile); len(got) != 0 {
		t.Fatalf("a finished making left %v for the sweep", got)
	}
	// As the crash leaves it: the old one's retiring never ran.
	if err := markFactoryForeman(st, first); err != nil {
		t.Fatal(err)
	}
	if !factoryOwnedConversation(first) {
		t.Fatal("the old foreman is not marked; the premise is gone")
	}
	got := foremanSweep(st, profile)
	if len(got) != 1 || !sameSessionFile(got[0], first) {
		t.Fatalf("the sweep retired %v; want the old foreman alone", got)
	}
	if factoryOwnedConversation(first) || !factoryOwnedConversation(fresh) {
		t.Fatalf("after the sweep: old marked %v, current marked %v", factoryOwnedConversation(first), factoryOwnedConversation(fresh))
	}
	if again := foremanSweep(st, profile); len(again) != 0 {
		t.Fatalf("a second sweep retired %v", again)
	}
}

// THE BRIEF NO LONGER FENCES THE MANAGER INTO ONE TOOL. It was told to answer
// "only from" factory_floor, so it could not use what the app tells it with
// each message, nor answer anything a plain assistant can.
func TestTheForemansBriefLetsItAnswerBeyondTheFloor(t *testing.T) {
	for _, want := range []string{"factory_floor", "tagged afi-now", "tagged app-places", "is the person, never you", "your other tools", "only when the person asks", "Never stop, ship, merge or post"} {
		if !strings.Contains(foremanOpening, want) {
			t.Errorf("the brief lacks %q: %s", want, foremanOpening)
		}
	}
	if strings.Contains(foremanOpening, "only from it") {
		t.Errorf("the brief still fences every answer into the floor: %s", foremanOpening)
	}
	// The journal escapes these, and the brief is found in it by its words.
	if strings.ContainsAny(foremanOpening, `<>"&`) {
		t.Errorf("the brief carries a character the journal escapes: %s", foremanOpening)
	}
}

func TestTheFactoryManagersDoorReopensItAndFreshPutsTheOldOneAway(t *testing.T) {
	s := newLocalSetup(t)
	convs := foremanFakeOpens(s)
	st := s.start()
	ref := map[string]any{"factory": st.Factory}

	var first appwire.FactoryForeman
	s.must(appwire.FeatureFactoryForemanOpen, ref, &first)
	if !s.backend.isHeld(first.Agent) {
		t.Fatal("the manager is not held open")
	}

	// PUT AWAY BY THE PERSON, then opened by its own button: it comes back.
	if err := s.backend.Archive([]string{first.Agent}); err != nil {
		t.Fatal(err)
	}
	if !s.backend.tidied(first.Agent) || s.backend.isHeld(first.Agent) {
		t.Fatal("the archive did not put the manager away; the premise is gone")
	}
	var again appwire.FactoryForeman
	s.must(appwire.FeatureFactoryForemanOpen, ref, &again)
	if again.Agent != first.Agent {
		t.Fatalf("the door answered %q after an archive; want the same manager %q", again.Agent, first.Agent)
	}
	if s.backend.tidied(first.Agent) || !s.backend.isHeld(first.Agent) {
		t.Fatal("the manager's own door left it archived")
	}

	// STARTED OVER: a new manager, held open; the old one stopped and archived.
	var fresh appwire.FactoryForeman
	s.must(appwire.FeatureFactoryForemanFresh, ref, &fresh)
	if fresh.Agent == "" || fresh.Agent == first.Agent {
		t.Fatalf("a fresh start answered %q; want a new conversation", fresh.Agent)
	}
	if !s.backend.isHeld(fresh.Agent) || s.backend.tidied(fresh.Agent) {
		t.Fatal("the fresh manager is not held open")
	}
	// The old one is put away off the call, so the new one is answered first.
	foremanEventually(t, "the replaced manager being stopped and archived", func() bool {
		return s.backend.tidied(first.Agent) && !s.backend.isHeld(first.Agent) && convs.stops(first.Agent) > 0
	})
	// ITS NEW NAME WENT THROUGH THE CONVERSATION, where an open reads a title
	// from: left in the folder's row alone it read "Factory manager" again the
	// first time the conversation was opened.
	if got := convs.renamedTo(first.Agent); got != foremanEarlierTitle {
		t.Errorf("the replaced manager's conversation was renamed %q; want %q", got, foremanEarlierTitle)
	}
	var after appwire.FactoryForeman
	s.must(appwire.FeatureFactoryForemanOpen, ref, &after)
	if after.Agent != fresh.Agent {
		t.Fatalf("the door after a fresh start answered %q; want %q", after.Agent, fresh.Agent)
	}
	// THE NEW ONE IS THE FACTORY'S AND THE OLD ONE IS THE PERSON'S AGAIN, in
	// Archived with its history, never hidden where no list shows it.
	for id, want := range map[string]bool{fresh.Agent: true, first.Agent: false} {
		a, err := s.backend.find(id)
		if err != nil {
			t.Fatal(err)
		}
		owned, title := false, ""
		s.backend.locked(func() { owned, title = a.view.FactoryOwned, a.view.Title })
		if owned != want {
			t.Errorf("%s: the factory's = %v, want %v", id, owned, want)
		}
		if wantTitle := map[bool]string{true: foremanTitle, false: foremanEarlierTitle}[want]; title != wantTitle {
			t.Errorf("%s is titled %q; want %q", id, title, wantTitle)
		}
	}
	s.mustFail(appwire.FeatureFactoryForemanFresh, map[string]any{"factory": "factory_other"}, appwire.CodeNotFound)
}

// foremanConvs is the fake conversations a rig opened, by agent id, so a test
// can read whether one was stopped.
type foremanConvs struct {
	mu sync.Mutex
	by map[string]*fakeConversation
}

// renamedTo is the name the rig's conversation for agent was last given by
// the person's rename door, "" for none.
func (c *foremanConvs) renamedTo(agent string) string {
	c.mu.Lock()
	conv := c.by[agent]
	c.mu.Unlock()
	if conv == nil {
		return ""
	}
	conv.mu.Lock()
	defer conv.mu.Unlock()
	return renamed[conv]
}

func (c *foremanConvs) stops(agent string) int {
	c.mu.Lock()
	conv := c.by[agent]
	c.mu.Unlock()
	if conv == nil {
		return 0
	}
	conv.mu.Lock()
	defer conv.mu.Unlock()
	return conv.interrupted
}

// foremanFakeOpens has the rig open every conversation as a fake that holds
// its turn open until it is stopped, so a manager can be put away mid-turn.
func foremanFakeOpens(s *localSetup) *foremanConvs {
	convs := &foremanConvs{by: map[string]*fakeConversation{}}
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		conv := &fakeConversation{}
		convs.mu.Lock()
		convs.by[appAgentID(ask.Transcript)] = conv
		convs.mu.Unlock()
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	return convs
}

// TWO ASKS NEVER EACH MAKE A MANAGER. A double press of the manager's button
// once made two: the second was the store's, and the first was left held and
// the factory's with no floor behind it and no door that reached it again.
func TestManyOpensAtOnceMakeOneFactoryManager(t *testing.T) {
	s := newLocalSetup(t)
	foremanFakeOpens(s)
	st := s.start()
	const asks = 8
	answers := make([]string, asks)
	errs := make([]error, asks)
	var wg sync.WaitGroup
	for i := range asks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := s.backend.FactoryForemanOpen(context.Background(), appwire.FactoryRef{Factory: st.Factory})
			answers[i], errs[i] = out.Agent, err
		}()
	}
	wg.Wait()
	for i := range asks {
		if errs[i] != nil || answers[i] == "" || answers[i] != answers[0] {
			t.Fatalf("ask %d answered %q, %v; the first answered %q", i, answers[i], errs[i], answers[0])
		}
	}
	if _, holders := foremanHandleHolder(t, s.backend.local.host.profile); holders != 1 {
		t.Fatalf("%d conversations hold @foreman, want 1", holders)
	}
	named, _ := s.backend.local.host.floor.Foreman()
	if appAgentID(named) != answers[0] {
		t.Fatalf("the store names %q, the asks were answered %q", appAgentID(named), answers[0])
	}
}

// THE FACTORY PUTS ITS MANAGER RIGHT AS IT STARTS, not only when the manager's
// button is pressed. The app brings its tabs back by conversation id and
// resumes a reply the last quit cut, so the manager working in the codeaf home
// was written to there again without factory.foreman_open ever being called.
func TestAFactoryStartingMovesItsManagerOutOfTheCodeafHome(t *testing.T) {
	s := newLocalSetup(t)
	convs := foremanFakeOpens(s)
	floor, err := store.Open(v3FactoryRoot())
	if err != nil {
		t.Fatal(err)
	}
	old := homeForeman(t, floor, factoryProfile(s.backend.profileDir), s.home)
	oldID := appAgentID(old)

	st := s.start()
	foremanEventually(t, "the manager being made again as the factory started", func() bool {
		named, _ := s.backend.local.host.floor.Foreman()
		return named != "" && !sameSessionFile(named, old) && s.backend.tidied(oldID)
	})
	named, _ := s.backend.local.host.floor.Foreman()
	meta, err := session.LoadMetaIn(session.DirOf(named), "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(s.backend.local.host.floor.Root(), "chats", "foreman"); meta.Workspace != want {
		t.Fatalf("the manager works in %q; want %q", meta.Workspace, want)
	}
	if !factoryOwnedConversation(named) || factoryOwnedConversation(old) {
		t.Fatalf("the mark did not follow the manager: new %v, old %v", factoryOwnedConversation(named), factoryOwnedConversation(old))
	}
	// THE OLD ONE NO LONGER READS AS THE MANAGER, in the engine's word and in
	// its own journal's: the tab the app brings back may still hold it.
	foremanEventually(t, "the replaced manager being renamed", func() bool { return convs.renamedTo(oldID) == foremanEarlierTitle })
	was, err := s.backend.find(oldID)
	if err != nil {
		t.Fatal(err)
	}
	owned, title := true, ""
	s.backend.locked(func() { owned, title = was.view.FactoryOwned, was.view.Title })
	if owned || title != foremanEarlierTitle {
		t.Fatalf("the replaced manager reads %q, the factory's %v; want %q and the person's own", title, owned, foremanEarlierTitle)
	}
	// AND ITS BUTTON OPENS THE NEW ONE, with nothing more made.
	var opened appwire.FactoryForeman
	s.must(appwire.FeatureFactoryForemanOpen, map[string]any{"factory": st.Factory}, &opened)
	if opened.Agent != appAgentID(named) {
		t.Fatalf("the door answered %q; want the manager the start made, %q", opened.Agent, appAgentID(named))
	}
}

// A MANAGER MADE BEFORE ITS MARK EXISTED IS TOLD TO THE APP AS THE FACTORY'S.
// After a relaunch the app read such a manager back as one of the person's
// own chats and offered to archive it; opening it by its button set the flag
// in the engine's memory and told the app nothing, because the conversation
// was already open.
func TestAnAlreadyOpenManagerIsSaidToBeTheFactorysWhenItsDoorOpensIt(t *testing.T) {
	s := newLocalSetup(t)
	foremanFakeOpens(s)
	heard := &foremanHeard{next: s.backend.emit}
	s.backend.emit = heard
	st := s.start()
	ref := map[string]any{"factory": st.Factory}
	var first appwire.FactoryForeman
	s.must(appwire.FeatureFactoryForemanOpen, ref, &first)
	a, err := s.backend.find(first.Agent)
	if err != nil {
		t.Fatal(err)
	}
	// As a relaunch before the mark left it: open, and an ordinary chat.
	s.backend.locked(func() { a.view.FactoryOwned = false })
	said := func() int { return heard.owned(first.Agent) }
	before := said()
	var again appwire.FactoryForeman
	s.must(appwire.FeatureFactoryForemanOpen, ref, &again)
	if again.Agent != first.Agent {
		t.Fatalf("the door answered %q; want %q", again.Agent, first.Agent)
	}
	foremanEventually(t, "the app being told the open manager is the factory's", func() bool { return said() > before })
}

// foremanHeard keeps what the engine told the app of its agents, and passes
// every event on.
type foremanHeard struct {
	next appwire.Emitter

	mu      sync.Mutex
	changed []appwire.Agent
}

func (h *foremanHeard) Emit(event string, data any) {
	if changed, ok := data.(appwire.AgentChanged); ok {
		h.mu.Lock()
		h.changed = append(h.changed, changed.Agent)
		h.mu.Unlock()
	}
	h.next.Emit(event, data)
}

// owned is how many times the app was told agent is the factory's.
func (h *foremanHeard) owned(agent string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, a := range h.changed {
		if a.ID == agent && a.FactoryOwned {
			n++
		}
	}
	return n
}
