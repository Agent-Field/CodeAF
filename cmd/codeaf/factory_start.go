package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── `factory_start`: THE MANAGER STARTS ITS RUN WHEN THE PERSON SAYS SO ──────
//
// The manager's brief says it starts the run when asked, and until this door
// it had no way to: on the owner's run of 2026-10-09 the person said `start`
// and the manager spent ten tool calls reading the manual and the source
// before telling them to press ▶ run. The door is the floor's own control,
// the one the top bar's button sends, reached from the manager's chat:
//
//   - an item that never ran (or was dismissed) is launched ([factory.Seam.Launch]);
//     the manager is in the chat where the person asked, so the launch never
//     gives it a second turn to shape: an item it had not shaped is recorded
//     as kept ([shapeKeptRecord]) before it goes;
//   - a paused run goes on with the same step in the same chat ([factory.Seam.Hold]);
//   - a run held at an approve step goes past it ([factory.Seam.Answer] with
//     yes), which the tool's description reserves for the person's own clear
//     word in the chat;
//   - anything else (a running item, a queued one, a question that is not an
//     approve step, a run that is over) changes nothing and says why.
//
// THE RUNNER'S DOORS ARE READ AT THE MOMENT OF ASKING ([factoryRunnerDoors]),
// because the conversation's config is built before this process takes the
// floor's run lock, and a window that does not hold it asks the process that
// does through the mailbox.

// The lines `factory_start` answers, said once so the manual and the tests
// quote the same words.
const (
	sayStartLaunched  = "%s started"
	sayStartResumed   = "%s resumed"
	sayStartContinued = "%s went past %s"
	sayStartRunning   = "%s is already running"
	sayStartQueued    = "%s is queued · it starts when a bench frees"
)

// startItem starts, resumes or continues the run of item it on st.
func startItem(st *store.Store, workspace string, it factory.Item) (string, error) {
	dirs := factoryRepoDirs(st, workspace)
	seam := factoryNeedsCheckout(factory.LocalSeam(st, time.Now(), factoryRunnerDoors(st), factory.WithRepoDirs(dirs)), st, dirs)
	ref := it.Ref()
	switch it.State {
	case factory.StateLanded:
		return "", fmt.Errorf("%s has landed · its proof waits for the person's approval", ref)
	case factory.StateShipped:
		return "", fmt.Errorf("%s has shipped · its run is over", ref)
	case factory.StateNeedsYou:
		if it.QKind != factory.QKindApprove {
			return "", fmt.Errorf("%s waits on a question, not an approve step · answer it, or ask the person", ref)
		}
		if seam.Answer == nil {
			return "", errors.New("this window cannot reach the floor's runner")
		}
		if err := seam.Answer(it.ID, true, ""); err != nil {
			return "", err
		}
		return fmt.Sprintf(sayStartContinued, ref, heldStep(it)), nil
	case factory.StateRunning, factory.StateQueued:
		if it.Stream != nil && it.Stream.Paused {
			hold := seam.Hold
			if hold == nil && seam.Pause != nil {
				hold = func(id int, _ bool) error { return seam.Pause(id) }
			}
			if hold == nil {
				return "", errors.New("this window cannot reach the floor's runner")
			}
			if err := hold(it.ID, false); err != nil {
				return "", err
			}
			return fmt.Sprintf(sayStartResumed, ref), nil
		}
		if it.State == factory.StateQueued {
			return fmt.Sprintf(sayStartQueued, ref), nil
		}
		return fmt.Sprintf(sayStartRunning, ref), nil
	}
	if seam.Launch == nil {
		return "", errors.New("this window cannot reach the floor's runner")
	}
	// THE MANAGER IS HERE, IN THE CHAT WHERE THE PERSON SAID START: the
	// launch gives it no second turn to shape what it has just talked over.
	if !factoryrun.ShapedByManager(it) {
		if err := st.Update(it.ID, func(x *factory.Item) error {
			if !factoryrun.ShapedByManager(*x) {
				x.Adapted = append(append([]string(nil), x.Adapted...), shapeKeptRecord)
			}
			return nil
		}); err != nil {
			return "", err
		}
	}
	if err := seam.Launch(it.ID); err != nil {
		return "", err
	}
	return fmt.Sprintf(sayStartLaunched, ref), nil
}

// heldStep is the name of the approve step item it is held at, "the approve
// step" when the stream does not say.
func heldStep(it factory.Item) string {
	if it.Stream != nil {
		for _, ph := range it.Stream.Phases {
			if ph.State == factory.PhaseWaiting && strings.TrimSpace(ph.Name) != "" {
				return ph.Name
			}
		}
	}
	return "the approve step"
}

// StartRun is `factory_start` for the conversation that asks: the item it
// manages, started, resumed or continued.
func (d seamRunDoor) StartRun(_ context.Context, conversation string) (string, error) {
	it, err := d.managed(conversation)
	if err != nil {
		return "", err
	}
	return startItem(d.st, d.workspace, it)
}

// Manages says whether conversation is the manager of an item on the floor.
func (d seamRunDoor) Manages(conversation string) bool {
	_, err := d.managed(conversation)
	return err == nil
}
