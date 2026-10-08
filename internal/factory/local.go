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
func LocalSeam(st ItemStore, started time.Time) Seam {
	if st == nil || st.Root() == "" {
		return Seam{}
	}
	return Seam{
		Load: func() (Snapshot, error) { return localLoad(st, started, time.Now()) },
		New:  func(repo, words string) (int, error) { return localNew(st, repo, words, time.Now()) },
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
}

// localLoad copies the floor out of the store. Everything on the snapshot is
// read off the items themselves: the repos are the ones the items name, the
// day's spend is what their streams spent today, and the shift's arrivals are
// what was made since the window opened. A FLOOR WITH NOTHING ON IT IS ALL
// ZEROES, which the surface draws as nothing.
func localLoad(st ItemStore, started, now time.Time) (Snapshot, error) {
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
		snap.Repos = append(snap.Repos, Repo{Name: name, Recipe: DefaultRecipe()})
	}
	return snap, nil
}

// localNew makes an item from words typed on the floor. The chips lift out
// (a cap, a gate, a round count, an effort, a security pass) and what is left,
// tidied, is the title. The stages are the default recipe for an issue, with
// the chips written onto them the way the mock writes them: rounds onto
// review, the effort onto write, security switched on.
func localNew(st ItemStore, repo, words string, now time.Time) (int, error) {
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
		Stages:  CopyStages(DefaultRecipe().For(KindIssue)),
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
