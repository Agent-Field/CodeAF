package mock

import (
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// New generates a world and returns it as a seam with EVERY door filled, the
// mock clock's included. The world behind it is reachable only through the
// doors.
func New(seed int64, repos, issues, benches int, now time.Time) factory.Seam {
	return NewWorld(seed, repos, issues, benches, now).Seam()
}

// Seam is the world's doors. Each takes the world's one lock for the whole of
// its work, so two doors called at once see each other's result whole.
func (w *World) Seam() factory.Seam {
	s := factory.Seam{
		Load: w.Load,
		// THE FLOOR'S OWN MARKS are the items' own Marked here, since the
		// made-up floor has no store to keep them in; only a new item takes one.
		Mark: func(ids []int, on bool) error {
			for _, id := range ids {
				if err := w.with(id, func(it *factory.Item) error {
					if on && it.State != factory.StateNew {
						return fmt.Errorf("only a new item takes a mark")
					}
					it.Marked = on
					return nil
				}); err != nil {
					return err
				}
			}
			return nil
		},
		Launch: func(id int) error {
			return w.with(id, func(it *factory.Item) error {
				if it.State == factory.StateLanded || it.State == factory.StateShipped {
					return fmt.Errorf("%s has already landed", it.Ref())
				}
				w.launch(it)
				return nil
			})
		},
		Stop: func(id int) error {
			return w.with(id, func(it *factory.Item) error { w.stop(it); return nil })
		},
		Pause: func(id int) error {
			return w.with(id, func(it *factory.Item) error {
				if it.Stream == nil || (it.State != factory.StateRunning && it.State != factory.StateNeedsYou) {
					return fmt.Errorf("%s is not on a bench", it.Ref())
				}
				it.Stream.Paused = !it.Stream.Paused
				it.Changed = w.now
				return nil
			})
		},
		Dismiss: func(id int) error {
			return w.with(id, func(it *factory.Item) error {
				if it.State == factory.StateRunning || it.State == factory.StateNeedsYou {
					return fmt.Errorf("%s is on a bench; stop it first", it.Ref())
				}
				it.State = factory.StateDismissed
				it.Changed = w.now
				return nil
			})
		},
		Answer: func(id int, yes bool, words string) error {
			return w.with(id, func(it *factory.Item) error { return w.answer(it, yes, words) })
		},
		Steer: func(id int, words string) error {
			return w.with(id, func(it *factory.Item) error { return w.steer(it, words) })
		},
		SignOff: func(id int, edited bool) (bool, error) {
			due := false
			err := w.with(id, func(it *factory.Item) error {
				var err error
				due, err = w.signOff(it, edited)
				return err
			})
			return due, err
		},
		SendBack: func(id int, words string) error {
			return w.with(id, func(it *factory.Item) error {
				if err := w.reopen(it, "prove", words, 6*time.Minute); err != nil {
					return err
				}
				w.say(it, glyphSaid, "said", "sent back: "+words)
				return nil
			})
		},
		Reverify: func(id int) error {
			return w.with(id, func(it *factory.Item) error {
				if err := w.reopen(it, "re-verify", "every check again", 4*time.Minute); err != nil {
					return err
				}
				w.say(it, glyphTest, "test", "re-running every check")
				return nil
			})
		},
		Bank: func(repo, sentence string) error {
			w.mu.Lock()
			defer w.mu.Unlock()
			r := w.repo(repo)
			if r == nil {
				return fmt.Errorf("no repo %s", repo)
			}
			r.Habits = append(r.Habits, sentence)
			return nil
		},
		New: func(repo, words string) (int, error) {
			w.mu.Lock()
			defer w.mu.Unlock()
			r := w.repo(repo)
			if r == nil {
				return 0, fmt.Errorf("no repo %s", repo)
			}
			it, err := w.newFromWords(r, words)
			if err != nil {
				return 0, err
			}
			return it.ID, nil
		},
		Sync: func(id int, on bool) error {
			return w.with(id, func(it *factory.Item) error {
				if it.Origin != factory.OriginTerminal {
					return fmt.Errorf("%s already lives on %s", it.Ref(), it.Origin)
				}
				it.Synced = on
				it.Changed = w.now
				return nil
			})
		},
		AskAuthor: func(id int) error {
			return w.with(id, func(it *factory.Item) error {
				if len(it.Triage.Questions) == 0 {
					return fmt.Errorf("%s has no questions for its author", it.Ref())
				}
				it.Triage.Questions = nil
				it.Changed = w.now
				return nil
			})
		},
		SetStage: func(id, index int, on bool) error {
			return w.with(id, func(it *factory.Item) error {
				if index < 0 || index >= len(it.Stages) {
					return fmt.Errorf("%s has no stage %d", it.Ref(), index)
				}
				it.Stages[index].On = on
				it.Changed = w.now
				return nil
			})
		},
		AddStage: func(id int, words string) error {
			return w.with(id, func(it *factory.Item) error {
				if parseStage(words).Ask == "" {
					return fmt.Errorf("say what the stage should do")
				}
				w.addItemStage(it, words)
				return nil
			})
		},
		BankStages: func(id int) error {
			return w.with(id, func(it *factory.Item) error {
				if it.Kind != factory.KindIssue {
					return fmt.Errorf("only an issue's stages bank onto the recipe")
				}
				r := w.repo(it.Repo)
				if r == nil {
					return fmt.Errorf("no repo %s", it.Repo)
				}
				r.Recipe.Stages = copyStages(it.Stages)
				return nil
			})
		},
		SetGate: func(id int, g factory.Gate) error {
			return w.with(id, func(it *factory.Item) error {
				switch g {
				case factory.GatePlan, factory.GateShip, factory.GateNone:
				default:
					return fmt.Errorf("no gate %q", g)
				}
				it.Gate = g
				it.Changed = w.now
				return nil
			})
		},
		SetCap: func(id int, usd float64) error {
			return w.with(id, func(it *factory.Item) error {
				if usd <= 0 {
					return fmt.Errorf("a cap is above zero")
				}
				it.Cap = usd
				it.Changed = w.now
				return nil
			})
		},
		SetEffort: func(id int, stage int, effort string) error {
			return w.with(id, func(it *factory.Item) error {
				if _, ok := effortRate[effort]; !ok {
					return fmt.Errorf("no effort %q; cheap, strong, or nothing", effort)
				}
				if stage < 0 || stage >= len(it.Stages) {
					return fmt.Errorf("%s has no stage %d", it.Ref(), stage)
				}
				it.Stages[stage].Effort = effort
				it.Changed = w.now
				return nil
			})
		},
		Tick: func(d time.Duration) error {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.tick(d)
			return nil
		},
		Sleep: func(d time.Duration) error {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.sleep(d)
			return nil
		},
	}
	w.settingsDoors(&s)
	w.refreshDoors(&s)
	return s
}

// addItemStage places a stage from words onto the item's own copy of the
// recipe. A RUN IS ITS STAGES AT LAUNCH, so a running stream keeps its phases;
// the bookkeeping that ties phases to stages is shifted to keep pointing at
// the same stages.
func (w *World) addItemStage(it *factory.Item, words string) {
	before := len(it.Stages)
	it.Stages = addStage(it.Stages, words)
	at := stageAt(it.Stages, words)
	if r := w.runs[it.ID]; r != nil && len(it.Stages) > before {
		for i, s := range r.stage {
			if s >= at {
				r.stage[i] = s + 1
			}
		}
	}
	it.Changed = w.now
}

func (w *World) with(id int, f func(*factory.Item) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	it := w.item(id)
	if it == nil {
		return fmt.Errorf("no item %d on the floor", id)
	}
	return f(it)
}

// Load copies the floor. THE SNAPSHOT SHARES NO MEMORY WITH THE WORLD: every
// slice and every stream is copied, so a frame may hold it while the
// simulation moves on.
func (w *World) Load() (factory.Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	snap := factory.Snapshot{
		Now:     w.now,
		Benches: w.benches,
		Daily:   w.daily,
		Rail:    w.rail,
		Shift:   w.shift,
		Speed:   w.speed,
	}
	snap.Shift.Shipping = append([]string(nil), w.shift.Shipping...)
	names := make([]string, 0, len(w.repos))
	for _, r := range w.repos {
		snap.Repos = append(snap.Repos, copyRepo(*r))
		names = append(names, r.Name)
	}
	snap.Items = make([]factory.Item, 0, len(w.items))
	for _, it := range w.items {
		c := copyItem(*it)
		c.URL = itemURL(c)
		snap.Items = append(snap.Items, c)
	}
	w.busyInto(&snap)
	snap.Sources = []factory.SourceInfo{
		{Name: string(factory.OriginChat), Writes: false},
		{Name: string(factory.OriginForge), Writes: true, Repos: names, Polled: w.now},
	}
	return snap, nil
}

func copyStages(in []factory.Stage) []factory.Stage {
	if in == nil {
		return nil
	}
	out := make([]factory.Stage, len(in))
	for i, s := range in {
		s.Proof = append([]string(nil), s.Proof...)
		out[i] = s
	}
	return out
}

func copyRepo(r factory.Repo) factory.Repo {
	r.Areas = append([]string(nil), r.Areas...)
	r.Habits = append([]string(nil), r.Habits...)
	r.Recipe.Stages = copyStages(r.Recipe.Stages)
	r.Recipe.Policy = append([]string(nil), r.Recipe.Policy...)
	return r
}

func copyItem(it factory.Item) factory.Item {
	it.Stages = copyStages(it.Stages)
	it.Labels = append([]string(nil), it.Labels...)
	it.Proof = append([]factory.Claim(nil), it.Proof...)
	it.Policy = append([]factory.Claim(nil), it.Policy...)
	it.Triage.Questions = append([]string(nil), it.Triage.Questions...)
	if it.Stream != nil {
		s := *it.Stream
		s.Phases = append([]factory.Phase(nil), s.Phases...)
		s.Activity = append([]int(nil), s.Activity...)
		s.Log = append([]factory.LogLine(nil), s.Log...)
		it.Stream = &s
	}
	return it
}
