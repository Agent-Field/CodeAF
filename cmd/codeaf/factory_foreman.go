package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE FOREMAN: THE FLOOR'S OWN CONVERSATION ──────────────────────────────
//
// The app's factory.foreman_open opens the foreman ([appBackend.FactoryForemanOpen]):
// ONE conversation per factory home, the factory-wide manager. It is never the
// runner. It reads every job through `factory_floor` (queued, running, waiting
// on the person, shipped today), proposes by selecting items, and starts or
// prioritises one job when the person asks, through the job page's own Start
// and priority ([seamFloorDoor.StartItem]). It never stops, ships, merges or
// posts, and its belt has no verb that could. It reads only the floor of the
// home it belongs to.
//
// Like an item's own conversation (factory_talk.go) it is OPTIONAL AND NEVER
// MADE BY DEFAULT: the first open makes it, every later open answers the same
// one, and the store keeps which one it is (store/marks.go). It is made on
// this machine's disk with no model call: a session folder in this window's
// workspace bucket, seeded with [foremanBrief], and a member titled
// "Factory manager" (handle `foreman`) directly in the one `factory` team,
// beside the items' own teams.

// foremanTitle is the name a person reads for the factory-wide manager: the
// conversation's title and its word in the `factory` team, so the tab, the
// breadcrumb and the team all say it.
const foremanTitle = "Factory manager"

// foremanHandle is its address in the `factory` team, which a handle keeps
// short and lowercase ([teams.ValidHandle]). IT IS NOT THE NAME: it stays
// `foreman` so every foreman made before the rename keeps its address. The
// conversation is found again by the store's mark ([store.Store.Foreman]),
// never by either word.
const foremanHandle = "foreman"

// foremanOldTitle is the title and team word a foreman made before the rename
// was given; [foremanRetitle] moves it to [foremanTitle] on the next open.
const foremanOldTitle = "foreman"

// foremanEarlierTitle is what a foreman that was REPLACED is titled from then
// on, when its title was still the manager's own. A tab the app brings back
// may still hold that conversation: under the manager's name it read as the
// manager, and a person wrote to it there while the manager's button opened
// another one.
const foremanEarlierTitle = "Factory manager (earlier)"

// foremanOpening is the brief's fixed part, said once so the brief, the test
// and the manual quote the same words. It is kept free of the characters a
// journal line escapes (angle brackets, double quotes), so the conversation's
// file holds it as written and can be searched for it.
const foremanOpening = "You are the foreman of this factory, its factory-wide manager. Every job, its state, its step and its pull request come through factory_floor: read it before you answer what is running, what waits, what is queued, what shipped or how many jobs a repository has, and never answer those from memory. A message from the person may start with blocks the app writes. One tagged afi-now is the app's present state: the time, the factory's counts, what waits on the person ('Waiting on you' is the person, never you), what is running, what was made today and what was noticed lately. One tagged app-places says where things are in the app, for when the person asks where to find or do something. They are the app's, not the person's words, and were true when the message was sent: answer from them what they say, read factory_floor for what they do not, and never quote them back. Anything else the person asks, answer as any assistant would, with your other tools. Start a job or set its priority with factory_floor only when the person asks for it here, naming the job by its ref. Propose what to take on by selecting jobs. Never stop, ship, merge or post. Name jobs by their ref."

// floorDoor is `factory_floor`'s door over the store, or nil.
//
// A NIL STORE IS A NIL DOOR, for [factoryDoor]'s reason: a typed nil would put
// the tool on the belt with nothing behind it. The floor is read through the
// same local seam the page draws ([factory.LocalSeam]), with the same record
// of where each repository is checked out, so the foreman reads the recipe the
// page reads and its marks are the ones the page draws.
func floorDoor(st *store.Store, workspace, profileDir string) session.FloorDoor {
	if st == nil {
		return nil
	}
	seam := factory.LocalSeam(st, time.Now(), factoryPipelineOption(st), factory.WithRepoDirs(factoryRepoDirs(st, workspace)))
	if seam.Load == nil || seam.Mark == nil {
		return nil
	}
	return seamFloorDoor{seam: seam, st: st, workspace: workspace, profileDir: profileDir}
}

// foremanFloorDoor is `factory_floor`'s door for ONE conversation: the floor's
// door when transcript is the foreman's session file as the store names it
// ([store.Store.Foreman]), and nil for every other conversation, so the tool is
// on the foreman's belt and on no other. Another conversation that wants the
// floor reaches it through `factory_item` and `factory_add`, or the person
// opens the foreman with `m`. A nil store, a store that names no foreman yet,
// or a store that cannot be read is a nil door, by [floorDoor]'s law.
func foremanFloorDoor(st *store.Store, workspace, profileDir, transcript string) session.FloorDoor {
	if st == nil || strings.TrimSpace(transcript) == "" {
		return nil
	}
	chat, err := st.Foreman()
	if err != nil || !sameSessionFile(chat, transcript) {
		return nil
	}
	return floorDoor(st, workspace, profileDir)
}

// sameSessionFile says whether two paths name one session file: cleaned, and
// with symlinks resolved where the file is there to resolve, which is the key
// [foremanJoinTeam] files the foreman under.
func sameSessionFile(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	resolve := func(p string) string {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		return filepath.Clean(p)
	}
	return resolve(a) == resolve(b)
}

// seamFloorDoor answers the foreman's door from a seam's Load and Mark, and
// starts or prioritises a job over st the way the job page does.
type seamFloorDoor struct {
	seam       factory.Seam
	st         *store.Store
	workspace  string
	profileDir string
}

// StartItem is the foreman's start of one job: the job page's Start, sent as
// the person through the app's press when there is one, and otherwise the
// job manager's own start ([startItem]) on this machine's floor.
func (d seamFloorDoor) StartItem(ctx context.Context, id int) (string, error) {
	it, err := d.st.Get(id)
	if err != nil {
		return "", err
	}
	if press := appLocalPress.Load(); press != nil && it.State == factory.StateNew {
		if err := (*press)(ctx, id, factoryVerbStart, guardBody{}); err != nil {
			return "", fmt.Errorf("Nothing changed: %w", err)
		}
		return fmt.Sprintf(sayStartLaunched, it.Ref()), nil
	}
	line, err := startItem(d.st, d.workspace, d.profileDir, it)
	if err != nil {
		return "", fmt.Errorf("Nothing changed: %w", err)
	}
	return line, nil
}

// SetPriority is the job page's priority, as the person, through the app's
// press; a window with none says so and changes nothing.
func (d seamFloorDoor) SetPriority(ctx context.Context, id int, value string) (string, error) {
	it, err := d.st.Get(id)
	if err != nil {
		return "", err
	}
	press := appLocalPress.Load()
	if press == nil {
		return "", errNoPress
	}
	if err := (*press)(ctx, id, factoryVerbPriority, struct {
		Priority string `json:"priority"`
	}{value}); err != nil {
		return "", fmt.Errorf("Nothing changed: %w", err)
	}
	return fmt.Sprintf("%s is now %s (%s)", it.Ref(), factory.PriorityShort(value), value), nil
}

func (d seamFloorDoor) Snapshot(context.Context) (factory.Snapshot, error) { return d.seam.Load() }

func (d seamFloorDoor) Mark(_ context.Context, ids []int, on bool) error {
	return d.seam.Mark(ids, on)
}

// withForeman hangs the foreman's door on the floor's seam: `m` exists only
// where this machine's floor is drawn, the same law the Talk door keeps.
func withForeman(seam factory.Seam, st *store.Store, workspace, profileDir string) factory.Seam {
	if st == nil || seam.Load == nil {
		return seam
	}
	seam.Foreman = foremanMaker(st, workspace, profileDir, seam.Load)
	return seam
}

// foremanMaker is the Foreman door's maker: the conversation the store names
// when its session file is still there, and otherwise one made now and named
// in the store, so A SECOND ASK OPENS THE SAME CONVERSATION. load is the floor's
// read, asked once at the making for the recipes' policy lines.
//
// A FOREMAN WHOSE FOLDER THE FACTORY REFUSES IS MADE AGAIN, in a folder of its
// own. A host that named no folder once gave the foreman the engine's working
// directory, which under the desk is the codeaf home itself: every save of
// that conversation then swept the whole home, and its tool calls waited behind
// a capture that never finished, so the manager answered nothing at all. No
// new foreman is made there ([factoryFolderRefusal]); this is the same refusal
// for the one a store already names, and it is that rule whole: the home, any
// folder above it, and any folder inside it that is not one of the factory's
// own areas. The old conversation is left on disk, off the `factory` team and
// no longer marked the factory's. The app stops and archives it
// ([appBackend.foremanOpened], [appBackend.settleForeman]); the terminal's
// door leaves it as it is, an ordinary conversation in the person's list.
func foremanMaker(st *store.Store, workspace, profileDir string, load func() (factory.Snapshot, error)) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		factoryReconcileOnce(profileDir, st)
		if chat, err := st.Foreman(); err == nil && chat != "" {
			if _, serr := os.Stat(chat); serr == nil && foremanFolderRefusal(st, chat) == nil {
				foremanRetitle(profileDir, chat)
				// One made before the mark existed takes it on its next open.
				_ = markFactoryForeman(st, chat)
				return chat, nil
			}
		}
		return foremanMint(st, workspace, profileDir, load)
	}
}

// foremanFresh starts the foreman over: a new conversation with none of the
// old one's words, which takes its place in the store and in the `factory`
// team. It is the app's Clear ([appBackend.FactoryForemanFresh]); the old
// conversation is left on disk for its owner to stop and put away.
func foremanFresh(st *store.Store, workspace, profileDir string, load func() (factory.Snapshot, error)) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		factoryReconcileOnce(profileDir, st)
		return foremanMint(st, workspace, profileDir, load)
	}
}

// foremanFolderRefusal is [factoryFolderRefusal] for a foreman that already
// exists, by the folder its conversation records. A conversation whose record
// cannot be read, or names no folder, is not refused: this rule moves a
// foreman out of a folder known to be wrong, never one it cannot see.
func foremanFolderRefusal(st *store.Store, transcript string) error {
	meta, err := session.LoadMeta(session.DirOf(transcript))
	if err != nil {
		return nil
	}
	return factoryFolderRefusal(st, meta.Workspace)
}

// foremanMint makes the foreman's conversation now and names it in the store.
// A foreman the store already names gives up its place in the `factory` team
// first, so the new one takes the `foreman` handle rather than a second-best
// one beside it.
func foremanMint(st *store.Store, workspace, profileDir string, load func() (factory.Snapshot, error)) (string, error) {
	where := strings.TrimSpace(workspace)
	// A HOST WITH NO FOLDER OF ITS OWN (the desk's factory, or an engine
	// started in the codeaf home) gives the foreman an empty folder under the
	// floor: it reads the floor through its door, never through files
	// (factory_chat_folder.go). A host whose own folder is refused is the
	// same host, for this purpose.
	if where == "" || factoryFolderRefusal(st, where) != nil {
		own, err := factoryForemanFolder(st)
		if err != nil {
			return "", err
		}
		where = own
	}
	if err := factoryFolderRefusal(st, where); err != nil {
		return "", err
	}
	bucket, err := v3ProjectDir(where)
	if err != nil {
		return "", err
	}
	place, err := v3MintSession(bucket, where, v3StampLaunchDir(v3LaunchDir(), where), false)
	if err != nil {
		return "", err
	}
	transcript := place.Transcript()
	if err := session.SeedConversation(transcript, where, foremanTitle, foremanBrief(foremanPolicyRead(load))); err != nil {
		return "", err
	}
	old, _ := st.Foreman()
	if old != "" {
		foremanLeaveTeam(profileDir, old)
	}
	if err := foremanJoinTeam(profileDir, transcript, where); err != nil {
		return "", err
	}
	// THE STORE'S WORD COMES FIRST, THEN THE MARKS. The store is what makes a
	// conversation the foreman; the mark only says so beside the journal. A
	// making cut short after the store named the new one leaves marks a later
	// open or start puts right ([markFactoryForeman] there, [foremanSweep]);
	// one cut short before it leaves a conversation nobody names and nothing
	// hides, which the person can see and delete. Marked first, that same
	// conversation was hidden from every list with no door that opened it.
	if err := st.SetForeman(transcript); err != nil {
		return "", err
	}
	if err := markFactoryForeman(st, transcript); err != nil {
		return "", err
	}
	if old != "" && !sameSessionFile(old, transcript) {
		foremanRetire(old)
	}
	return transcript, nil
}

// foremanSweep retires every conversation that still carries the foreman's
// mark and is not the one the store names: a making that was cut short
// between naming the new foreman and retiring the old one left the old one
// marked, so it was the factory's in every reading, in no list, and behind no
// door. They are found where a replaced foreman is always left, among the
// `factory` team's former members under the foreman's handle. It answers the
// conversations it retired, for their owner to put away.
func foremanSweep(st *store.Store, profileDir string) []string {
	current, err := st.Foreman()
	if err != nil {
		return nil
	}
	f, err := teams.Load(profileDir)
	if err != nil {
		return nil
	}
	var retired []string
	for _, t := range f.Teams {
		for _, m := range t.FormerMembers {
			file := strings.TrimSpace(m.File)
			if file == "" || m.Handle != foremanHandle || sameSessionFile(file, current) || !factoryForemanMarked(file) {
				continue
			}
			foremanRetire(file)
			retired = append(retired, file)
		}
	}
	return retired
}

// foremanRetire makes a replaced foreman an ordinary conversation of the
// person's: its mark comes off, and a title that was still the manager's own
// says it is the earlier one. A title the person gave it is theirs and stays.
// It is best effort; the new foreman is the factory's either way.
func foremanRetire(transcript string) {
	unmarkFactoryForeman(transcript)
	dir := session.DirOf(transcript)
	meta, err := session.LoadMeta(dir)
	if err != nil {
		return
	}
	if title := strings.TrimSpace(meta.Title); title == foremanTitle || title == foremanOldTitle {
		meta.Title = foremanEarlierTitle
		_ = session.SaveMeta(dir, meta)
	}
}

// foremanPolicyWait is how long a foreman's making waits for the floor's read
// before it is made without the recipes' policy lines.
const foremanPolicyWait = 5 * time.Second

// foremanPolicyRead is the floor's read for the brief's policy lines, WAITED
// FOR A BOUNDED TIME. The read is the one factory_floor bounds for the same
// reason (internal/session's floorWithin): it reads disk and may wait on a
// lock, and a making that waited on it without limit was an open of the
// manager that never answered. A brief without the policy lines is still a
// foreman; a floor that could not be read in time says nothing here.
func foremanPolicyRead(load func() (factory.Snapshot, error)) factory.Snapshot {
	if load == nil {
		return factory.Snapshot{}
	}
	done := make(chan factory.Snapshot, 1)
	// Guarded: a read that panics answers nothing, and the wait below says so.
	guard.Go("app/factory foreman policy read", func() {
		snap, _ := load()
		done <- snap
	})
	select {
	case snap := <-done:
		return snap
	case <-time.After(foremanPolicyWait):
		return factory.Snapshot{}
	}
}

// foremanBrief is the foreman's opening note: [foremanOpening], then each
// repository's policy lines as facts. A floor with no policy says none (the
// emptiness law).
func foremanBrief(snap factory.Snapshot) string {
	lines := []string{foremanOpening}
	for _, repo := range snap.Repos {
		for _, p := range repo.Recipe.Policy {
			if p = strings.Join(strings.Fields(p), " "); p != "" {
				lines = append(lines, "policy on "+repo.Name+": "+p)
			}
		}
	}
	return strings.Join(lines, "\n")
}

// foremanJoinTeam writes the foreman into the one `factory` team, made here
// when no item has made it yet, in ONE read-modify-write of the teams file.
// The team is found by the rule [talkJoinTeam] finds it by.
func foremanJoinTeam(profileDir, transcript, where string) error {
	key := transcript
	if real, err := filepath.EvalSymlinks(transcript); err == nil {
		key = real
	}
	key = filepath.Clean(key)
	now := time.Now()
	return teams.UpdateOpen(profileDir, func(f *teams.File) error {
		id := factoryParentTeam(f, now)
		return f.AddMember(id, teams.Member{Key: key, File: transcript, Where: where, Word: foremanTitle, Handle: foremanHandle, JoinedAt: now})
	})
}

// foremanLeaveTeam takes a foreman that is being replaced out of every team
// that holds it, so its handle is free for the one made in its place. It is
// best effort: a team file that cannot be written leaves the old member where
// it was, and the new one joins beside it under another handle.
func foremanLeaveTeam(profileDir, transcript string) {
	key := transcript
	if real, err := filepath.EvalSymlinks(transcript); err == nil {
		key = real
	}
	key = filepath.Clean(key)
	_ = teams.UpdateOpen(profileDir, func(f *teams.File) error {
		for _, t := range f.Teams {
			if t.Holds(key) {
				if err := f.RemoveMember(t.ID, key); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// foremanRetitle renames a foreman made before the rename: its conversation's
// title and its word in the `factory` team, when each still says the old
// title. A title the person changed is theirs and stays. It is best effort;
// the conversation opens either way.
func foremanRetitle(profileDir, transcript string) {
	dir := session.DirOf(transcript)
	if meta, err := session.LoadMeta(dir); err == nil && strings.TrimSpace(meta.Title) == foremanOldTitle {
		meta.Title = foremanTitle
		_ = session.SaveMeta(dir, meta)
	}
	key := transcript
	if real, err := filepath.EvalSymlinks(transcript); err == nil {
		key = real
	}
	key = filepath.Clean(key)
	_ = teams.UpdateOpen(profileDir, func(f *teams.File) error {
		for ti := range f.Teams {
			for mi := range f.Teams[ti].Members {
				if m := &f.Teams[ti].Members[mi]; m.Key == key && m.Word == foremanOldTitle {
					m.Word = foremanTitle
				}
			}
		}
		return nil
	})
}
