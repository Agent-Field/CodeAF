package factory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ItemStore is what the local seam needs of a store of items, and
// internal/factory/store's *Store is the one that answers it. It is an
// interface here rather than that type because the store speaks this
// package's [Item], so this package cannot import it back.
//
// Root answers "" for a store that is not there, which is how a nil *Store
// handed in through this interface is still seen as no store at all.
type ItemStore interface {
	Root() string
	List() ([]Item, error)
	Create(Item) (Item, error)
	Update(id int, change func(*Item) error) error
}

// LocalSeam is the factory as this machine alone can run it today: a floor of
// the items kept in st, and the doors that change an item's own words, chips
// and stages. started is when this window opened, which is where the
// handover's shift begins.
//
// EVERY ENGINE DOOR IS NIL unless the launch hands one in. Without
// [WithRunner] nothing here launches, stops, pauses, answers, steers, signs
// off, sends back or re-checks, and nothing ever syncs or asks an author, so
// the surface draws no key for any of those: A CAPABILITY THAT CANNOT WORK IS
// ABSENT, NOT BROKEN. [WithRunner] binds the eight runner doors; every window
// is handed the lease's, which queue a job for a worker and signal its lease.
//
// A NIL STORE IS NO FLOOR: the zero Seam, whose every door is absent.
//
// Banking requires both [WithRepoDirs] and [WithRecipeFiles]. The pipeline
// interpreter supplies the YAML writer, so every save uses the same encoder.
// Without either option the write doors stay absent.
func LocalSeam(st ItemStore, started time.Time, opts ...LocalOption) Seam {
	if st == nil || st.Root() == "" {
		return Seam{}
	}
	var o localOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	seam := Seam{
		Load: func() (Snapshot, error) {
			snap, err := localLoad(st, started, time.Now(), o.recipe)
			// THE RAIL IS THE STORE'S, read with the floor so the handover's
			// money clause and the floor it sits over are one reading.
			if keeper, ok := st.(RailKeeper); ok && err == nil {
				snap.Rail, _ = keeper.Rail()
			}
			// WHAT IS IN FLIGHT IS THE STORE'S TOO, because the process doing
			// the work (the triage worker, a refresh) is not always the one
			// drawing it: the window reads it here, within one poll of it
			// starting.
			if keeper, ok := st.(BusyKeeper); ok && err == nil {
				snap.Busy, snap.BusyAll, _ = keeper.Busy()
			}
			if keeper, ok := st.(ReadCoster); ok && err == nil {
				snap.LastReadCost, _, _ = keeper.ReadCost()
			}
			// WHERE EACH REPOSITORY IS CHECKED OUT is read with the floor, so
			// `r` can see an item has nowhere to run before it starts one
			// ([Snapshot.Checkouts]). Only a seam told where repositories are
			// checked out says anything; one that was not says nothing (nil).
			if o.dirs != nil && err == nil {
				snap.Checkouts = localCheckouts(snap.Repos, o.dirs)
			}
			return snap, err
		},
		New: func(repo, words string) (int, error) {
			return localNew(st, repo, words, time.Now(), o.recipe(strings.TrimSpace(repo)))
		},
		Dismiss: func(id int) error {
			return st.Update(id, func(it *Item) error {
				it.State = StateDismissed
				return nil
			})
		},
		SetCap: func(id int, usd float64) error {
			if usd < 0 {
				return errors.New("a budget is never below nothing")
			}
			return st.Update(id, func(it *Item) error {
				it.Cap = usd
				return nil
			})
		},
		// THE PERSON'S OWN ROADS GO THROUGH THE SAME BOUND as every edit
		// ([Edit]): a stage the recipe file fixes is never switched off, and
		// its thinking never changes, with the one sentence ([FixedRefusal]).
		// Switching a fixed stage on is allowed.
		SetStage: func(id, index int, on bool) error {
			recipe := o.itemRecipe(st, id)
			return st.Update(id, func(it *Item) error {
				if index < 0 || index >= len(it.Stages) {
					return fmt.Errorf("there is no stage %d", index+1)
				}
				if !on && it.Stages[index].On && StageFixed(it.Stages, index, recipe, it.Kind) {
					return FixedRefusal(it.Stages[index].Name)
				}
				it.Stages[index].On = on
				return nil
			})
		},
		AddStage: func(id int, words string) error {
			if _, _, err := StageSentence(words); err != nil {
				return err
			}
			return st.Update(id, func(it *Item) error {
				next, err := AddStageSentence(it.Stages, words)
				if err != nil {
					return err
				}
				if added, _, _ := StageSentence(words); StageIndex(next, added.Name) >= 0 {
					next[StageIndex(next, added.Name)].By = ByYou
				}
				it.Stages = next
				return nil
			})
		},
		SetEffort: func(id int, stage int, effort string) error {
			switch effort {
			case "", "cheap", "strong":
			default:
				return fmt.Errorf("%q is not a thinking level", effort)
			}
			recipe := o.itemRecipe(st, id)
			return st.Update(id, func(it *Item) error {
				if stage < 0 || stage >= len(it.Stages) {
					return fmt.Errorf("there is no stage %d", stage+1)
				}
				if effort != it.Stages[stage].Effort && StageFixed(it.Stages, stage, recipe, it.Kind) {
					return FixedRefusal(it.Stages[stage].Name)
				}
				it.Stages[stage].Effort = effort
				return nil
			})
		},
	}
	// THE EDIT DOOR knows by itself which bounds apply: an item none of whose
	// stages has started is shaped before its run, and one with a stage begun
	// is edited in its run, the tail only.
	seam.Edit = func(_ context.Context, id int, e RunEdit) (Item, []string, error) {
		var out Item
		var lines []string
		err := st.Update(id, func(it *Item) error {
			mode := EditBeforeRun
			if len(startedStages(*it)) > 0 {
				mode = EditInRun
			}
			next, l, err := Edit(*it, e, o.recipe(it.Repo), mode)
			if err != nil {
				return err
			}
			*it = next
			out, lines = next, l
			return nil
		})
		if err != nil {
			return Item{}, nil, err
		}
		return out, lines, nil
	}
	localSettings(&seam, st, o)
	localRunner(&seam, o)
	// THE FLOOR'S OWN MARKS, kept by the store when it keeps them (marks.go).
	localMarks(&seam, st)
	if o.talk != nil {
		seam.Talk = func(ctx context.Context, id int) (string, error) {
			return localTalk(ctx, st, id, o.talk)
		}
	}
	if o.refetch != nil {
		localRefresh(&seam, st, o.refetch)
	}
	if o.dirs != nil && o.files != nil {
		seam.BankStages = func(id int) error {
			it, err := localItem(st, id)
			if err != nil {
				return err
			}
			dir, err := o.dir(it.Repo)
			if err != nil {
				return err
			}
			return o.files.BankStages(dir, it.Kind, it.Stages)
		}
		seam.BankNote = func(repo, sentence string) (string, error) {
			dir, err := o.dir(repo)
			if err != nil {
				return "", err
			}
			return BankRecipeChange(context.Background(), dir, sentence, "A habit the factory offered after clean sign-offs.", "- "+sentence,
				func(d string) error { return o.files.BankRule(d, sentence) })
		}
		seam.Bank = func(repo, sentence string) error {
			_, err := seam.BankNote(repo, sentence)
			return err
		}
	}
	return seam
}

// localCheckouts is the folder each repository on the floor is checked out
// in, by its name, leaving out the ones dir does not know. Never nil.
func localCheckouts(repos []Repo, dir func(repo string) string) map[string]string {
	out := make(map[string]string, len(repos))
	for _, r := range repos {
		if d := strings.TrimSpace(dir(r.Name)); d != "" {
			out[r.Name] = d
		}
	}
	return out
}

// LocalOption changes how [LocalSeam] is built.
type LocalOption func(*localOptions)

// WithRepoDirs tells the local seam where each repository is checked out:
// dir answers the folder for a repo's name, or "" when this machine does not
// know it. THE FOLDER IS THE PERSON'S OWN CHECKOUT OF THE TRUNK, never a
// worktree an item's run made, because the recipe read from it is the policy
// every item on that repository runs under ([Load]).
func WithRepoDirs(dir func(repo string) string) LocalOption {
	return func(o *localOptions) { o.dirs = dir }
}

// WithTalk gives the local seam its [Seam.Talk] door: maker is what makes an
// item's conversation and answers its session file. It is a func handed in
// rather than anything this package builds, because a conversation is the
// session engine's and its team is the teams file's, and THIS PACKAGE IMPORTS
// NEITHER. A seam built without it has no Talk door, and the floor draws no
// `T`: a launch with no conversations to make (--once, a far host) leaves it
// out.
func WithTalk(maker func(ctx context.Context, it Item) (string, error)) LocalOption {
	return func(o *localOptions) { o.talk = maker }
}

// WithRecipeLoader gives every recipe read the same interpreter as the runner.
// It is injected because the pipeline package already depends on this vocabulary.
func WithRecipeLoader(load func(string) (Recipe, []Problem, error)) LocalOption {
	return func(o *localOptions) { o.loadRecipe = load }
}

// WithRepositoryRecipeLoader reads rules by repository even without a checkout.
func WithRepositoryRecipeLoader(load func(repo, dir string) (Recipe, []Problem, error)) LocalOption {
	return func(o *localOptions) { o.loadRepoRecipe = load }
}

func (o localOptions) defaultRecipe() Recipe {
	if o.loadRecipe != nil {
		r, _, _ := o.loadRecipe("")
		return r
	}
	return DefaultRecipe()
}

func (o localOptions) load(dir string) (Recipe, []Problem, error) {
	if o.loadRecipe != nil {
		return o.loadRecipe(dir)
	}
	return Load(dir)
}

type localOptions struct {
	loadRepoRecipe func(string, string) (Recipe, []Problem, error)
	loadRecipe     func(string) (Recipe, []Problem, error)
	files          RecipeFiles
	runner         RunnerDoors
	dirs           func(repo string) string
	lister         RepoLister
	talk           func(ctx context.Context, it Item) (string, error)
	refetch        func(ctx context.Context, it Item) error
}

// WithRefetch gives the local seam its three source doors, [Seam.Refresh],
// [Seam.RefreshAll] and [Seam.Open]: refetch reads one item again from the
// source it came from and folds what it read into the store
// (internal/factory/github's Refetcher is the one for GitHub). It is a func
// handed in because THIS PACKAGE KNOWS NO VENDOR. A seam built without it has
// none of the three doors, and the floor draws no `u`, `U` or `g`.
func WithRefetch(refetch func(ctx context.Context, it Item) error) LocalOption {
	return func(o *localOptions) { o.refetch = refetch }
}

// BusyKeeper is a store that keeps what is in flight on the floor, so a
// window can draw work another process is doing. internal/factory/store's
// *Store is one; a word of "" clears an item, and an empty all clears the
// floor's line.
type BusyKeeper interface {
	SetBusy(id int, word string) error
	SetBusyAll(words string) error
	Busy() (items map[int]string, all string, err error)
}

// ReadCoster is a store that remembers what triage reads cost: the last one,
// and the average of every one it was told about. Zero is not known.
type ReadCoster interface {
	ReadCost() (last, avg float64, err error)
}

// annotator is a store that can write an item without stamping Changed,
// which is how a read taken off for a refresh leaves the row's age alone.
type annotator interface {
	Annotate(id int, change func(*Item) error) error
}

// ErrNotOnSource is what [Seam.Open] answers for an item that has no page on
// any source: work typed in the terminal, split off a chat, or read before
// items carried their address.
var ErrNotOnSource = errors.New("this item is not on github")

// The busy words the floor draws, spelled once. Reading and refreshing are an
// item being read RIGHT NOW, and only those spin on the floor; waiting is an
// item queued for a read that has not reached it yet, which a whole floor's
// re-read may write for every item behind the one it is on.
const (
	BusyReading    = "reading"
	BusyRefreshing = "refreshing"
	BusyWaiting    = "waiting"
)

// localRefresh hangs the three source doors over st.
func localRefresh(seam *Seam, st ItemStore, refetch func(context.Context, Item) error) {
	busy, _ := st.(BusyKeeper)
	refresh := func(ctx context.Context, it Item) error {
		if busy != nil {
			_ = busy.SetBusy(it.ID, BusyRefreshing)
			defer func() { _ = busy.SetBusy(it.ID, "") }()
		}
		// ONLY AN ITEM WITH A PLACE ON ITS SOURCE IS READ AGAIN. A terminal or
		// chat item has nothing upstream; its refresh is a fresh read alone.
		if it.Origin == OriginForge && it.Num > 0 {
			if err := refetch(ctx, it); err != nil {
				return err
			}
		}
		unread := func(cur *Item) error {
			ClearRead(&cur.Triage)
			return nil
		}
		if a, ok := st.(annotator); ok {
			return a.Annotate(it.ID, unread)
		}
		return st.Update(it.ID, unread)
	}
	seam.Refresh = func(ctx context.Context, id int) error {
		it, err := localItem(st, id)
		if err != nil {
			return err
		}
		return refresh(ctx, it)
	}
	seam.RefreshAll = func(ctx context.Context) (int, float64, error) {
		items, err := st.List()
		if err != nil {
			return 0, 0, err
		}
		var todo []Item
		for _, it := range items {
			if it.State != StateDismissed {
				todo = append(todo, it)
			}
		}
		each := DefaultReadCost
		if c, ok := st.(ReadCoster); ok {
			if _, avg, err := c.ReadCost(); err == nil && avg > 0 {
				each = avg
			}
		}
		count, est := len(todo), float64(len(todo))*each
		if IsDryRun(ctx) || count == 0 {
			return count, est, nil
		}
		line := func(done int) {
			if busy != nil {
				_ = busy.SetBusyAll(fmt.Sprintf("refreshing %d items · %d done", count, done))
			}
		}
		if busy != nil {
			defer func() { _ = busy.SetBusyAll("") }()
		}
		var errs []error
		for i, it := range todo {
			line(i)
			if ctx.Err() != nil {
				errs = append(errs, ctx.Err())
				break
			}
			if err := refresh(ctx, it); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", it.Ref(), err))
			}
		}
		return count, est, errors.Join(errs...)
	}
	seam.Open = func(id int) (string, error) {
		it, err := localItem(st, id)
		if err != nil {
			return "", err
		}
		if u := strings.TrimSpace(it.URL); u != "" {
			return u, nil
		}
		return "", ErrNotOnSource
	}
}

// localTalk is the Talk door: the item's conversation when it has one, and
// otherwise one made by maker and kept on the item. ONE PER ITEM: the store's
// read-modify-write keeps the first conversation written, so two asks that
// raced to make one both answer the same file.
func localTalk(ctx context.Context, st ItemStore, id int, maker func(context.Context, Item) (string, error)) (string, error) {
	it, err := localItem(st, id)
	if err != nil {
		return "", err
	}
	if chat := strings.TrimSpace(it.Talk); chat != "" {
		return chat, nil
	}
	// A DROPPED JOB HAS NO CONVERSATION AND IS NEVER GIVEN ONE: the drop
	// deleted its agent, and one made now would be an agent nothing runs for,
	// deleted again by Bring back ([RefuseDropped]).
	if err := RefuseDropped(it); err != nil {
		return "", err
	}
	chat, err := maker(ctx, it)
	if err != nil {
		return "", err
	}
	if chat = strings.TrimSpace(chat); chat == "" {
		return "", errors.New("the item's conversation could not be made")
	}
	kept := chat
	err = st.Update(id, func(it *Item) error {
		if held := strings.TrimSpace(it.Talk); held != "" {
			kept = held
			return nil
		}
		it.Talk = chat
		return nil
	})
	if err != nil {
		return "", err
	}
	return kept, nil
}

// dir is where repo is checked out, or an error that says it is not known.
func (o localOptions) dir(repo string) (string, error) {
	if d := strings.TrimSpace(o.dirs(repo)); d != "" {
		return d, nil
	}
	return "", fmt.Errorf("codeaf does not know where %s is checked out, so its recipe has nowhere to go", repo)
}

// recipe is repo's recipe: its file's when the folder is known, the default
// recipe otherwise. A FILE THAT CANNOT BE READ IS THE DEFAULT RECIPE, and a
// line that did not load is left out, so one repository's mistyped file never
// takes the whole floor down with it.
func (o localOptions) recipe(repo string) Recipe {
	if o.loadRepoRecipe != nil && repo != "" {
		dir := ""
		if o.dirs != nil {
			dir = strings.TrimSpace(o.dirs(repo))
		}
		r, _, _ := o.loadRepoRecipe(repo, dir)
		return r
	}
	if o.dirs == nil || repo == "" {
		return o.defaultRecipe()
	}
	d := strings.TrimSpace(o.dirs(repo))
	if d == "" {
		return o.defaultRecipe()
	}
	r, _, _ := o.load(d)
	return r
}

// itemRecipe is the recipe of the repository item id is on, read before the
// store's update so no git read happens inside it; the default recipe when
// the item or its folder is not known.
func (o localOptions) itemRecipe(st ItemStore, id int) Recipe {
	it, err := localItem(st, id)
	if err != nil {
		return DefaultRecipe()
	}
	return o.recipe(it.Repo)
}

// localItem is one item off the store, by its id.
func localItem(st ItemStore, id int) (Item, error) {
	items, err := st.List()
	if err != nil {
		return Item{}, err
	}
	for _, it := range items {
		if it.ID == id {
			return it, nil
		}
	}
	return Item{}, fmt.Errorf("there is no item %d", id)
}

// localLoad copies the floor out of the store. Everything on the snapshot is
// read off the items themselves: the repos are the ones the items name, the
// day's spend is what their streams spent today, and the shift's arrivals are
// what was made since the window opened. A FLOOR WITH NOTHING ON IT IS ALL
// ZEROES, which the surface draws as nothing.
func localLoad(st ItemStore, started, now time.Time, recipe func(repo string) Recipe) (Snapshot, error) {
	items, err := st.List()
	if err != nil {
		return Snapshot{}, err
	}
	items = IssueJobs(items)
	snap := Snapshot{
		Now:   now,
		Items: items,
		Shift: shiftSince(items, started, now),
		// THE CHAT IS ALWAYS A SOURCE, and the terminal is the other door work
		// comes in by. Neither writes outward: nothing here posts anywhere.
		Sources: []SourceInfo{{Name: string(OriginChat), Writes: false}, {Name: string(OriginTerminal)}},
	}
	seen := map[string]bool{}
	var names []string
	y, m, d := now.Date()
	for _, it := range items {
		if it.Repo != "" && !seen[it.Repo] {
			seen[it.Repo] = true
			names = append(names, it.Repo)
		}
		if it.State == StateRunning {
			snap.Benches++
		}
		if it.Stream != nil {
			if sy, sm, sd := it.Changed.In(now.Location()).Date(); sy == y && sm == m && sd == d {
				snap.Daily += it.Stream.Spent
			}
		}
	}
	sort.Strings(names)
	for _, name := range names {
		r := recipe(name)
		snap.Repos = append(snap.Repos, Repo{Name: name, Recipe: r, Habits: r.Habits})
	}
	return snap, nil
}

// localNew makes an item from words typed on the floor. The chips lift out
// (a cap, a gate word, a round count, an effort, a security pass) and what is left,
// tidied, is the title. The stages are the repo's recipe for an issue, with
// the chips written onto them: rounds onto review, the effort onto write,
// security switched on or added.
func localNew(st ItemStore, repo, words string, now time.Time, recipe Recipe) (int, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return 0, errors.New("say which repository the work is on")
	}
	c := Lift(words)
	security, rest := LiftSecurity(c.Rest)
	title := TidyWords(rest)
	if title == "" {
		return 0, errors.New("say what the work is, not only how")
	}
	it := Item{
		Autonomy: AutonomyAuto,
		Repo:     repo,
		Places:   []string{repo},
		Kind:     KindIssue,
		Title:    title,
		Tier:     TierOwner,
		Origin:   OriginTerminal,
		State:    StateNew,
		Created:  now,
		Changed:  now,
		Cap:      c.Cap,
		Ask:      &JobAsk{Text: title, At: now, Source: AskTUI},
		Triage:   Triage{Type: GuessType(title)},
		Stages:   CopyStages(recipe.For(KindIssue)),
	}
	// THE GATE WORDS PLACE APPROVE STEPS ([ApproveForGate]): `plan first`
	// one after plan, `ship it` one before the result, `self-ship` none, so
	// the item ships itself on green proof. A fixed approve stays.
	if c.Gate == GateNone {
		for i, s := range it.Stages {
			if IsApprove(s) && StageFixed(it.Stages, i, recipe, KindIssue) {
				return 0, FixedRefusal(s.Name)
			}
		}
	}
	if c.Gate != "" {
		it.Stages = ApproveForGate(it.Stages, c.Gate)
	}
	// A CHIP IS THE PERSON'S EDIT, under the same bound: rounds or thinking
	// laid on a stage the recipe fixes is refused in the one sentence, while
	// switching security on is always allowed.
	if i := StageIndex(it.Stages, "review"); c.Rounds > 0 && i >= 0 {
		if c.Rounds != it.Stages[i].Max && StageFixed(it.Stages, i, recipe, KindIssue) {
			return 0, FixedRefusal(it.Stages[i].Name)
		}
		it.Stages[i].Max = c.Rounds
	}
	// SECURITY IS IN NO DEFAULT LIST ([DefaultRecipe]): the chip switches on a
	// recipe's own security stage, or lays the shipped one after review.
	if security {
		if i := StageIndex(it.Stages, "security"); i >= 0 {
			it.Stages[i].On = true
		} else if sec, ok := ShippedStage("security"); ok {
			it.Stages = PlaceStage(it.Stages, sec, "after review")
		}
	}
	if i := StageIndex(it.Stages, "write"); c.Effort != "" && i >= 0 {
		if c.Effort != it.Stages[i].Effort && StageFixed(it.Stages, i, recipe, KindIssue) {
			return 0, FixedRefusal(it.Stages[i].Name)
		}
		it.Stages[i].Effort = c.Effort
	}
	made, err := st.Create(it)
	if err != nil {
		return 0, err
	}
	return made.ID, nil
}

// localSettings fills the floor's own settings doors (settings.go) that this
// machine can answer: the watched repositories and the rail when the store
// keeps them, and a repository's recipe file when [WithRepoDirs] says where
// repositories are checked out. EVERY OTHER SETTINGS DOOR STAYS NIL here; the
// GitHub connection is the launch's to fill, because only it knows the
// profile a token is kept in.
func localSettings(seam *Seam, st ItemStore, o localOptions) {
	if keeper, ok := st.(RepoKeeper); ok {
		list := o.lister
		seam.Repos = func(ctx context.Context) ([]string, []RepoInfo, error) {
			watched, err := keeper.Repos()
			if err != nil || list == nil {
				return watched, nil, err
			}
			available, err := list(ctx)
			// EACH REPOSITORY CARRIES WHERE IT IS CHECKED OUT, when this
			// machine knows, because the floor's stages run only there.
			if o.dirs != nil {
				for i := range available {
					available[i].Dir = strings.TrimSpace(o.dirs(available[i].Full))
				}
			}
			return watched, available, err
		}
		seam.SetRepos = keeper.SetRepos
	}
	if keeper, ok := st.(RailKeeper); ok {
		seam.SetRail = func(usd float64) error {
			if usd < 0 {
				return errors.New("a rail is never below nothing")
			}
			return keeper.SetRail(usd)
		}
	}
	if o.dirs == nil && o.loadRepoRecipe == nil {
		return
	}
	seam.RecipeAt = func(repo string) (Recipe, []Problem, string, error) {
		dir := ""
		if o.dirs != nil {
			dir = strings.TrimSpace(o.dirs(repo))
		}
		if o.loadRepoRecipe != nil {
			r, probs, err := o.loadRepoRecipe(repo, dir)
			return r, probs, dir, err
		}
		if dir == "" {
			return o.defaultRecipe(), nil, "", nil
		}
		r, probs, err := o.load(dir)
		return r, probs, dir, err
	}
	if o.dirs == nil || o.files == nil {
		return
	}
	seam.SaveRecipe = func(repo string, r Recipe) error {
		dir, err := o.dir(repo)
		if err != nil {
			return err
		}
		return o.files.Save(dir, r)
	}
	localRecipeOffer(seam, st, o)
}

// localRecipeOffer binds the first offer's three doors ([Seam.RecipeOffer]).
// THE NO IS THE STORE'S, kept in `<store>/recipe-offers.json` beside its
// other settings, so a repository put away in one window is put away in every
// window and after a restart.
func localRecipeOffer(seam *Seam, st ItemStore, o localOptions) {
	path := filepath.Join(st.Root(), "recipe-offers.json")
	var mu sync.Mutex
	seam.RecipeOffer = func(ctx context.Context, repo string) (bool, error) {
		dir := strings.TrimSpace(o.dirs(repo))
		if dir == "" {
			return false, nil
		}
		if _, err := os.Stat(filepath.Join(dir, PipelineFile)); !errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		mu.Lock()
		defer mu.Unlock()
		away, err := readRecipeOffers(path)
		if err != nil {
			return false, err
		}
		for _, r := range away {
			if strings.EqualFold(r, repo) {
				return false, nil
			}
		}
		return true, nil
	}
	seam.WriteRecipe = func(ctx context.Context, repo string) (string, error) {
		dir, err := o.dir(repo)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(filepath.Join(dir, PipelineFile)); !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%s has a recipe already · %s", repo, PipelineFile)
		}
		if err := o.files.WriteDefault(dir); err != nil {
			return "", err
		}
		return checkoutBranch(dir), nil
	}
	seam.RecipeNotNow = func(repo string) error {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			return errors.New("which repository?")
		}
		mu.Lock()
		defer mu.Unlock()
		away, err := readRecipeOffers(path)
		if err != nil {
			return err
		}
		for _, r := range away {
			if strings.EqualFold(r, repo) {
				return nil
			}
		}
		away = append(away, repo)
		sort.Strings(away)
		data, err := json.MarshalIndent(recipeOffersDoc{Schema: 1, NotNow: away}, "", "  ")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}
}

// recipeOffersDoc is `<store>/recipe-offers.json`: the repositories whose
// first offer of a recipe file was answered `not now`.
type recipeOffersDoc struct {
	Schema int      `json:"schema"`
	NotNow []string `json:"not_now"`
}

// readRecipeOffers is the repositories put away. NO FILE IS NONE.
func readRecipeOffers(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d recipeOffersDoc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("factory store: cannot read recipe-offers.json: %w", err)
	}
	return d.NotNow, nil
}

// checkoutBranch is the branch the checkout at dir stands on, read from its
// HEAD (following a worktree's `.git` file), and "" when HEAD is not a branch
// or cannot be read. NOTHING IS RUN: git's own files are read as they stand.
func checkoutBranch(dir string) string {
	gitDir := filepath.Join(dir, ".git")
	if info, err := os.Stat(gitDir); err == nil && !info.IsDir() {
		data, err := os.ReadFile(gitDir)
		if err != nil {
			return ""
		}
		rest, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
		if !ok {
			return ""
		}
		gitDir = strings.TrimSpace(rest)
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(dir, gitDir)
		}
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	ref, ok := strings.CutPrefix(strings.TrimSpace(string(head)), "ref: refs/heads/")
	if !ok {
		return ""
	}
	return ref
}

// RunnerDoors is the engine half of the seam: the eight doors that move an
// item through its stages. internal/factory/run's *Runner answers it; it is an
// interface here because that package imports this one.
type RunnerDoors interface {
	Launch(id int) error
	Stop(id int) error
	Pause(id int) error
	Answer(id int, yes bool, words string) error
	Steer(id int, words string) error
	SignOff(id int, edited bool) (bool, error)
	SendBack(id int, words string) error
	Reverify(id int) error
}

// HoldDoor is the pause with its intent said ([Seam.Hold]): on holds, off goes
// on, and an ask already true changes nothing. internal/factory/run's *Runner
// answers it; a runner that does not leaves the seam's Hold door absent.
type HoldDoor interface {
	Hold(id int, on bool) error
}

// WithRunner binds the eight runner doors onto the seam. A window's floor is
// handed the lease's doors (internal/factory/lease's Doors), which queue and
// signal rather than run; a test may hand a Runner itself. A nil r binds
// nothing.
func WithRunner(r RunnerDoors) LocalOption {
	return func(o *localOptions) { o.runner = r }
}

// localRunner hangs the runner's doors over the seam.
func localRunner(seam *Seam, o localOptions) {
	r := o.runner
	if r == nil {
		return
	}
	seam.Launch = r.Launch
	seam.Stop = r.Stop
	seam.Pause = r.Pause
	if h, ok := r.(HoldDoor); ok {
		seam.Hold = h.Hold
	}
	seam.Answer = r.Answer
	seam.Steer = r.Steer
	seam.SignOff = r.SignOff
	seam.SendBack = r.SendBack
	seam.Reverify = r.Reverify
}
