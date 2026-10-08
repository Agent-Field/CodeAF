package factory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
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
// [WithRunner] or [WithMailbox] nothing here launches, stops, pauses, answers,
// steers, signs off, sends back or re-checks, and nothing ever syncs or asks an
// author, so the surface draws no key for any of those: A CAPABILITY THAT
// CANNOT WORK IS ABSENT, NOT BROKEN. [WithRunner] binds the eight runner doors
// directly in the one process that runs the floor's items, and [WithMailbox]
// binds the same eight in every other window as asks posted to that process.
//
// A NIL STORE IS NO FLOOR: the zero Seam, whose every door is absent.
//
// The two bank doors are the exception, and only with [WithRepoDirs]: when
// the seam is told where a repository is checked out, a repo's recipe and
// habits are read from its `.codeaf/factory.md` (recipefile.go), BankStages
// writes an item's stages into that file and Bank appends a habit to it.
// Without that option they stay nil, because there is nowhere to write.
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
		SetGate: func(id int, g Gate) error {
			switch g {
			case GatePlan, GateShip, GateNone:
			default:
				return fmt.Errorf("%q is not a gate", g)
			}
			return st.Update(id, func(it *Item) error {
				it.Gate = g
				return nil
			})
		},
		SetCap: func(id int, usd float64) error {
			if usd < 0 {
				return errors.New("a cap is never below nothing")
			}
			return st.Update(id, func(it *Item) error {
				it.Cap = usd
				return nil
			})
		},
		SetStage: func(id, index int, on bool) error {
			return st.Update(id, func(it *Item) error {
				if index < 0 || index >= len(it.Stages) {
					return fmt.Errorf("there is no stage %d", index+1)
				}
				it.Stages[index].On = on
				return nil
			})
		},
		AddStage: func(id int, words string) error {
			if ParseStage(words).Ask == "" {
				return errors.New("say what the stage should do")
			}
			return st.Update(id, func(it *Item) error {
				it.Stages = AddStageWords(it.Stages, words)
				return nil
			})
		},
		SetEffort: func(id int, stage int, effort string) error {
			switch effort {
			case "", "cheap", "strong":
			default:
				return fmt.Errorf("%q is not an effort", effort)
			}
			return st.Update(id, func(it *Item) error {
				if stage < 0 || stage >= len(it.Stages) {
					return fmt.Errorf("there is no stage %d", stage+1)
				}
				it.Stages[stage].Effort = effort
				return nil
			})
		},
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
	if o.dirs != nil {
		seam.BankStages = func(id int) error {
			it, err := localItem(st, id)
			if err != nil {
				return err
			}
			dir, err := o.dir(it.Repo)
			if err != nil {
				return err
			}
			return BankRecipeStages(dir, it.Kind, it.Stages)
		}
		seam.Bank = func(repo, sentence string) error {
			dir, err := o.dir(repo)
			if err != nil {
				return err
			}
			return BankRecipeHabit(dir, sentence)
		}
	}
	return seam
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

type localOptions struct {
	runner  RunnerDoors
	mailbox Mailbox
	dirs    func(repo string) string
	lister  RepoLister
	talk    func(ctx context.Context, it Item) (string, error)
	refetch func(ctx context.Context, it Item) error
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
	if o.dirs == nil || repo == "" {
		return DefaultRecipe()
	}
	d := strings.TrimSpace(o.dirs(repo))
	if d == "" {
		return DefaultRecipe()
	}
	r, _, _ := Load(d)
	return r
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
	snap := Snapshot{
		Now:   now,
		Items: items,
		Shift: Shift{Since: started},
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
		if it.Created.After(started) {
			snap.Shift.Arrived++
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
// (a cap, a gate, a round count, an effort, a security pass) and what is left,
// tidied, is the title. The stages are the repo's recipe for an issue, with
// the chips written onto them: rounds onto review, the effort onto write,
// security switched on.
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
		Repo:    repo,
		Places:  []string{repo},
		Kind:    KindIssue,
		Title:   title,
		Tier:    TierOwner,
		Origin:  OriginTerminal,
		State:   StateNew,
		Created: now,
		Changed: now,
		Cap:     c.Cap,
		Gate:    c.Gate,
		Triage:  Triage{Type: GuessType(title)},
		Stages:  CopyStages(recipe.For(KindIssue)),
	}
	if it.Gate == "" {
		it.Gate = GateShip
	}
	if i := StageIndex(it.Stages, "review"); c.Rounds > 0 && i >= 0 {
		it.Stages[i].Max = c.Rounds
	}
	if i := StageIndex(it.Stages, "security"); security && i >= 0 {
		it.Stages[i].On = true
	}
	if i := StageIndex(it.Stages, "write"); c.Effort != "" && i >= 0 {
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
	if o.dirs == nil {
		return
	}
	seam.RecipeAt = func(repo string) (Recipe, []Problem, string, error) {
		dir := strings.TrimSpace(o.dirs(repo))
		if dir == "" {
			return DefaultRecipe(), nil, "", nil
		}
		r, probs, err := Load(dir)
		return r, probs, dir, err
	}
	seam.SaveRecipe = func(repo string, r Recipe) error {
		dir, err := o.dir(repo)
		if err != nil {
			return err
		}
		return Save(dir, r)
	}
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

// WithRunner binds the eight runner doors onto the seam. ONLY THE PROCESS THAT
// RUNS THE FLOOR'S ITEMS IS HANDED ONE (cmd/codeaf's factory_run.go): a
// second runner over the same store would run the same item twice. A nil r
// binds nothing.
func WithRunner(r RunnerDoors) LocalOption {
	return func(o *localOptions) { o.runner = r }
}

// ── THE MAILBOX: EVERY RUNNER VERB FROM EVERY WINDOW ───────────────────────
//
// One process on a machine holds the floor's runner, because a second runner
// over the same store would run the same item twice. On the ordinary launch
// that process is the session host, NOT the window a person sits at, so the
// window cannot call the runner's doors itself. It asks instead: an [Ask] is
// posted to the store's mailbox, the owner drains the mailbox about once a
// second, carries each ask through the runner's own door ([Carry]) and writes
// a [Reply], and the window waits for that reply and says it. THE REFUSAL A
// PERSON READS IS THE RUNNER'S OWN SENTENCE, word for word, whichever window
// they pressed the key in.

// The eight verbs an ask carries, spelled once: the mailbox's file keeps them
// and [Carry] reads them back.
const (
	VerbLaunch   = "launch"
	VerbStop     = "stop"
	VerbPause    = "pause"
	VerbAnswer   = "answer"
	VerbSteer    = "steer"
	VerbSignOff  = "signoff"
	VerbSendBack = "sendback"
	VerbReverify = "reverify"
)

// Ask is one runner verb a window asked of the process that runs the floor.
// Yes, Words and Edited are the door's own arguments, each read only by the
// verbs that take it. Seq is the mailbox's number for it, which its [Reply]
// carries back; At is when it was posted.
type Ask struct {
	ID     int       `json:"id"`
	Verb   string    `json:"verb"`
	Yes    bool      `json:"yes,omitempty"`
	Words  string    `json:"words,omitempty"`
	Edited bool      `json:"edited,omitempty"`
	At     time.Time `json:"at"`
	Seq    int       `json:"seq"`
}

// Reply is the owner's answer to the ask numbered Seq: the door's refusal in
// its own words ("" when it did what was asked), and for a sign-off whether a
// habit is due to be offered. At is when it was written.
type Reply struct {
	Seq      int       `json:"seq"`
	Err      string    `json:"err,omitempty"`
	HabitDue bool      `json:"habitDue,omitempty"`
	At       time.Time `json:"at"`
}

// Mailbox is where a window posts an ask and waits for its reply.
// internal/factory/store's *Mailbox is the one that answers it; it is an
// interface here because that package imports this one.
type Mailbox interface {
	// Post appends ask and answers the number its reply will carry.
	Post(ask Ask) (int, error)
	// Wait answers the reply to seq, or [ErrRunnerSilent] when none comes
	// within timeout.
	Wait(seq int, timeout time.Duration) (Reply, error)
}

// ErrRunnerSilent is what a door posted through the mailbox answers when the
// process that runs the floor wrote no reply in time: nothing on this machine
// is draining the mailbox, which is almost always codeaf's own host having
// gone. The manual quotes it (internal/manual/chat/factory.md).
var ErrRunnerSilent = errors.New("the floor's runner did not answer · is codeaf running?")

// MailboxWait is how long a window waits for the owner's reply before it says
// [ErrRunnerSilent]. The owner drains about once a second, so this is several
// of its passes. A variable so a test can shorten it.
var MailboxWait = 5 * time.Second

// Carry is the owner's half of an ask: it calls the runner door the verb
// names and answers the reply, the door's error in its own words. A verb this
// build does not know is refused in words rather than dropped, so a newer
// window asking an older owner hears why nothing happened.
func Carry(r RunnerDoors, ask Ask) Reply {
	var (
		err error
		due bool
	)
	switch ask.Verb {
	case VerbLaunch:
		err = r.Launch(ask.ID)
	case VerbStop:
		err = r.Stop(ask.ID)
	case VerbPause:
		err = r.Pause(ask.ID)
	case VerbAnswer:
		err = r.Answer(ask.ID, ask.Yes, ask.Words)
	case VerbSteer:
		err = r.Steer(ask.ID, ask.Words)
	case VerbSignOff:
		due, err = r.SignOff(ask.ID, ask.Edited)
	case VerbSendBack:
		err = r.SendBack(ask.ID, ask.Words)
	case VerbReverify:
		err = r.Reverify(ask.ID)
	default:
		err = fmt.Errorf("the floor's runner does not know %q", ask.Verb)
	}
	reply := Reply{Seq: ask.Seq, HabitDue: due}
	if err != nil {
		reply.Err = err.Error()
	}
	return reply
}

// WithMailbox binds all eight runner doors onto the seam as asks posted to m
// and waited on, for a window whose process does not run the floor's items.
// A seam handed both a runner and a mailbox uses the runner: THE WINDOW THAT
// HOLDS THE LOCK KEEPS THE DIRECT DOORS. A nil m binds nothing.
func WithMailbox(m Mailbox) LocalOption {
	return func(o *localOptions) { o.mailbox = m }
}

// localRunner hangs the runner's doors, directly or through the mailbox, over
// the seam.
func localRunner(seam *Seam, o localOptions) {
	if r := o.runner; r != nil {
		seam.Launch = r.Launch
		seam.Stop = r.Stop
		seam.Pause = r.Pause
		seam.Answer = r.Answer
		seam.Steer = r.Steer
		seam.SignOff = r.SignOff
		seam.SendBack = r.SendBack
		seam.Reverify = r.Reverify
		return
	}
	m := o.mailbox
	if m == nil {
		return
	}
	ask := func(a Ask) (bool, error) {
		seq, err := m.Post(a)
		if err != nil {
			return false, err
		}
		reply, err := m.Wait(seq, MailboxWait)
		if err != nil {
			return false, err
		}
		if reply.Err != "" {
			return reply.HabitDue, errors.New(reply.Err)
		}
		return reply.HabitDue, nil
	}
	plain := func(a Ask) error {
		_, err := ask(a)
		return err
	}
	seam.Launch = func(id int) error { return plain(Ask{ID: id, Verb: VerbLaunch}) }
	seam.Stop = func(id int) error { return plain(Ask{ID: id, Verb: VerbStop}) }
	seam.Pause = func(id int) error { return plain(Ask{ID: id, Verb: VerbPause}) }
	seam.Answer = func(id int, yes bool, words string) error {
		return plain(Ask{ID: id, Verb: VerbAnswer, Yes: yes, Words: words})
	}
	seam.Steer = func(id int, words string) error { return plain(Ask{ID: id, Verb: VerbSteer, Words: words}) }
	seam.SignOff = func(id int, edited bool) (bool, error) {
		return ask(Ask{ID: id, Verb: VerbSignOff, Edited: edited})
	}
	seam.SendBack = func(id int, words string) error { return plain(Ask{ID: id, Verb: VerbSendBack, Words: words}) }
	seam.Reverify = func(id int) error { return plain(Ask{ID: id, Verb: VerbReverify}) }
}
