package remote

// These are the three task-command questions whose answers belong to the
// engine machine. The surface sends intent; shaping, admission and spending
// remain with the session agent that owns the conversation.
const (
	MethodTaskStart    = "Task.Start"
	MethodPlannerStart = "Task.StartPlanner"
	MethodTaskJudge    = "Task.Judge"
	MethodTaskRoom     = "Task.Room"
	MethodTaskSteer    = "Task.Steer"
	MethodTaskStop     = "Task.Stop"
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
type TaskSteerArgs struct {
	ID   uint64 `json:"id"`
	Text string `json:"text"`
}

// TaskSteered preserves the local door's distinction for a node waiting on
// its own pieces; the room uses it to say that the line woke the task.
type TaskSteered struct {
	Waiting bool `json:"waiting,omitempty"`
}

// TaskStopArgs uses the session's already-prefixed work id unchanged.
type TaskStopArgs struct {
	ID string `json:"id"`
}

// TaskStopped carries the engine's person-facing sentence without rewriting it.
type TaskStopped struct {
	Line string `json:"line,omitempty"`
}

// TaskStartArgs carries the person's brief without interpreting it locally.
type TaskStartArgs struct {
	Brief string `json:"brief"`
}

// PlannerStartArgs also carries the sizing hint used by the adaptive form.
type PlannerStartArgs struct {
	Brief string `json:"brief"`
	Hint  string `json:"hint,omitempty"`
}

// TaskStarted is the receipt the existing single-task note draws. Note is the
// engine's fallback line — "brief kept as you wrote it" — carried to the surface
// only when the engine's shaper was invoked and came back cut; empty otherwise.
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

// TaskJudged carries the bounded sizing answer back to the command.
type TaskJudged struct {
	Parallel bool     `json:"parallel,omitempty"`
	Parts    []string `json:"parts,omitempty"`
	Why      string   `json:"why,omitempty"`
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
