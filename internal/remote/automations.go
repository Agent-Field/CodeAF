package remote

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/automation"
)

// ── THE ENGINE HALF OF THE AUTOMATIONS DOORS, AND THE SURFACE HALF ──────────
//
// wire_automations.go says what the thirteen doors are and why they follow the
// machine that runs the automations. This is what answers them, from the store
// the engine door handed over ([EngineAutomations]), what asks them, and the one
// thing a window's hello buys here: presence on this machine for as long as the
// connection is attached. It is a file of its own for teams.go's reason —
// server.go and client.go are where every lane meets.

// errNoAutomations is every automations door on an engine that keeps none. It is
// a refusal and never an empty answer: an empty list over the wire would be a
// claim that the far machine has no automations, when the truth is that this
// engine cannot say — and the welcome already told the window not to ask
// ([Welcome.Automations]).
var errNoAutomations = errors.New("engine: this engine keeps no automations")

// automationRunsMost is the most one Runs answer carries, whatever was asked.
// A run's detail may be sixteen kilobytes (internal/automation's clock clips it
// there), so two hundred of them is already a few megabytes down an ssh pipe:
// the newest of a history a person scrolls, never the whole of a rhythm that
// has been running for a year. Zero and below are the store's own default.
const automationRunsMost = 200

// isAutomationsMethod reports whether a call is one of the thirteen.
func isAutomationsMethod(method string) bool {
	switch method {
	case MethodAutomationsList, MethodAutomationsRuns, MethodAutomationsChanges,
		MethodAutomationsCursor, MethodAutomationsActive, MethodAutomationsWindows,
		MethodAutomationsCreate, MethodAutomationsUpdate, MethodAutomationsSetStatus,
		MethodAutomationsDelete, MethodAutomationsRunNow, MethodAutomationsStopRun,
		MethodAutomationsClaim:
		return true
	}
	return false
}

// automationsCall answers the automations doors, and says whether the method
// was one of them at all. A false hands the call on to the refusal an engine
// from before these doors answers.
//
// THE STORE IS READ OFF THE ENGINE UNDER THE SESSION'S LOCK AND USED OUTSIDE
// IT, the way [server.teamsCall] reads its profile: every answer below is a
// database round trip, and none of that belongs under the lock every frame of
// this conversation takes.
func (s *server) automationsCall(call Frame) (json.RawMessage, bool, error) {
	if !isAutomationsMethod(call.Method) {
		return nil, false, nil
	}
	sess := s.session
	sess.mu.Lock()
	autos := sess.engine.Automations
	sess.mu.Unlock()
	if !autos.on() {
		return nil, true, errNoAutomations
	}
	store := autos.Store

	switch call.Method {
	case MethodAutomationsList:
		list, err := store.List()
		return automationsAnswer(list, err)

	case MethodAutomationsRuns:
		args, err := arg[AutomationRunsArgs](call)
		if err != nil {
			return nil, true, err
		}
		limit := args.Limit
		if limit > automationRunsMost {
			limit = automationRunsMost
		}
		runs, err := store.Runs(args.ID, limit)
		return automationsAnswer(runs, err)

	case MethodAutomationsChanges:
		args, err := arg[AutomationChangesArgs](call)
		if err != nil {
			return nil, true, err
		}
		runs, cursor, err := store.Changes(args.After)
		return automationsAnswer(AutomationChanges{Runs: runs, Cursor: cursor}, err)

	case MethodAutomationsCursor:
		cursor, err := store.Cursor()
		return automationsAnswer(cursor, err)

	case MethodAutomationsActive:
		active, err := store.Active()
		return automationsAnswer(active, err)

	case MethodAutomationsWindows:
		// THE SAME COUNT THIS MACHINE'S CLOCK TAKES, from the same folder: a
		// window that asked anything else would be told a number the clock
		// does not act on.
		open, err := automation.NewPresence(store.Root()).Count()
		return automationsAnswer(open, err)

	case MethodAutomationsCreate, MethodAutomationsUpdate:
		args, err := arg[AutomationArgs](call)
		if err != nil {
			return nil, true, err
		}
		if call.Method == MethodAutomationsCreate {
			saved, err := store.Create(args.Automation)
			return automationsAnswer(saved, err)
		}
		saved, err := store.Update(args.Automation)
		return automationsAnswer(saved, err)

	case MethodAutomationsSetStatus:
		args, err := arg[AutomationStatusArgs](call)
		if err != nil {
			return nil, true, err
		}
		saved, err := store.SetStatus(args.ID, args.Status)
		return automationsAnswer(saved, err)

	case MethodAutomationsDelete, MethodAutomationsRunNow:
		args, err := arg[AutomationIDArgs](call)
		if err != nil {
			return nil, true, err
		}
		if call.Method == MethodAutomationsDelete {
			return nil, true, store.Delete(args.ID)
		}
		// THE QUEUED RUN IS NOT ANSWERED. It reaches every window the way every
		// run does — through Changes — and a second road for this one would be
		// two copies of one run's news in the window that asked.
		_, err = store.QueueNow(args.ID)
		return nil, true, err

	default: // MethodAutomationsStopRun, MethodAutomationsClaim
		args, err := arg[AutomationRunArgs](call)
		if err != nil {
			return nil, true, err
		}
		if call.Method == MethodAutomationsStopRun {
			return nil, true, store.RequestStop(args.Run)
		}
		first, err := store.Claim(args.Run)
		return automationsAnswer(first, err)
	}
}

// automationsAnswer is one store answer as a result's payload, or the store's
// own refusal. The refusal travels in the store's words, for the reason
// [Client.SaveStanding] gives: a row redrawn as changed over a store that
// refused the change would be the screen lying about somebody else's disk.
func automationsAnswer[T any](value T, err error) (json.RawMessage, bool, error) {
	if err != nil {
		return nil, true, err
	}
	payload, err := json.Marshal(value)
	return payload, true, err
}

// ── presence for an attached window ─────────────────────────────────────────

// holdWindow registers this connection as a window open on this machine when
// its hello said it is one ([Hello.Window]), so this machine's automations clock
// runs for as long as the connection is attached.
//
// IT IS HELD BEFORE THE WELCOME IS SENT, which is the order that makes "this
// window included" true from the window's first reading: a surface holding a
// welcome may ask how many windows are open at once, and an answer taken before
// this one was counted would leave it out of the very count it asked about. A
// connection the engine then refuses lets go on its way out, with everything
// else it held ([server.letWindowGo] runs on every road).
//
// ONE HOLD PER CONNECTION, and a redial is a new connection. A link that drops
// lets its hold go when this engine sees the pipe end, and the redial takes a
// new one; the clock's own half-minute grace is what covers the gap between
// the two, the same grace that covers a window restarting onto a new build.
func (s *server) holdWindow(hello Hello) {
	if !hello.Window || s.session == nil {
		return
	}
	sess := s.session
	sess.mu.Lock()
	autos := sess.engine.Automations
	sess.mu.Unlock()
	if !autos.on() || autos.HoldWindow == nil {
		return
	}
	s.window = autos.HoldWindow(windowLabel(hello.Surface))
}

// letWindowGo releases what [server.holdWindow] took, once. It runs on every
// road out of a connection — a detach, a torn pipe, a close, a refusal, a fault —
// because a window that has gone and is still counted is a clock running the
// person's work with nobody in front of it.
func (s *server) letWindowGo() {
	release := s.window
	s.window = nil
	if release != nil {
		release()
	}
}

// windowLabel is what an attached window's presence file says about it, for a
// person reading the folder and for nothing else: liveness is the lock, never
// the label (internal/automation's presence.go). The machine's name is
// sanitized for [machineLabel]'s reason — it is text one machine sent.
func windowLabel(surface string) string {
	if name := machineLabel(surface); name != "" {
		return "attached from " + name
	}
	return "attached window"
}

// ── the surface half ────────────────────────────────────────────────────────

// askAutomations makes one automations call and reads its answer into out, which
// may be nil for a door that answers nothing.
//
// THE ONE REFUSAL A WINDOW MAY BRANCH ON COMES BACK AS THE STORE'S OWN SENTINEL.
// A refusal crosses this wire as its words ([Client.deliver]), which is right for
// every refusal a person reads; "no such automation" is also one a window may
// ACT on — the row it pressed was deleted from another window — and the seam on
// the machine the store is on answers it as [automation.ErrNotFound]. A window
// over a connection must be able to ask the same question the same way.
func (c *Client) askAutomations(method string, args any, out any) error {
	payload, err := c.call(nil, method, args)
	if err != nil {
		if strings.TrimSpace(err.Error()) == automation.ErrNotFound.Error() {
			return automation.ErrNotFound
		}
		return err
	}
	if out == nil || len(payload) == 0 {
		return nil
	}
	return json.Unmarshal(payload, out)
}

// AutomationsList is every automation on the engine's machine.
func (c *Client) AutomationsList() ([]automation.Automation, error) {
	var list []automation.Automation
	err := c.askAutomations(MethodAutomationsList, nil, &list)
	return list, err
}

// AutomationsRuns is one automation's history on the engine's machine, newest
// first, at most limit long.
func (c *Client) AutomationsRuns(id string, limit int) ([]automation.Run, error) {
	var runs []automation.Run
	err := c.askAutomations(MethodAutomationsRuns, AutomationRunsArgs{ID: id, Limit: limit}, &runs)
	return runs, err
}

// AutomationsChanges is every run that changed on the engine's machine after the
// cursor, and the cursor to ask from next time.
//
// A FAILED READING KEEPS THE CURSOR IT WAS GIVEN, which is the store's own
// answer to a failed read: the window asks again from the same place on its next
// beat, and a run that changed in between is still after it.
func (c *Client) AutomationsChanges(cursor int64) ([]automation.Run, int64, error) {
	var changes AutomationChanges
	if err := c.askAutomations(MethodAutomationsChanges, AutomationChangesArgs{After: cursor}, &changes); err != nil {
		return nil, cursor, err
	}
	return changes.Runs, changes.Cursor, nil
}

// AutomationsCursor is the engine machine's change counter now.
func (c *Client) AutomationsCursor() (int64, error) {
	var cursor int64
	err := c.askAutomations(MethodAutomationsCursor, nil, &cursor)
	return cursor, err
}

// AutomationsActive is every run queued or in hand on the engine's machine.
func (c *Client) AutomationsActive() ([]automation.Run, error) {
	var active []automation.Run
	err := c.askAutomations(MethodAutomationsActive, nil, &active)
	return active, err
}

// AutomationsWindows is how many codeaf windows are open on the engine's
// machine, counted the way its clock counts them.
func (c *Client) AutomationsWindows() (int, error) {
	var open int
	err := c.askAutomations(MethodAutomationsWindows, nil, &open)
	return open, err
}

// AutomationsCreate saves a new automation in the engine machine's store and
// answers it as saved there.
func (c *Client) AutomationsCreate(a automation.Automation) (automation.Automation, error) {
	var saved automation.Automation
	err := c.askAutomations(MethodAutomationsCreate, AutomationArgs{Automation: a}, &saved)
	return saved, err
}

// AutomationsUpdate saves the person's change to one automation on the engine's
// machine and answers it as saved there.
func (c *Client) AutomationsUpdate(a automation.Automation) (automation.Automation, error) {
	var saved automation.Automation
	err := c.askAutomations(MethodAutomationsUpdate, AutomationArgs{Automation: a}, &saved)
	return saved, err
}

// AutomationsSetStatus pauses or resumes one automation on the engine's machine.
func (c *Client) AutomationsSetStatus(id string, status automation.Status) (automation.Automation, error) {
	var saved automation.Automation
	err := c.askAutomations(MethodAutomationsSetStatus, AutomationStatusArgs{ID: id, Status: status}, &saved)
	return saved, err
}

// AutomationsDelete removes one automation and its history from the engine's
// machine.
func (c *Client) AutomationsDelete(id string) error {
	return c.askAutomations(MethodAutomationsDelete, AutomationIDArgs{ID: id}, nil)
}

// AutomationsRunNow asks the engine machine's clock for a run outside the
// schedule.
func (c *Client) AutomationsRunNow(id string) error {
	return c.askAutomations(MethodAutomationsRunNow, AutomationIDArgs{ID: id}, nil)
}

// AutomationsStopRun asks the engine machine's clock to stop one run in hand.
func (c *Client) AutomationsStopRun(run int64) error {
	return c.askAutomations(MethodAutomationsStopRun, AutomationRunArgs{Run: run}, nil)
}

// AutomationsClaim answers whether this window is the first to ask for a run,
// of every window reading the engine machine's store.
func (c *Client) AutomationsClaim(run int64) (bool, error) {
	var first bool
	err := c.askAutomations(MethodAutomationsClaim, AutomationRunArgs{Run: run}, &first)
	return first, err
}
