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
// factory tools, and `factory_item` beside them ([itemDoor]), which is how the
// conversation proposes a change to the item — its stages, ask me at, budget, thinking,
// or a note for its stages — and the person's key makes it. THE CONVERSATION IS
// THE ITEM'S HUB, and the brief says so: the model is told it may change the
// item through the card, leave notes the stages will read, and that the stages
// will report into this conversation once they run.

// factoryTeamName is the one team every item's team sits under.
const factoryTeamName = "factory"

// talkClosing is the brief's last sentence, said once so the brief and the
// manual quote the same words.
const talkClosing = "This is the item's own conversation on the factory floor. Nothing here runs it; the person does that on the floor."

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
		parent := factoryParentTeam(f, now)
		id := teams.NewID()
		f.Teams = append(f.Teams, teams.Team{ID: id, Name: name, Parent: parent, Made: now})
		return f.AddMember(id, teams.Member{Key: key, File: transcript, Where: where, Word: name, Handle: talkHandle(it), JoinedAt: now})
	})
}

// factoryParentTeam is the one open `factory` team every item's team sits
// under, found in f or made in it now. It is called inside a teams.Update, so
// finding and making are one read-modify-write of the teams file.
func factoryParentTeam(f *teams.File, now time.Time) string {
	for _, t := range f.Teams {
		if t.Name == factoryTeamName && t.Parent == "" && !t.Closed() && !t.Root {
			return t.ID
		}
	}
	if root, ok := f.Root(); ok {
		for _, t := range f.Teams {
			if t.Name == factoryTeamName && t.Parent == root.ID && !t.Closed() {
				return t.ID
			}
		}
	}
	parent := teams.NewID()
	f.Teams = append(f.Teams, teams.Team{ID: parent, Name: factoryTeamName, Made: now})
	return parent
}

// talkHandle is the one handle an item's conversation has for good, made from
// its ref (`#1` is `item1`). A handle that arrives with a member is a given
// one, so the title model never chooses it again and the team's Traffic never
// announces `@twice is now @doublecount` into a conversation about an item
// the person calls by its number. A ref with no digits has no handle given, and
// the word list's guess stands as for any member.
func talkHandle(it factory.Item) string {
	var digits strings.Builder
	for _, r := range it.Ref() {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	if digits.Len() == 0 {
		return ""
	}
	h := "item" + digits.String()
	if teams.ValidHandle(h) != nil {
		return ""
	}
	return h
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
	// THE MARKER IS THE FIRST LINE, and a surface draws the item's live card
	// in its place (internal/tui3's factoryitemcard.go), so the person sees the
	// item where the model sees its brief.
	line(talkMarker(it))
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
	line(strings.Join(facts, " · "))
	// THE GATE AND THE CAP ARE SAID AS WHAT THEY ARE, `ask me at pull request · budget $5`,
	// and the labels on a line of their own: the conversation is read by a
	// model and by a person, and neither knows a "chip" as anything but a
	// shape some screen draws.
	var set, labels []string
	add2 := func(list *[]string, s string) {
		if s = strings.TrimSpace(s); s != "" {
			*list = append(*list, s)
		}
	}
	if it.Gate != "" {
		add2(&set, "ask me at "+session.ItemGateWord(string(it.Gate)))
	}
	if it.Cap > 0 {
		add2(&set, "budget $"+strconv.FormatFloat(it.Cap, 'f', -1, 64))
	}
	for _, label := range it.Labels {
		add2(&labels, label)
	}
	if len(set) > 0 {
		line(strings.Join(set, " · "))
	}
	if len(labels) > 0 {
		line("labels: " + strings.Join(labels, " · "))
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
	for _, note := range it.Notes {
		line("note for the stages: " + note)
	}
	line(fmt.Sprintf("Call this item %s in everything you say; the person knows it by that name. This conversation is %s's hub: through factory_item you can change its stages, where the run asks the person (the person calls it \"ask me at\"; the tool's field is gate), its budget (field cap) and its thinking (field effort), and leave notes its stages will read. The floor id for factory_item is %d, and it goes in the tool's item field only, never in your words. Nothing changes until the person presses a key on the card. Once %s runs, its stages will report into this conversation.", it.Ref(), it.Ref(), it.ID, it.Ref()))
	line(talkClosing)
	return strings.TrimSpace(b.String())
}

// itemDoor is `factory_item`'s door over the store, or nil.
//
// A NIL STORE IS A NIL DOOR, for [factoryDoor]'s reason: with no floor there
// is no item to change, and the tool is then absent from the belt.
func itemDoor(st *store.Store, workspace string) session.ItemDoor {
	if st == nil {
		return nil
	}
	return storeItemDoor{st: st, dirs: factoryRepoDirs(st, workspace)}
}

// storeItemDoor changes one item through [session.ApplyItemChange], which is
// where every bound is held: the stages through [factory.Adapt], under the
// item's own repository's recipe.
type storeItemDoor struct {
	st   *store.Store
	dirs func(repo string) string
}

// get is the item, or the person's sentence for an id the floor does not have.
func (d storeItemDoor) get(item int) (factory.Item, error) {
	it, err := d.st.Get(item)
	if errors.Is(err, store.ErrNotFound) {
		return factory.Item{}, fmt.Errorf("there is no item %d on the factory floor", item)
	}
	return it, err
}

// Preview answers the item as it stands and as the change would leave it,
// writing nothing. A change the bounds refuse is refused here, before any
// card, in [factory.Adapt]'s own sentence.
func (d storeItemDoor) Preview(_ context.Context, item int, change session.ItemChange) (factory.Item, factory.Item, error) {
	it, err := d.get(item)
	if err != nil {
		return factory.Item{}, factory.Item{}, err
	}
	recipe := factoryItemRecipe(d.dirs, it)
	before := session.ItemStaged(it, recipe)
	after, err := session.ApplyItemChange(it, change, recipe)
	if err != nil {
		return factory.Item{}, factory.Item{}, err
	}
	return before, after, nil
}

// Apply makes the change in one read-modify-write of the item's document,
// against the item as it is NOW — a stage may have started since the card was
// raised, and the bounds are asked again — and answers the item as saved.
func (d storeItemDoor) Apply(_ context.Context, item int, change session.ItemChange) (factory.Item, error) {
	var now factory.Item
	err := d.st.Update(item, func(it *factory.Item) error {
		next, err := session.ApplyItemChange(*it, change, factoryItemRecipe(d.dirs, *it))
		if err != nil {
			return err
		}
		*it = next
		return nil
	})
	if errors.Is(err, store.ErrNotFound) {
		return factory.Item{}, fmt.Errorf("there is no item %d on the factory floor", item)
	}
	if err != nil {
		return factory.Item{}, err
	}
	if now, err = d.st.Get(item); err != nil {
		return factory.Item{}, err
	}
	return now, nil
}

// Stages is the stage names kind runs in repo's recipe today, which the recipe
// card shows before and after its line ([session.RecipeRows]). It is asked of
// the same checkout the bank writes to, so an unknown checkout refuses here in
// the bank's own words, before the person is asked anything.
func (d fileRecipeDoor) Stages(_ context.Context, repo string, kind factory.Kind) ([]string, error) {
	dir, err := d.dir(repo)
	if err != nil {
		return nil, err
	}
	recipe, _, err := factory.Load(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, st := range recipe.For(kind) {
		names = append(names, st.Name)
	}
	return names, nil
}

// talkMarker is the brief's first line, `[factory item #12]`, which a surface
// draws as the item's live card. An item the floor names by a forge number
// carries its floor id after it (`[factory item #1540 · 7]`) so the surface
// finds the right row whichever repository the number belongs to.
func talkMarker(it factory.Item) string {
	ref := it.Ref()
	if ref == "#"+strconv.Itoa(it.ID) || it.ID <= 0 {
		return "[factory item " + ref + "]"
	}
	return "[factory item " + ref + " · " + strconv.Itoa(it.ID) + "]"
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
