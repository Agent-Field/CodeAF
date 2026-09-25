package remote

import "time"

// These are the task-command questions whose answers belong to the engine
// machine. The surface sends intent; sizing, shaping, admission and spending
// remain with the session agent that owns the conversation.
const (
	MethodTaskStart = "Task.Start"
	// MethodTaskRedoStronger runs the newest task again with every seat nobody
	// pinned one step stronger ([session.Agent.RedoStronger]).
	MethodTaskRedoStronger = "Task.RedoStronger"
	MethodPlannerStart     = "Task.StartPlanner"
	MethodTaskRoom         = "Task.Room"
	MethodTaskSteer        = "Task.Steer"
	MethodTaskStop         = "Task.Stop"
	MethodTaskRetry        = "Task.Retry"
	MethodTaskModel        = "Task.Model"
	MethodTaskEffort       = "Task.Effort"
	MethodTaskSetEffort    = "Task.SetEffort"
	// MethodTaskWatch is the surface saying it draws tasks, and it is the only
	// one of these that asks for nothing back: what it buys is the engine
	// pushing "task" frames from then on (tasklane.go). It is sent once per
	// conversation the surface takes up, never on a frame.
	MethodTaskWatch = "Task.Watch"
	// MethodTaskResolve answers one task proposal. It is the fourth thing a
	// hosted rail needs and the one nothing carried: the card drew, the keys
	// worked, and `y` went nowhere — so the whole task seam failed to assert on
	// a hosted surface and the rail was never even subscribed.
	MethodTaskResolve = "Task.Resolve"
	// MethodTaskHold removes one proposal's admission clock while keeping the
	// question open. It travels separately from Resolve because typing is not an
	// answer and a deleted draft must leave the hold in force.
	MethodTaskHold = "Task.Hold"
	// MethodTaskSettle answers a landing that came home as the person's call:
	// accept it, say it is not finished, spend one more merge round on a branch
	// that would not fasten, hand the question to the model, or take it back.
	//
	// FIVE ACTS AND ONE METHOD, because they are one question being answered and
	// the surface draws them as one row (internal/tui3's tasksettle.go). Which act
	// is asked for is a field of [TaskSettleArgs] rather than five method names,
	// so an engine that speaks this method speaks all of it — a half-answered card
	// is the shape this whole door exists to end.
	MethodTaskSettle = "Task.Settle"
	// MethodTaskPending is the proposals the far engine is still waiting on. A
	// surface asks it where the local one reads [session.Agent.PendingTasks] —
	// when a turn ends with a proposal card still on screen — because a card
	// about a question nobody is asking any more has to stop asking it.
	MethodTaskPending = "Task.Pending"
)

// TaskRoomArgs names a node in the engine's current conversation. Unlike a
// record URI, the id exists before the node has written its first journal line.
type TaskRoomArgs struct {
	ID   uint64 `json:"id"`
	Tail int    `json:"tail,omitempty"`
}

// TaskSteerArgs carries one person's correction to a running node.
//
// ── AND THE NAME OF THE SEND, SO A LOST ANSWER IS NOT A LOST SENTENCE ──
//
// The id and the text alone cannot survive the one failure that matters over a
// wire: the engine TAKES the words and the answer never gets back — the link
// died, the deadline ran out. The surface then holds a sentence it can neither
// report as delivered nor send again, because a second send with nothing to
// recognise it by is a second correction on the worker's queue.
//
// So the surface's own name for the send crosses with it ([session.SteerSource]
// — a scope naming one life of one window, and a number counting its sends),
// and an engine that keeps them answers a repeat with the receipt already on
// the record, delivering nothing (Again below). It is the SEND's identity and
// never a hash of the words: two intentional sends of one sentence carry two
// Seqs and are two directions.
//
// BOTH ENDS DEGRADE HONESTLY. An engine that predates this ignores the two
// fields and steers exactly as it always did — which is why the surface asks
// whether the far machine keeps them ([Welcome.SteerRepeat]) BEFORE it sends,
// rather than discovering it by having asked twice.
type TaskSteerArgs struct {
	ID   uint64 `json:"id"`
	Text string `json:"text"`
	// Scope and Seq are the send's identity, absent on a surface that has no way
	// to mint one and on every client written before this.
	Scope string `json:"scope,omitempty"`
	Seq   uint64 `json:"seq,omitempty"`
	// Said is when the person pressed enter, which is what the node's record
	// orders its corrections by. It travels because a send may be repeated
	// minutes later and the instant that decides which correction is the later
	// one is the one they said it at, not the one the wire delivered it on.
	Said time.Time `json:"said,omitzero"`
	// Session is the conversation the surface believed it was addressing, and it
	// is checked at the engine against the one actually open.
	//
	// THE HANDLE DOES NOT CHANGE WHEN THE CONVERSATION DOES. `Session.Open` and
	// `Session.New` swap the engine's conversation behind the same client and the
	// same agent (cmd/codeaf's chatv3_host.go returns that agent unchanged), so a
	// send held over a swap — queued behind another, or retried after one — would
	// otherwise reach whatever task 7 means in the conversation that replaced it.
	// Empty is a surface making no claim.
	Session string `json:"session,omitempty"`
}

// TaskSteered carries the local door's whole receipt (internal/session's
// [session.SteerReceipt]) rather than only its waiting fact.
//
// HELD IS WHY IT GREW. A line said while the engine is checking a task's work is
// TAKEN — it goes on the task's record and the landing may not publish over it —
// and that is a success with a different sentence, not an error. Carried as an
// error it would have arrived here as bare text, so a hosted room could not tell
// "kept, and it will be read" from "refused, say it somewhere else"; the person
// furthest from the work would have been the one told least about it.
//
// An engine that predates the receipt fills Waiting and nothing else, which is
// exactly what this type meant before: absent fields read as false, and a room
// then draws the delivery it always drew.
type TaskSteered struct {
	Waiting   bool   `json:"waiting,omitempty"`
	Held      bool   `json:"held,omitempty"`
	Direction uint64 `json:"direction,omitempty"`
	Landing   string `json:"landing,omitempty"`
	// Again says this node already held these words FROM THIS SAME SEND, so
	// nothing was delivered a second time and Direction is the receipt the first
	// one was written down as. It is the answer a surface asking again for a
	// crossing nobody answered is hoping for, and it is why asking again is safe
	// at all ([TaskSteerArgs]).
	Again bool `json:"again,omitempty"`
	// Elsewhere says the conversation the surface named ([TaskSteerArgs.Session])
	// is not the one open here, so NOTHING WAS DELIVERED. It is a field rather
	// than an error string because the surface has to recognise it exactly: the
	// send stays unresolved and is never re-aimed at the task with that number in
	// the conversation that replaced it (internal/session's
	// [session.ErrNotThatConversation]).
	Elsewhere bool `json:"elsewhere,omitempty"`
	// Uncertain says the engine could not tell whether this correction was kept:
	// it reached the record and could not be taken back off the disk again
	// (internal/session's [session.ErrSendUnanswered] wearing an engine's reason
	// rather than a dead link's). It is a field for [Elsewhere]'s reason — the
	// surface has to recognise it exactly, keep the send under the name it has,
	// and offer to ask again rather than hand the words back to be renamed.
	Uncertain bool `json:"uncertain,omitempty"`
}

// TaskStopArgs uses the session's already-prefixed work id unchanged.
type TaskStopArgs struct {
	ID string `json:"id"`
}

// TaskStopped carries the engine's person-facing sentence without rewriting it.
type TaskStopped struct {
	Line string `json:"line,omitempty"`
}

// TaskStartArgs carries the person's brief without interpreting it locally, and
// whether they said the work is one worker's ([session.Agent.StartTask]'s solo):
// that is the one thing the surface knows and the engine cannot, because the
// word and the standing answer are both read on the surface's side.
//
// Effort is the one-task effort word the person said (`best`, `cheap`), read on
// the surface's side like solo, and empty on every ordinary start.
type TaskStartArgs struct {
	Brief  string `json:"brief"`
	Solo   bool   `json:"solo,omitempty"`
	Effort string `json:"effort,omitempty"`
}

// TaskRedoArgs names the task to run again stronger; 0 is the newest one.
type TaskRedoArgs struct {
	ID uint64 `json:"id,omitempty"`
}

// PlannerStartArgs also carries the sizing hint used by the adaptive form.
type PlannerStartArgs struct {
	Brief string `json:"brief"`
	Hint  string `json:"hint,omitempty"`
}

// TaskStarted is the receipt the existing single-task note draws. Note is the
// engine's one line about where the work stands when the ground ladder moved it
// (internal/session's taskstands.go), and empty on every ordinary start.
type TaskStarted struct {
	ID    uint64 `json:"id,omitempty"`
	Title string `json:"title,omitempty"`
	Note  string `json:"note,omitempty"`
}

// PlannerStarted is the receipt the existing adaptive-task note draws.
type PlannerStarted struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title,omitempty"`
}

// TaskPending is the open proposals, oldest id first — [session.Agent.PendingTasks]
// as it crosses. An empty list is the honest answer that nothing is waiting, and
// it is why the field is not omitempty: "no proposals" and "this engine did not
// say" have to stay two different readings on the way back.
type TaskPending struct {
	IDs []uint64 `json:"ids"`
}

// TaskResolveArgs is one person's answer to one proposal, in the engine's own
// three fields ([session.TaskAnswer]): whether it may run, the correction to
// append if they redirected it, and the model they picked where the card
// offered a choice.
type TaskResolveArgs struct {
	ID       uint64 `json:"id"`
	Approved bool   `json:"approved,omitempty"`
	Redirect string `json:"redirect,omitempty"`
	Model    string `json:"model,omitempty"`
}

// TaskHoldArgs names the proposal whose first typed rune stopped its clock.
type TaskHoldArgs struct {
	ID uint64 `json:"id"`
}

// TaskSettleArgs is one person's answer to one landing.
//
// THE THREE FLAGS ARE NOT RESOLUTIONS AND ARE KEPT APART FROM ONE. `accept` and
// `not right` are the engine's own words ([session.TaskResolution]) and travel in
// Resolution; the merge round, the hand-over and the take-back RESOLVE NOTHING —
// each moves the question rather than answering it — so folding them into the
// same field would put three acts that settle no task into the vocabulary of the
// two that do.
//
// An engine reads at most one of them: the flags are tested before Resolution,
// in the order below, and an argument with none of them set is a resolution.
type TaskSettleArgs struct {
	ID uint64 `json:"id"`
	// Merge spends one more merge round on a branch that clashed. It is the
	// conflict card's yes, and it takes nothing as done.
	Merge bool `json:"merge,omitempty"`
	// Hand gives this one decision to the model. Back takes it away again.
	Hand bool `json:"hand,omitempty"`
	Back bool `json:"back,omitempty"`
	// Resolution is `accept`, `reaudit` or `refute` — [session.TaskResolution] as
	// it crosses — and Why is the sentence a caller may put on the record with it.
	Resolution string `json:"resolution,omitempty"`
	Why        string `json:"why,omitempty"`
}

// TaskSettled is the answer, and its ONE FIELD IS THE ONE THING A SENTENCE
// CANNOT CARRY: whether the question was already gone.
//
// A landing can be settled from four places at once — this card, another window,
// the model's own `tasks … resolve`, a check that finally answered — and
// whichever answer arrives second finds nothing to spend. That is a card
// catching up rather than a fault, and the surface draws it as one
// ([session.ErrTaskDecided]). Every other refusal stays the engine's own
// sentence and arrives as the call's error.
type TaskSettled struct {
	Decided bool `json:"decided,omitempty"`
}

// TaskSetupArgs binds a setup change to the conversation whose task was drawn.
type TaskSetupArgs struct {
	ID      uint64 `json:"id"`
	Session string `json:"session"`
	Value   string `json:"value,omitempty"`
}
