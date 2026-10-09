package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
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
// THE CONVERSATION IS THE MANAGER OF THE ITEM'S RUN (the owner's decision of
// 2026-10-08). It is the LEAD of the item's team, the stages' conversations
// its members; a launch makes it when nobody pressed `T` first, through the
// same maker ([managerMaker], wired as the runner's Options.Manager); the
// runner reports each stage into it; and what the person types into it is the
// brief before a run and the steer during one (internal/factory/run's
// manager.go).
//
// The conversation's belt is every other conversation's on this launch: the
// factory tools, and `factory_item` beside them ([itemDoor]), which is how the
// conversation proposes a change to the item — budget, thinking, or a
// note for its stages — and the person's key makes it; and `factory_run` beside
// that ([runDoor]), the manager's own pen on the run's stages, which raises no
// card. THE CONVERSATION IS THE MANAGER OF THE RUN, and its brief is the
// owner's five blocks ([talkBlocks]) followed by the item's facts.

// factoryTeamName is the one team every item's team sits under.
const factoryTeamName = "factory"

// THE MANAGER'S BRIEF, the owner's five blocks (2026-10-08), word for word:
// who it is, what it knows, what it does, how it shapes the run, and how it
// speaks. The manual quotes them (internal/manual/chat/factory.md); a block
// respelled here is respelled there in the same change.
const (
	talkBlockWho   = "You are the manager of %s in %s."
	talkBlockKnow  = "You know: the issue and its comments, what codeaf read of it, the repository's recipe, policy and habits, the checkout, and what the person has said here."
	talkBlockDo    = "You do: shape the run, start it with factory_start when the person asks (nothing to look up first), report each stage here, answer the person, and hold at each approve step until the person says continue, saying what you wait on. The runner runs the steps: read a step to report on it, and never send one work, stop it or start one yourself."
	talkBlockShape = "Shape the run with factory_run: stages are one lowercase word each, at most nine. Each stage has an ask that says what done looks like, a loop (until, rounds, fanout) and one line of why. Keep the recipe's stages unless the item says otherwise; change asks before adding stages; add a stage only for work no existing stage covers. Never drop proof. An approve step is where the run holds until the person says continue: keep the recipe's, add one (named approve) after a stage the person wants to read first, and skip one only when the person asks. Nothing posts outward before an approve step."
	talkBlockSay   = "Say what you set in three lines at most, then stop. Do not narrate."
)

// talkBlockInbox is the sixth block (the owner's decision of 2026-10-09): the
// manager conversation is the item's one inbox, and this is when it answers a
// step's question itself and when it sends it to the person
// (factory_inbox.go). The manual quotes it (internal/manual/chat/factory.md).
const talkBlockInbox = "You are the inbox: every question a stage asks comes to you first, and you reply with factory_answer. Answer it yourself when the issue, the recipe, the stages, the notes or what the person said here settle it. Send it to the person (ask_person, one line of why) when it turns on taste, scope, risk, credentials or access, money, or anything those do not settle; the stage waits, and the person answers here."

// talkBlocks is the five blocks for one item, the ref and the repository
// filled in.
func talkBlocks(it factory.Item) []string {
	repo := strings.TrimSpace(it.Repo)
	if repo == "" {
		repo = "this repository"
	}
	return []string{fmt.Sprintf(talkBlockWho, it.Ref(), repo), talkBlockKnow, talkBlockDo, talkBlockShape, talkBlockInbox, talkBlockSay}
}

// talkItemDoor is the brief's one sentence about `factory_item`, which keeps
// what is not the stages: the budget, thinking and notes.
const talkItemDoor = "factory_item changes the budget (its field is cap), the thinking of every stage (its field is effort) and notes for the stages, behind a card the person answers. Its item field is %d; never say that number, call the item %s."

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

// managerMaker is the runner's Options.Manager: the item's own conversation,
// made by the Talk door's own maker ([talkMaker]) when the item has none, and
// named the lead of the item's team either way, so a conversation `T` made
// before this build, or before the team had a lead, is the lead from its next
// run on.
func managerMaker(st *store.Store, workspace, profileDir string) func(context.Context, factory.Item) (string, error) {
	mint := talkMaker(st, workspace, profileDir)
	dirs := factoryRepoDirs(st, workspace)
	return func(ctx context.Context, it factory.Item) (string, error) {
		chat := strings.TrimSpace(it.Talk)
		if chat == "" {
			return mint(ctx, it)
		}
		where := strings.TrimSpace(dirs(it.Repo))
		if where == "" {
			where = strings.TrimSpace(workspace)
		}
		if err := talkJoinTeam(profileDir, it, chat, where, talkTeamName(it)); err != nil {
			return "", err
		}
		return chat, nil
	}
}

// sessionTalk is the runner's [factoryrun.Talk] over internal/session's
// journal doors: a line goes in as the manager's own message, marked
// factory-progress, and what the person typed comes out by its instant.
type sessionTalk struct{}

// Say appends one line; a conversation that is not there any more is
// [factoryrun.ErrTalkGone], so the runner stops owing it lines.
func (sessionTalk) Say(transcript, line string) error {
	err := session.AppendProgress(transcript, line)
	if errors.Is(err, os.ErrNotExist) {
		return factoryrun.ErrTalkGone
	}
	return err
}

// Heard is what the person typed after the instant after.
func (sessionTalk) Heard(transcript string, after time.Time) ([]factoryrun.Heard, error) {
	lines, err := session.PersonLines(transcript, after)
	out := make([]factoryrun.Heard, 0, len(lines))
	for _, l := range lines {
		out = append(out, factoryrun.Heard{At: l.At, Words: l.Words})
	}
	return out, err
}

// LastSaid is the last sentence a conversation's model said.
func (sessionTalk) LastSaid(transcript string) string { return session.LastSaid(transcript) }

// talkTeamName is the item's team's name and its conversation's: the floor's
// ref and the title, `#12 · fix the ledger double count`.
func talkTeamName(it factory.Item) string {
	return it.Ref() + " · " + strings.Join(strings.Fields(it.Title), " ")
}

// talkJoinTeam puts the conversation in the item's team, under the one
// `factory` team, AS ITS LEAD, in ONE read-modify-write of the teams file. The
// item's team is the open one of its name under `factory` when a run already
// made it for its stages ([factoryItemTeam]), and is made here otherwise; the
// `factory` team is the open top-level team of that name, made here the first
// time any item asks for one. Called again it changes nothing.
//
// A LEAD THE TEAMS FILE REFUSES (one conversation leads one team and the
// teams under it) leaves the conversation a member: the item still has its
// conversation, and the Teams place says who leads.
func talkJoinTeam(profileDir string, it factory.Item, transcript, where, name string) error {
	key := transcript
	if real, err := filepath.EvalSymlinks(transcript); err == nil {
		key = real
	}
	key = filepath.Clean(key)
	now := time.Now()
	return teams.Update(profileDir, func(f *teams.File) error {
		id := itemTeamIn(f, name, now)
		if err := f.AddMember(id, teams.Member{Key: key, File: transcript, Where: where, Word: name, Handle: talkHandle(it), JoinedAt: now}); err != nil {
			return err
		}
		if t, ok := f.Team(id); ok && t.Manager == key {
			return nil
		}
		if f.CheckManager(id, key) == nil {
			_ = f.SetManager(id, key)
		}
		return nil
	})
}

// itemTeamIn is the item's team in f, named name under the one `factory`
// team: the open one there, or one made now. It is called inside a
// teams.Update, so finding and making are one read-modify-write.
func itemTeamIn(f *teams.File, name string, now time.Time) string {
	parent := factoryParentTeam(f, now)
	for _, t := range f.Teams {
		if t.Parent == parent && t.Name == name && !t.Closed() {
			return t.ID
		}
	}
	id := teams.NewID()
	f.Teams = append(f.Teams, teams.Team{ID: id, Name: name, Parent: parent, Made: now})
	return id
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

// talkBrief is the opening note: the marker, the manager's five blocks
// ([talkBlocks]), and then the item as the floor knows it: its name and
// facts, budget and thinking, its body, what codeaf read of it,
// its stages with their asks and loops, the recipe's policy and habits, the
// notes for its stages, and the one sentence about `factory_item`.
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
	gap := func() { b.WriteByte('\n') }
	// THE MARKER IS THE FIRST LINE, and a surface draws the item's live card
	// in its place (internal/tui3's factoryitemcard.go), so the person sees the
	// item where the model sees its brief.
	line(talkMarker(it))
	for _, block := range talkBlocks(it) {
		line(block)
	}
	// THE RECIPE LAW is one more line of the brief (factory_law.go): a fixed
	// stage is the team's, not the manager's.
	line(talkLawLine())
	gap()
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
	staged := session.ItemStaged(it, recipe)
	// THE CAP AND THE THINKING ARE SAID AS WHAT THEY ARE, `budget $5 · thinking strong`,
	// and the labels on a line of their own: the conversation is read by a
	// model and by a person, and neither knows a "chip" as anything but a
	// shape some screen draws.
	var set, labels []string
	add2 := func(list *[]string, s string) {
		if s = strings.TrimSpace(s); s != "" {
			*list = append(*list, s)
		}
	}
	if it.Cap > 0 {
		add2(&set, "budget $"+strconv.FormatFloat(it.Cap, 'f', -1, 64))
	}
	if e := session.ItemEffort(staged); e != "" {
		add2(&set, "thinking "+e)
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
		gap()
		line(body)
		gap()
	}
	if read := strings.TrimSpace(it.Triage.Read); read != "" {
		line("codeaf read it: " + read)
	}
	if lines := factory.StageLines(staged.Stages); len(lines) > 0 {
		line("stages:")
		for _, l := range lines {
			line(l)
		}
	}
	if len(recipe.Policy) > 0 {
		line("policy:")
		for _, p := range recipe.Policy {
			line("- " + p)
		}
	}
	if len(recipe.Habits) > 0 {
		line("habits:")
		for _, h := range recipe.Habits {
			line("- " + h)
		}
	}
	for _, note := range it.Notes {
		line("note for the stages: " + note)
	}
	line(fmt.Sprintf(talkItemDoor, it.ID, it.Ref()))
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
