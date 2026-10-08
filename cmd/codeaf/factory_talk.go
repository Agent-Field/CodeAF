package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// ── TALK IT THROUGH: THE ITEM'S OWN CONVERSATION ────────────────────────────
//
// `T` on the factory floor opens an item's own conversation, made on the first
// press and the same one on every press after ([factory.Seam.Talk]). It is
// OPTIONAL AND NEVER MADE BY DEFAULT: an item that nobody pressed `T` on has no
// conversation and no team.
//
// What the first press makes, all of it on this machine's disk and none of it
// a model call:
//
//   - a team for the item, named by its ref and title (`#12 · fix the ledger
//     double count`), NESTED UNDER ONE `factory` TEAM made once per home, so a
//     hundred items are one team on every list of teams, folded until a person
//     opens it (internal/tui3's factory_talk.go);
//   - one conversation in that team, a session folder in the bucket of the
//     repository's checkout (or this window's workspace when the checkout is
//     not known), whose journal opens with the item in front of it
//     ([talkBrief]) as the session's own note ([session.SeedConversation]).
//
// The conversation's belt is every other conversation's on this launch: the
// factory tools, and `factory_stages` beside them ([stagesDoor]), which is how
// the conversation proposes a change to the item and the person's key makes it.

// factoryTeamName is the one team every item's team sits under.
const factoryTeamName = "factory"

// talkClosing is the brief's last sentence, said once so the brief and the
// manual quote the same words.
const talkClosing = "This is the item's own conversation on the factory floor. Nothing here launches it; the person does that on the floor."

// talkMaker is the Talk door's maker for this machine's floor: st is the store
// the item lives in, workspace the window's own folder, and profileDir the
// profile whose teams file the item's team is written into.
func talkMaker(st *store.Store, workspace, profileDir string) func(context.Context, factory.Item) (string, error) {
	dirs := factoryRepoDirs(st, workspace)
	return func(_ context.Context, it factory.Item) (string, error) {
		where := strings.TrimSpace(dirs(it.Repo))
		if where == "" {
			where = strings.TrimSpace(workspace)
		}
		if where == "" {
			return "", errors.New("codeaf does not know which folder this item's conversation belongs in")
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
		name := talkTeamName(it)
		if err := session.SeedConversation(transcript, where, name, talkBrief(it, factoryItemRecipe(dirs, it))); err != nil {
			return "", err
		}
		if err := talkJoinTeam(profileDir, it, transcript, where, name); err != nil {
			return "", err
		}
		return transcript, nil
	}
}

// talkTeamName is the item's team's name and its conversation's: the floor's
// ref and the title, `#12 · fix the ledger double count`.
func talkTeamName(it factory.Item) string {
	return it.Ref() + " · " + strings.Join(strings.Fields(it.Title), " ")
}

// talkJoinTeam writes the item's team, under the one `factory` team, and the
// conversation into it, in ONE read-modify-write of the teams file. The
// `factory` team is the open top-level team of that name, made here the first
// time any item asks for one.
func talkJoinTeam(profileDir string, it factory.Item, transcript, where, name string) error {
	key := transcript
	if real, err := filepath.EvalSymlinks(transcript); err == nil {
		key = real
	}
	key = filepath.Clean(key)
	now := time.Now()
	return teams.Update(profileDir, func(f *teams.File) error {
		parent := ""
		for _, t := range f.Teams {
			if t.Name == factoryTeamName && t.Parent == "" && !t.Closed() && !t.Root {
				parent = t.ID
				break
			}
		}
		if root, ok := f.Root(); ok && parent == "" {
			for _, t := range f.Teams {
				if t.Name == factoryTeamName && t.Parent == root.ID && !t.Closed() {
					parent = t.ID
					break
				}
			}
		}
		if parent == "" {
			parent = teams.NewID()
			f.Teams = append(f.Teams, teams.Team{ID: parent, Name: factoryTeamName, Made: now})
		}
		id := teams.NewID()
		f.Teams = append(f.Teams, teams.Team{ID: id, Name: name, Parent: parent, Made: now})
		return f.AddMember(id, teams.Member{Key: key, File: transcript, Where: where, Word: name, JoinedAt: now})
	})
}

// factoryItemRecipe is the recipe an item runs under: its repository's file
// when this machine knows the checkout, the default recipe otherwise — the
// same reading the floor makes ([factory.LocalSeam]'s recipe).
func factoryItemRecipe(dirs func(string) string, it factory.Item) factory.Recipe {
	if dir := strings.TrimSpace(dirs(it.Repo)); dir != "" {
		if r, _, err := factory.Load(dir); err == nil {
			return r
		}
	}
	return factory.DefaultRecipe()
}

// talkBrief is the opening note: the item as the floor knows it, then the
// sentence that says what this conversation is and is not.
//
// EVERY LINE IS LEFT OUT WHEN THERE IS NOTHING TO SAY (the emptiness law): an
// item with no body, no author, no cap or no read says nothing about them.
func talkBrief(it factory.Item, recipe factory.Recipe) string {
	var b strings.Builder
	line := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			b.WriteString(s)
			b.WriteByte('\n')
		}
	}
	line(talkTeamName(it))
	var facts []string
	add := func(label, value string) {
		if value = strings.TrimSpace(value); value != "" {
			facts = append(facts, label+value)
		}
	}
	add("repo ", it.Repo)
	add("author ", it.Author)
	add("tier ", string(it.Tier))
	add("floor id ", strconv.Itoa(it.ID))
	line(strings.Join(facts, " · "))
	var chips []string
	add2 := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			chips = append(chips, s)
		}
	}
	if it.Gate != "" {
		add2("gate " + string(it.Gate))
	}
	if it.Cap > 0 {
		add2("cap $" + strconv.FormatFloat(it.Cap, 'f', -1, 64))
	}
	for _, label := range it.Labels {
		add2(label)
	}
	if len(chips) > 0 {
		line("chips: " + strings.Join(chips, " · "))
	}
	if body := strings.TrimSpace(it.Body); body != "" {
		b.WriteByte('\n')
		line(body)
		b.WriteByte('\n')
	}
	stages := it.Stages
	if len(stages) == 0 {
		stages = recipe.For(it.Kind)
	}
	if lines := factory.StageLines(stages); len(lines) > 0 {
		line("stages:")
		for _, l := range lines {
			line(l)
		}
	}
	line(it.Triage.Read)
	line(fmt.Sprintf("To change this item's stages, propose it with factory_stages (item %d); nothing changes until the person presses a key on the card.", it.ID))
	line(talkClosing)
	return strings.TrimSpace(b.String())
}

// stagesDoor is `factory_stages`' door over the store, or nil.
//
// A NIL STORE IS A NIL DOOR, for [factoryDoor]'s reason: with no floor there
// is no item to change, and the tool is then absent from the belt.
func stagesDoor(st *store.Store, workspace string) session.StagesDoor {
	if st == nil {
		return nil
	}
	return storeStagesDoor{st: st, dirs: factoryRepoDirs(st, workspace)}
}

// storeStagesDoor changes one item's stages through [factory.Adapt].
type storeStagesDoor struct {
	st   *store.Store
	dirs func(repo string) string
}

// Ref is the floor's name for an item, asked before any card is raised so a
// card never names an item the floor does not have.
func (d storeStagesDoor) Ref(_ context.Context, item int) (string, error) {
	it, err := d.st.Get(item)
	if errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("there is no item %d on the factory floor", item)
	}
	if err != nil {
		return "", err
	}
	return it.Ref(), nil
}

// Apply loads the item, reads its repository's recipe, applies the edit
// through [factory.Adapt] — WHERE EVERY BOUND IS HELD, and under a `fixed`
// recipe the refusal is Adapt's own sentence — and saves it, in one
// read-modify-write of the item's document. It answers the stages the item
// runs now, in order.
func (d storeStagesDoor) Apply(_ context.Context, item int, edit factory.PlanEdit) ([]string, error) {
	var now []string
	err := d.st.Update(item, func(it *factory.Item) error {
		next, _, err := factory.Adapt(*it, edit, factoryItemRecipe(d.dirs, *it))
		if err != nil {
			return err
		}
		*it = next
		for _, st := range it.Stages {
			if st.On {
				now = append(now, st.Name)
			}
		}
		return nil
	})
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("there is no item %d on the factory floor", item)
	}
	return now, err
}

// talkPutAway wraps a seam's Dismiss so an item put away takes its
// conversation with it: the conversation is archived (home's archive line) and
// the item's team is closed, so neither stands on any list once the item does
// not. A conversation or a team that cannot be put away is left as it is; the
// item is dismissed either way.
func talkPutAway(seam factory.Seam, st *store.Store, profileDir string) factory.Seam {
	dismiss := seam.Dismiss
	if dismiss == nil || st == nil {
		return seam
	}
	seam.Dismiss = func(id int) error {
		if err := dismiss(id); err != nil {
			return err
		}
		it, err := st.Get(id)
		if err != nil || strings.TrimSpace(it.Talk) == "" {
			return nil
		}
		_ = session.SetArchived(filepath.Dir(it.Talk), true)
		key := it.Talk
		if real, err := filepath.EvalSymlinks(key); err == nil {
			key = real
		}
		key = filepath.Clean(key)
		_ = teams.Update(profileDir, func(f *teams.File) error {
			for _, t := range f.Teams {
				if t.Holds(key) && !t.Closed() && t.Name != factoryTeamName {
					return f.Close(t.ID, time.Now(), "")
				}
			}
			return nil
		})
		return nil
	}
	return seam
}
