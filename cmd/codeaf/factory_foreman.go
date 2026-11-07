package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
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
// workspace bucket, seeded with [foremanBrief], and a member named `foreman`
// directly in the one `factory` team, beside the items' own teams.

// foremanName is the conversation's name and its handle in the `factory` team.
const foremanName = "foreman"

// foremanOpening is the brief's fixed part, said once so the brief, the test
// and the manual quote the same words.
const foremanOpening = "You are the foreman of this factory, its factory-wide manager. Every job, its state, its step and its pull request come through factory_floor; answer what is running, what waits, what is queued and what shipped only from it. Start a job or set its priority with factory_floor only when the person asks for it here, naming the job by its ref. Propose what to take on by selecting jobs. Never stop, ship, merge or post. Name jobs by their ref."

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
func foremanMaker(st *store.Store, workspace, profileDir string, load func() (factory.Snapshot, error)) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		factoryReconcileOnce(profileDir, st)
		if chat, err := st.Foreman(); err == nil && chat != "" {
			if _, serr := os.Stat(chat); serr == nil {
				return chat, nil
			}
		}
		where := strings.TrimSpace(workspace)
		if where == "" {
			return "", errors.New("codeaf does not know which folder the foreman's conversation belongs in")
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
		var snap factory.Snapshot
		if load != nil {
			snap, _ = load()
		}
		if err := session.SeedConversation(transcript, where, foremanName, foremanBrief(snap)); err != nil {
			return "", err
		}
		if err := foremanJoinTeam(profileDir, transcript, where); err != nil {
			return "", err
		}
		if err := st.SetForeman(transcript); err != nil {
			return "", err
		}
		return transcript, nil
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
		return f.AddMember(id, teams.Member{Key: key, File: transcript, Where: where, Word: foremanName, Handle: foremanName, JoinedAt: now})
	})
}
