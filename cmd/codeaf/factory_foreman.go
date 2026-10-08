package main

import (
	"context"
	"errors"
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
// `m` on the factory floor opens the foreman: ONE conversation per home (later
// one per product), the product-level judgment about what to take first. It is
// never the runner. It reads the floor through `factory_floor` and proposes by
// marking items; the person launches what is marked with `L`. It never
// launches, ships or posts, and its belt has no verb that could.
//
// Like an item's own conversation (`T`, factory_talk.go) it is OPTIONAL AND
// NEVER MADE BY DEFAULT: the first `m` makes it, every later `m` opens the same
// one, and the store keeps which one it is (store/marks.go). It is made on
// this machine's disk with no model call: a session folder in this window's
// workspace bucket, seeded with [foremanBrief], and a member named `foreman`
// directly in the one `factory` team, beside the items' own teams.

// foremanName is the conversation's name and its handle in the `factory` team.
const foremanName = "foreman"

// foremanOpening is the brief's fixed part, said once so the brief, the test
// and the manual quote the same words.
const foremanOpening = "You are the foreman of this factory floor. The floor's items, their reads and stages come through factory_floor. Propose a morning batch by marking items; the person launches with L. Never launch, ship or post. Name items by their ref."

// floorDoor is `factory_floor`'s door over the store, or nil.
//
// A NIL STORE IS A NIL DOOR, for [factoryDoor]'s reason: a typed nil would put
// the tool on the belt with nothing behind it. The floor is read through the
// same local seam the page draws ([factory.LocalSeam]), with the same record
// of where each repository is checked out, so the foreman reads the recipe the
// page reads and its marks are the ones the page draws.
func floorDoor(st *store.Store, workspace string) session.FloorDoor {
	if st == nil {
		return nil
	}
	seam := factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(factoryRepoDirs(st, workspace)))
	if seam.Load == nil || seam.Mark == nil {
		return nil
	}
	return seamFloorDoor{seam: seam}
}

// seamFloorDoor answers the foreman's door from a seam's Load and Mark.
type seamFloorDoor struct {
	seam factory.Seam
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
	return teams.Update(profileDir, func(f *teams.File) error {
		id := ""
		for _, t := range f.Teams {
			if t.Name == factoryTeamName && t.Parent == "" && !t.Closed() && !t.Root {
				id = t.ID
				break
			}
		}
		if root, ok := f.Root(); ok && id == "" {
			for _, t := range f.Teams {
				if t.Name == factoryTeamName && t.Parent == root.ID && !t.Closed() {
					id = t.ID
					break
				}
			}
		}
		if id == "" {
			id = teams.NewID()
			f.Teams = append(f.Teams, teams.Team{ID: id, Name: factoryTeamName, Made: now})
		}
		return f.AddMember(id, teams.Member{Key: key, File: transcript, Where: where, Word: foremanName, Handle: foremanName, JoinedAt: now})
	})
}
