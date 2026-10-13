package standing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AppOrder keeps the app's approval rule separate from the terminal's clean-run
// streak. Approval never widens the tools or permissions a firing may use.
type AppOrder struct {
	Scope        string   `json:"scope"`
	CrewID       string   `json:"crew_id,omitempty"`
	Wont         string   `json:"wont,omitempty"`
	ApprovedRuns int      `json:"approved_runs"`
	Skipped      int      `json:"skipped"`
	RunSeq       uint64   `json:"run_seq"`
	Runs         []AppRun `json:"runs,omitempty"`
}

// AppRun is a durable trigger and its decision. Evidence remains with the run
// until it is consumed, so approval cannot accidentally execute a later trigger.
type AppRun struct {
	Report       string    `json:"report,omitempty"`
	AskDelivered bool      `json:"ask_delivered,omitempty"`
	ID           string    `json:"id"`
	At           time.Time `json:"at"`
	State        string    `json:"state"`
	Line         string    `json:"line"`
	Evidence     string    `json:"evidence,omitempty"`
	Positive     string    `json:"positive,omitempty"`
	Approved     bool      `json:"approved,omitempty"`
}

const (
	StatusProposed Status = "proposed"
	StatusDeclined Status = "declined"
)

var ErrAppConflict = errors.New("this standing order changed; read it again")

// AppInboxDir is the scope-independent engine address the app Inbox reads.
func AppInboxDir(root, id string) string { return filepath.Join(root, id, "app-inbox") }

func (a AppOrder) validate() error {
	switch a.Scope {
	case "this_chat", "everywhere", "not_here":
	case "this_crew":
		if a.CrewID == "" {
			return errors.New("This crew needs an agent in a crew")
		}
	default:
		return errors.New("unknown standing order scope")
	}
	if a.ApprovedRuns < 0 || a.ApprovedRuns > TrustAfter || a.Skipped < 0 {
		return errors.New("invalid standing order counters")
	}
	return nil
}

// AppTrust is the fixed five-approval track, independent of unattended outcomes.
func (it Item) AppTrust() string {
	if it.App == nil || it.App.ApprovedRuns == 0 {
		return "asks_first"
	}
	if it.App.ApprovedRuns < TrustAfter {
		return "earning"
	}
	return "trusted"
}

// AppWaitingRun is the single decision that must be consumed before a fresh
// look. Completed decisions remain on disk so late clicks cannot execute twice.
func (it Item) AppWaitingRun() *AppRun {
	if it.App == nil {
		return nil
	}
	for i := len(it.App.Runs) - 1; i >= 0; i-- {
		if it.App.Runs[i].State == "waiting" || it.App.Runs[i].State == "authorized" {
			return &it.App.Runs[i]
		}
	}
	return nil
}

// Propose persists a dormant card. Create remains the ratified-only door for
// terminal callers; a proposal is never eligible for a ticker pass.
func (s *Store) Propose(item Item) (Item, error) {
	if item.App == nil {
		return Item{}, errors.New("an app proposal needs its scope")
	}
	if err := item.Validate(); err != nil {
		return Item{}, err
	}
	if item.ID == "" {
		item.ID = newID()
	}
	if err := checkID(item.ID); err != nil {
		return Item{}, err
	}
	item.Status, item.Created, item.Updated, item.Revision = StatusProposed, s.now(), s.now(), 1
	item.Schema = SchemaOf(item)
	err := s.underItemLock(item.ID, func() error {
		if _, err := os.Stat(s.ItemPath(item.ID)); err == nil {
			return ErrAppConflict
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		data, err := marshalItem(item)
		if err != nil {
			return err
		}
		return writeAtomic(s.ItemPath(item.ID), data)
	})
	return item, err
}

// ChangeApp applies a person's mutation to the current disk document under the
// same flock the ticker takes. A stale expect cannot overwrite a newer firing.
func (s *Store) ChangeApp(id string, change func(*Item) error) (Item, error) {
	var item Item
	if err := checkID(id); err != nil {
		return item, ErrNotFound
	}
	err := s.underItemLock(id, func() error {
		var err error
		item, err = s.read(s.ItemPath(id))
		if err != nil {
			return err
		}
		if err := change(&item); err != nil {
			return err
		}
		if err := item.Validate(); err != nil {
			return err
		}
		item.Updated, item.Revision = s.now(), item.Revision+1
		data, err := marshalItem(item)
		if err != nil {
			return err
		}
		return writeAtomic(s.ItemPath(id), data)
	})
	return item, err
}

// RatifyApp arms the exact card currently on disk. A changed schedule starts
// from ratification time rather than the time its proposal was first shown.
func (s *Store) RatifyApp(id string) (Item, error) {
	return s.ChangeApp(id, func(it *Item) error {
		if it.Status != StatusProposed {
			return ErrAppConflict
		}
		due, err := firstDue(*it, s.now())
		if err != nil {
			return err
		}
		it.Status, it.NextDue = StatusActive, due
		return nil
	})
}

// DecideAppRun consumes one named trigger. Skip changes neither trust nor the
// approval count; Not right resets the track even for an already finished run.
func DecideAppRun(it *Item, id, verb string) error {
	if it.App == nil {
		return errors.New("this order has no app run track")
	}
	var run *AppRun
	for i := range it.App.Runs {
		if it.App.Runs[i].ID == id {
			run = &it.App.Runs[i]
			break
		}
	}
	if run == nil {
		return ErrNotFound
	}
	if verb == "not_right" {
		if run.State == "not_right" {
			return ErrAppConflict
		}
		it.App.ApprovedRuns = 0
		pending := run.State == "waiting" || run.State == "authorized"
		if run.State == "waiting" || run.State == "authorized" {
			run.Evidence = ""
		}
		run.State = "not_right"
		if pending {
			it.NeedsPerson = ""
		}
		return nil
	}
	if it.Status != StatusActive || run.State != "waiting" {
		return ErrAppConflict
	}
	switch verb {
	case "go", "run_now":
		run.State, run.Approved = "authorized", true
		it.App.ApprovedRuns = min(TrustAfter, it.App.ApprovedRuns+1)
	case "skip":
		run.State, run.Evidence = "skipped", ""
	case "pause":
		it.Status = StatusPaused
		run.State, run.Evidence = "skipped_paused", ""
		it.App.Skipped++
	default:
		return errors.New("unknown standing run verb")
	}
	it.NeedsPerson = ""
	return nil
}

func (it *Item) appRun(now time.Time, found sighting, state string) *AppRun {
	it.App.RunSeq++
	id := fmt.Sprintf("run_%d", it.App.RunSeq)
	it.App.Runs = append(it.App.Runs, AppRun{ID: id, At: now, State: state, Line: found.line, Evidence: found.evidence, Positive: found.positive})
	return &it.App.Runs[len(it.App.Runs)-1]
}

// appGate records the trigger before asking. Approved evidence bypasses a new
// probe, while trusted runs still pass through the ordinary permission rails.
func (t *Ticker) appGate(ctx context.Context, item *Item, now time.Time, found sighting) (bool, error) {
	if item.App == nil || (item.When.Kind == WhenAt && item.Does.Kind == ActionSay) {
		return false, nil
	}
	if run := item.AppWaitingRun(); run != nil && run.State == "authorized" {
		return false, nil
	}
	state := "waiting"
	if item.AppTrust() == "trusted" {
		state = "authorized"
	}
	run := item.appRun(now, found, state)
	item.Positive = found.positive
	if state == "waiting" {
		item.NeedsPerson = fmt.Sprintf("%s · %s · Go ahead · Skip this one · Pause order · Up to $%.2f.", found.line, run.ID, item.Rails.PerRunUSD)
	}
	if err := t.Store.saveActive(item); err != nil {
		return true, err
	}
	if state == "waiting" {
		if err := t.Store.Log(item.ID, item.NeedsPerson); err != nil {
			return true, err
		}
		return true, t.deliverAppAsk(ctx, item)
	}
	return false, nil
}

// appPaused observes triggers without running their action and consumes the
// schedule or event identity once. Skipped lines are retained in the order log.
func (t *Ticker) appPaused(ctxNow time.Time, item *Item, found sighting) error {
	_, err := t.Store.ChangeApp(item.ID, func(current *Item) error {
		if current.Status != StatusPaused || current.Revision != item.Revision {
			return ErrAppConflict
		}
		current.NextDue, current.Fingerprint, current.Positive = item.NextDue, item.Fingerprint, item.Positive
		current.SpentUSD = max(current.SpentUSD, item.SpentUSD)
		current.LastChecked, current.LastCheckLine = ctxNow, found.line
		if found.state == stateReady {
			current.App.Skipped++
			current.Positive = found.positive
			run := current.appRun(ctxNow, found, "skipped_paused")
			run.Line = "Skipped while paused · " + found.line + " · " + ctxNow.Format("15:04")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if found.state == stateReady {
		return t.Store.Log(item.ID, "Skipped while paused · "+found.line+" · "+ctxNow.Format("15:04"))
	}
	return nil
}

// Asking is a durable notification, never a new model turn. Retry uses the
// same note identity until the recipient addresses have all acknowledged it.
func (t *Ticker) deliverAppAsk(ctx context.Context, item *Item) error {
	run := item.AppWaitingRun()
	if run == nil || run.State != "waiting" || run.AskDelivered {
		return nil
	}
	deliverer, ok := t.Runner.(Deliverer)
	if !ok {
		return nil
	}
	_, err := deliverer.Deliver(ctx, *item, Pending{ID: item.ID + "_" + run.ID + "_ask", Kind: "order_ask", Text: item.NeedsPerson, At: run.At})
	if err != nil {
		return err
	}
	run.AskDelivered = true
	return t.Store.saveActive(item)
}

// Paused app orders still account for opportunities, while ordinary paused
// terminal orders preserve their existing silent behavior.
func (t *Ticker) observeAppPaused(ctx context.Context, pass *Pass, item *Item) {
	if item.Status != StatusPaused || item.App == nil || item.When.Kind == WhenHold {
		return
	}
	if item.When.Kind == WhenProbe {
		held, err := t.railsHold(ctx, item, t.clock(), pass)
		if held || err != nil {
			return
		}
	}
	before := item.SpentUSD
	found, err := t.look(ctx, item, t.clock(), pass.At)
	if found.state == stateUndecided && item.SpentUSD > before {
		_, spendErr := t.Store.ChangeApp(item.ID, func(current *Item) error { current.SpentUSD = max(current.SpentUSD, item.SpentUSD); return nil })
		err = errors.Join(err, spendErr)
	}
	if err == nil && found.state != stateAsleep && found.state != stateUndecided {
		err = t.appPaused(t.clock(), item, found)
	}
	if err != nil && !errors.Is(err, ErrAppConflict) {
		pass.Errors++
		pass.Notes = append(pass.Notes, oneLine(err.Error()))
	}
}

func appNeedsPerson(item *Item) bool {
	return item.App != nil && item.NeedsPerson != "" && !IsBaselineLine(item.NeedsPerson) && item.AppWaitingRun() == nil
}
