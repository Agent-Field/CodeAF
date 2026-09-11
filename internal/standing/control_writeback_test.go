package standing

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/effort"
)

// The callback changes the saved item while an admitted operation is outside
// the owner. This fixes the ordering without sleeps or a probabilistic race.
type controlWritebackRunner struct {
	onSay func(Item)
	calls int
	fail  error
}

func (r *controlWritebackRunner) Probe(context.Context, Item) (string, error) { return "", nil }
func (r *controlWritebackRunner) Run(context.Context, Item, string, string) (Outcome, error) {
	return Outcome{}, errors.New("unexpected task execution")
}
func (r *controlWritebackRunner) Say(_ context.Context, item Item, _ string) (Outcome, error) {
	r.calls++
	if r.onSay != nil {
		r.onSay(item)
	}
	return Outcome{Kind: "said", USD: .02}, r.fail
}

func controlWritebackItem(t *testing.T) (*Store, Item, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	item := reminder("Every 30 minutes report", time.Time{})
	item.When = When{Kind: WhenEvery, Words: "every 30 minutes", Every: "30m"}
	made, err := store.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	due := now.Add(31 * time.Minute)
	store.clock = held(due)
	return store, made, due
}

func saveControlWriteback(t *testing.T, store *Store, id string, edit func(*Item)) Item {
	t.Helper()
	item, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	edit(&item)
	if err := store.Save(item); err != nil {
		t.Fatal(err)
	}
	return item
}

func TestControlWritebackPreservesPauseAndStopDuringFiring(t *testing.T) {
	for _, status := range []Status{StatusPaused, StatusRetired} {
		for _, failed := range []bool{false, true} {
			name := string(status) + "/success"
			if failed {
				name = string(status) + "/failure"
			}
			t.Run(name, func(t *testing.T) {
				store, made, now := controlWritebackItem(t)
				runner := &controlWritebackRunner{onSay: func(Item) {
					saveControlWriteback(t, store, made.ID, func(item *Item) {
						item.Status = status
						if status == StatusRetired {
							item.RetiredWhy = "stopped by you"
						}
					})
				}}
				if failed {
					runner.fail = errors.New("delivery failed")
				}
				ticker := newTicker(store, runner, now)
				pass := mustTick(t, ticker)
				got, err := store.Get(made.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Status != status {
					t.Errorf("late result undid %s: got %s", status, got.Status)
				}
				if status == StatusRetired && got.RetiredWhy != "stopped by you" {
					t.Errorf("stop reason lost: %q", got.RetiredWhy)
				}
				if !failed && (got.Runs != 1 || got.LastOutcome != "said" || pass.Fired != 1) {
					t.Errorf("preserving control lost the completed occurrence: %+v, pass %+v", got, pass)
				}
				if failed && (pass.Errors != 1 || got.LastCheckLine == "") {
					t.Errorf("failure was not recorded: %+v, pass %+v", got, pass)
				}
				ticker.Now = held(now.Add(2 * time.Hour))
				store.clock = ticker.Now
				mustTick(t, ticker)
				if runner.calls != 1 {
					t.Errorf("controlled item ran again: %d calls", runner.calls)
				}
			})
		}
	}
}

func TestControlWritebackPreservesControlDuringJudgment(t *testing.T) {
	for _, status := range []Status{StatusPaused, StatusRetired} {
		for _, result := range []string{"quiet", "ready", "failure"} {
			t.Run(string(status)+"/"+result, func(t *testing.T) {
				store, made, now := controlWritebackItem(t)
				saveControlWriteback(t, store, made.ID, func(item *Item) {
					item.When = When{Kind: WhenProbe, Probe: Probe{Command: "true"}, Hint: "if there is news"}
				})
				runner := &controlWritebackRunner{}
				ticker := newTicker(store, runner, now)
				ticker.Sentinel = func(context.Context, Judgment) (bool, string, float64, error) {
					saveControlWriteback(t, store, made.ID, func(item *Item) {
						item.Status = status
						if status == StatusRetired {
							item.RetiredWhy = "stopped by you"
						}
					})
					if result == "failure" {
						return false, "", 0, errors.New("judgment failed")
					}
					return result == "ready", "checked for news", .01, nil
				}
				mustTick(t, ticker)
				got, err := store.Get(made.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Status != status {
					t.Errorf("judgment undid %s: got %s", status, got.Status)
				}
				if runner.calls != 0 {
					t.Errorf("a result admitted action after %s: %d calls", status, runner.calls)
				}
			})
		}
	}
}

func TestControlWritebackPreservesRevisedConfiguration(t *testing.T) {
	store, made, now := controlWritebackItem(t)
	var revised Item
	runner := &controlWritebackRunner{onSay: func(Item) {
		revised = saveControlWriteback(t, store, made.ID, func(item *Item) {
			item.Grant = "Only prepare a draft; do not send"
			item.Exceptions = []Exception{{SessionID: "private-conversation"}}
			item.Brief = Brief{Title: "New responsibility", Prompt: "Use the revised instructions"}
			item.When = When{Kind: WhenEvery, Words: "every two hours", Every: "2h"}
			item.NextDue = now.Add(2 * time.Hour)
			item.Does.Say = "The revised report"
			item.Rails.MaxPerDay = 2
		})
	}}
	mustTick(t, newTicker(store, runner, now))
	got, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Grant != revised.Grant || !reflect.DeepEqual(got.Exceptions, revised.Exceptions) || got.Brief != revised.Brief || !reflect.DeepEqual(got.When, revised.When) || got.Does.Say != revised.Does.Say || got.Rails.MaxPerDay != revised.Rails.MaxPerDay || !got.NextDue.Equal(revised.NextDue) {
		t.Errorf("completion overwrote revised instructions or schedule: got %+v, want configuration %+v", got, revised)
	}
	if got.Runs != 1 || got.LastOutcome != "said" {
		t.Errorf("configuration preservation dropped occurrence evidence: %+v", got)
	}
}

func TestControlWritebackDoesNotRetireARevisedOneShot(t *testing.T) {
	store, made, now := controlWritebackItem(t)
	saveControlWriteback(t, store, made.ID, func(item *Item) {
		item.When = When{Kind: WhenAt, At: now.Add(-time.Minute)}
	})
	runner := &controlWritebackRunner{onSay: func(Item) {
		saveControlWriteback(t, store, made.ID, func(item *Item) {
			item.When = When{Kind: WhenEvery, Every: "2h", Words: "every two hours"}
			item.NextDue = now.Add(2 * time.Hour)
		})
	}}
	mustTick(t, newTicker(store, runner, now))
	got, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusActive || got.When.Kind != WhenEvery || got.Runs != 1 {
		t.Errorf("completed one-shot retired its replacement or lost completion: %+v", got)
	}
}

func TestControlWritebackStalePausePreservesCompletedOccurrence(t *testing.T) {
	store, made, now := controlWritebackItem(t)
	// The control surface opened before the operation completed. Saving its
	// pause must not replace the new execution history with that earlier view.
	stale := made
	runner := &controlWritebackRunner{}
	mustTick(t, newTicker(store, runner, now))
	completed, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetStatus(stale.ID, StatusPaused, ""); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPaused || got.Runs != completed.Runs || !got.LastFired.Equal(completed.LastFired) || got.LastOutcome != completed.LastOutcome || !got.NextDue.Equal(completed.NextDue) {
		t.Errorf("stale pause erased the completed occurrence: before %+v, after %+v", completed, got)
	}
}

func TestControlWritebackHarmlessEditDoesNotReplayOccurrence(t *testing.T) {
	for _, kind := range []WhenKind{WhenAt, WhenEvery} {
		for _, edit := range []string{"filing", "effort"} {
			t.Run(string(kind)+"/"+edit, func(t *testing.T) {
				store, made, now := controlWritebackItem(t)
				if kind == WhenAt {
					saveControlWriteback(t, store, made.ID, func(item *Item) {
						item.When = When{Kind: WhenAt, At: now.Add(-time.Minute)}
					})
				}
				runner := &controlWritebackRunner{onSay: func(Item) {
					if edit == "filing" {
						if _, err := store.FileExchange(made.ID, "/filed/exchange", "/filed/exchange/transcript"); err != nil {
							t.Fatal(err)
						}
					} else if err := store.SetStandingEffort(made.ID, effort.High); err != nil {
						t.Fatal(err)
					}
				}}
				ticker := newTicker(store, runner, now)
				mustTick(t, ticker)
				got, err := store.Get(made.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Runs != 1 {
					t.Errorf("completed occurrence not retained: %d runs", got.Runs)
				}
				if edit == "filing" && got.Origin.Exchange != "/filed/exchange" {
					t.Errorf("filing edit was lost: %+v", got.Origin)
				}
				if edit == "effort" && got.Does.Effort != effort.High.String() {
					t.Errorf("effort edit was lost: %q", got.Does.Effort)
				}
				if kind == WhenAt && got.Status != StatusRetired {
					t.Errorf("consumed one-shot remains %s", got.Status)
				}
				if kind == WhenEvery && !got.NextDue.After(now) {
					t.Errorf("consumed recurrence remains due at %s", got.NextDue)
				}
				ticker.Now = held(now.Add(time.Minute))
				store.clock = ticker.Now
				mustTick(t, ticker)
				if runner.calls != 1 {
					t.Errorf("harmless %s edit replayed the occurrence: %d actions", edit, runner.calls)
				}
			})
		}
	}
}
