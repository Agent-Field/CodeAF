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

// TaskStarted is the receipt the existing single-task note draws.
type TaskStarted struct {
	ID    uint64 `json:"id,omitempty"`
	Title string `json:"title,omitempty"`
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
