package remote

// wire_automations.go is the version-21 delta: the far machine's automations,
// read and changed over the connection, and the one field on the hello that
// makes an attached window a window open over there ([Hello.Window]).
//
// automations.go is what answers these doors and what asks them; what is here is
// only what a frame has to say to name them.

import "github.com/Agent-Field/codeaf/internal/automation"

// ── AUTOMATIONS FOLLOW THE MACHINE THAT RUNS THEM ───────────────────────────
//
// An automation is a row in one machine's store, run by that machine's clock
// while a window is open on it (docs/design/automations/DESIGN.md). A window
// over --host or --at sits in front of a conversation on the FAR machine, and
// that conversation's `automation` tool proposes into the far store — so the
// list, the run lines and the notifications such a window draws are the far
// machine's, and a window that read this laptop's store instead would draw
// automations that run somewhere else, beside conversations it cannot see. These
// doors are how it reads the right machine, answered from the store the engine
// door hands over ([EngineAutomations]).
//
// ONE DOOR PER FUNCTION OF THE SURFACE'S SEAM (internal/tui3's AutomationsSeam),
// and each answers exactly what the store's own method answers, carried as
// internal/automation's own types — the bargain this protocol's header states,
// so a field added to an automation or a run travels without a wire change. The
// surface already calls every one of them off its loop, because the seam says
// any of them may be a round trip to another machine.
//
// [Welcome.Automations] says the engine answers them. An engine whose store
// could not be opened answers each one with a refusal and says nothing at the
// door, and the window then has no automations over that connection: ABSENT,
// and never this laptop's store standing in for the far one.
//
// ── AND THE WINDOW IS A WINDOW OVER THERE ───────────────────────────────────
//
// The far clock runs only while a window is open on its machine, and a window
// over a connection is a process on another machine that no presence file on
// the far disk could ever name. So the hello says so ([Hello.Window]) and the
// engine holds presence for that connection for exactly as long as it is
// attached — released when it closes, however it closes — which is the whole of
// "the far machine's clock runs while a window is attached there".
const (
	// The readings. Every one is a getter (callclass.go): the surface's watcher
	// asks Changes, List, Active and Windows on its beat, and Runs and Cursor on
	// the way in, so none of them may queue behind a turn.

	// MethodAutomationsList is every automation on the engine's machine, the
	// ones waiting on the person first ([automation.Store.List]).
	MethodAutomationsList = "Automations.List" // nothing → []automation.Automation
	// MethodAutomationsRuns is one automation's history, newest first.
	MethodAutomationsRuns = "Automations.Runs" // AutomationRunsArgs → []automation.Run
	// MethodAutomationsChanges is every run that changed after the cursor a
	// window holds, and the cursor to ask from next time — the one reading that
	// carries a run's news to a window on another machine (the design's
	// Delivery section names it).
	MethodAutomationsChanges = "Automations.Changes" // AutomationChangesArgs → AutomationChanges
	// MethodAutomationsCursor is the store's change counter now: where a window
	// that wants only what happens from here on starts.
	MethodAutomationsCursor = "Automations.Cursor" // nothing → int64
	// MethodAutomationsActive is every run queued or in hand.
	MethodAutomationsActive = "Automations.Active" // nothing → []automation.Run
	// MethodAutomationsWindows is how many codeaf windows are open on the
	// engine's machine — its own and every attached one, this window included
	// when its hello said [Hello.Window].
	MethodAutomationsWindows = "Automations.Windows" // nothing → int

	// The person's changes, and the one claim. Every one is a small act: a
	// keystroke on the list, owed no order behind a turn and waiting nothing
	// longer than a getter does (callclass.go says why an act gets no longer).

	// MethodAutomationsCreate saves a new automation the person agreed to, and
	// MethodAutomationsUpdate their change to one. Both answer the automation as
	// the store saved it, its next wake worked out on the engine's own clock.
	MethodAutomationsCreate = "Automations.Create" // AutomationArgs → automation.Automation
	MethodAutomationsUpdate = "Automations.Update" // AutomationArgs → automation.Automation
	// MethodAutomationsSetStatus pauses or resumes one.
	MethodAutomationsSetStatus = "Automations.SetStatus" // AutomationStatusArgs → automation.Automation
	// MethodAutomationsDelete removes one and its history.
	MethodAutomationsDelete = "Automations.Delete" // AutomationIDArgs → nothing
	// MethodAutomationsRunNow asks for a run outside the schedule. The run
	// itself arrives the way every run does, through Changes.
	MethodAutomationsRunNow = "Automations.RunNow" // AutomationIDArgs → nothing
	// MethodAutomationsStopRun asks the engine machine's clock to stop one run
	// in hand; the clock sees it within seconds and records it as stopped.
	MethodAutomationsStopRun = "Automations.StopRun" // AutomationRunArgs → nothing
	// MethodAutomationsClaim answers whether THIS window is the first to ask
	// for a run, which decides the one window that raises its desktop
	// notification. It is asked of the store every window reading that machine
	// shares, so two windows on two machines watching one store raise one
	// notification between them, not two.
	MethodAutomationsClaim = "Automations.Claim" // AutomationRunArgs → bool
)

// AutomationIDArgs names one automation by the id its store gave it.
//
// IT IS A STRUCT AND NOT A BARE STRING for [PlacesTaskArgs]' reason: a door
// that learns to carry a second fact about the same automation can do so
// without a second door and without a second version.
type AutomationIDArgs struct {
	ID string `json:"id"`
}

// AutomationRunsArgs asks for one automation's history, newest first, at most
// Limit long. Zero Limit is the store's own default, and no answer carries more
// than [automationRunsMost] whatever was asked.
type AutomationRunsArgs struct {
	ID    string `json:"id"`
	Limit int    `json:"limit,omitempty"`
}

// AutomationChangesArgs is the cursor a window holds on the store's change
// counter. Zero is a window that has read nothing, which is answered with every
// run the store has ever stamped.
type AutomationChangesArgs struct {
	After int64 `json:"after,omitempty"`
}

// AutomationChanges is what changed after the cursor, oldest change first, and
// the cursor to ask from next time.
//
// THE CURSOR IS THE STORE'S AND NEVER WORKED OUT HERE. The far store reads its
// counter first and the runs up to it ([automation.Store.Changes] says why that
// order is the one that loses nothing), so a window that kept anything else —
// the highest run it saw, the time it asked — would one day step past news it
// was never handed.
type AutomationChanges struct {
	Runs   []automation.Run `json:"runs,omitempty"`
	Cursor int64            `json:"cursor"`
}

// AutomationArgs is a whole automation, as a person agreed to it or changed it.
// It travels whole for [MemoryUpdateArgs]' reason: the store's Create and
// Update read the person's half entire, and a wire that carried only what
// changed would have to know which fields a person touched.
type AutomationArgs struct {
	Automation automation.Automation `json:"automation"`
}

// AutomationStatusArgs is a pause or a resume. Any other status is the store's
// to refuse, in its own words.
type AutomationStatusArgs struct {
	ID     string            `json:"id"`
	Status automation.Status `json:"status"`
}

// AutomationRunArgs names one run by the store's own number for it.
type AutomationRunArgs struct {
	Run int64 `json:"run"`
}

// EngineAutomations is THIS MACHINE'S automations, as the door that built the
// engine hands them to the wire: the store the clock runs from and this
// machine's conversations propose into, the zone a typed rhythm is read in, and
// the door that registers an attached window as open here.
//
// IT IS A HOOK AND NOT AN IMPORT, because the store is opened once per process
// and the clock is started by cmd/codeaf, which this package must never import.
// So the door supplies both halves and this package only asks.
//
// NIL, OR A NIL STORE, IS AUTOMATIONS OFF ON THIS ENGINE — the reading
// [Engine.World] and [Engine.Memory] already ask for. The welcome says nothing
// ([Welcome.Automations]), every door answers the same refusal, and a hello
// that said [Hello.Window] holds nothing, because there is no clock here for a
// window to keep running.
type EngineAutomations struct {
	// Store is this machine's automations database. Required.
	Store *automation.Store
	// Zone is the zone a rhythm typed in an attached window is read in, carried
	// on the welcome ([Welcome.AutomationsZone]). It is this machine's, because
	// this machine's clock reads the rhythm.
	Zone string
	// HoldWindow registers one window as open on this machine until release is
	// called, and keeps a clock running while it is. label says what the window
	// is, for a person reading the presence folder. Nil is a door that cannot
	// hold presence: the readings still answer, and nothing keeps the clock
	// running on an attached window's behalf.
	HoldWindow func(label string) (release func())
}

// on reports whether this engine answers the automations doors at all. It is
// nil-safe, because nil is the ordinary state of an engine with no store.
func (e *EngineAutomations) on() bool { return e != nil && e.Store != nil }

// zone is the zone the welcome carries, and empty when automations are off.
func (e *EngineAutomations) zone() string {
	if !e.on() {
		return ""
	}
	return e.Zone
}
