package factory

import (
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
// EVERY ENGINE DOOR IS NIL. Nothing here launches, stops, pauses, answers,
// steers, signs off, sends back, re-checks, banks, syncs or asks an author,
// so the surface draws no key for any of those: A CAPABILITY THAT CANNOT WORK
// IS ABSENT, NOT BROKEN. Tick and Sleep are nil too, because a real floor
// keeps no clock of its own and draws no speed.
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
		Load: func() (Snapshot, error) { return localLoad(st, started, time.Now(), o.recipe) },
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

type localOptions struct {
	dirs func(repo string) string
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
// the chips written onto them the way the mock writes them: rounds onto
// review, the effort onto write, security switched on.
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
