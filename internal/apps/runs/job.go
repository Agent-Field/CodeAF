package runs

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// job is the durable interpreter state of one run. Definition steps are loaded
// from the store on each advance so the journal does not keep a second copy.
type job struct {
	Seq        int                `json:"seq"`
	State      bridge.RunState    `json:"state"`
	Member     string             `json:"member"`
	Role       svc.Role           `json:"role"`
	Agent      bool               `json:"agent"`
	Data       svc.DataMode       `json:"data"`
	Month      float64            `json:"month"`
	AtCap      string             `json:"at_cap,omitempty"`
	Env        map[string]any     `json:"env"`
	Done       map[string]stepRec `json:"done"`
	Loops      map[string]loopRec `json:"loops"`
	Attempts   map[string]int     `json:"attempts"`
	WaitPath   string             `json:"wait_path,omitempty"`
	WaitKind   string             `json:"wait_kind,omitempty"`
	WaitUntil  time.Time          `json:"wait_until,omitempty"`
	ApprovalID string             `json:"approval_id,omitempty"`
	Answer     string             `json:"answer,omitempty"`
	Edit       map[string]any     `json:"edit,omitempty"`
	Jump       string             `json:"jump,omitempty"`
	JumpPrefix string             `json:"jump_prefix,omitempty"`
}

type stepRec struct {
	Status   bridge.RunStatus `json:"status"`
	Output   any              `json:"output,omitempty"`
	AI       bool             `json:"ai,omitempty"`
	Error    string           `json:"error,omitempty"`
	Approval string           `json:"approval,omitempty"`
	Title    string           `json:"title,omitempty"`
}

type loopRec struct {
	Items []any  `json:"items"`
	Index int    `json:"index"`
	Max   int    `json:"max"`
	As    string `json:"as,omitempty"`
}

type piece struct {
	kind string
	path string
	step *stepView
}

// stepView is the step plus a gate the YAML omitted. Outbound tools always
// wait on send, even when the file has no gate.
type stepView struct {
	step *stepBody
	gate *gateBody
}

type stepBody struct {
	ID            string
	Title         string
	If            string
	OnError       string
	RetryMax      int
	RetryBackoff  string
	Agent         string
	Prompt        string
	Output        map[string]any
	Tool          string
	As            string
	Args          map[string]any
	RecordsQuery  *recBody
	RecordsCreate *recBody
	RecordsUpdate *recBody
	RecordsDelete *recBody
	Wait          *waitBody
	OnTimeout     string
	Loop          *loopBody
	Steps         []*stepBody
	Shell         string
	Workspace     string
}

type gateBody struct {
	Approval string
	Who      string
	Title    string
}

type recBody struct {
	Type  string
	Query string
	ID    string
	IDs   any
	Set   map[string]any
	Limit int
}

type waitBody struct {
	For     string
	Until   string
	Thread  string
	Timeout string
}

type loopBody struct {
	Over  string
	As    string
	Until string
	Max   int
}

func terminal(status bridge.RunStatus) bool {
	switch status {
	case bridge.RunDone, bridge.RunFailed, bridge.RunStopped, bridge.RunNeedsYou:
		return true
	default:
		return false
	}
}
