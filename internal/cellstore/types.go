package cellstore

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/cell"
)

const schemaV = 1

// Cell-relative paths of the files this package persists inside a cell.
const (
	ReceiptsDir = cell.StateDir + "/receipts"
	BlobsDir    = cell.StateDir + "/blobs"
	TurnsPath   = cell.StateDir + "/turns.jsonl"
)

// Quality is the seal's exactness (SCHEMAS.md 1): Quiescent when no call left
// a process running, Turbulent otherwise.
type Quality string

const (
	Quiescent Quality = "Quiescent"
	Turbulent Quality = "Turbulent"
)

// Trigger is why the turn exists. Setup turns are the agent installing what
// preflight listed.
type Trigger string

const (
	AgentRun Trigger = "AgentRun"
	Setup    Trigger = "Setup"
)

// Turn is one sealed commit. Field order is the wire order and V is first (L11).
// Times are unix milliseconds (SCHEMAS.md ruling 6). Build one with newTurn only.
type Turn struct {
	V             uint16   `json:"V"`
	ID            string   `json:"id"`
	Parent        string   `json:"parent,omitempty"`
	MergeParents  []string `json:"merge_parents,omitempty"`
	SealedAtMs    int64    `json:"sealed_at_ms"`
	Quality       Quality  `json:"quality"`
	Trigger       Trigger  `json:"trigger"`
	Device        string   `json:"device"`
	Fence         uint64   `json:"fence"`
	Receipt       string   `json:"receipt"`
	ExcludedPaths []string `json:"excluded_paths,omitempty"`
}

// Range is a byte range, end exclusive.
type Range struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

// Call is one tool call in a receipt. For a call whose SideEffect is external
// the output also lives at BlobsDir/<hash> (SCHEMAS.md ruling 3).
type Call struct {
	Tool       string `json:"tool"`
	ArgsHash   string `json:"args_hash"`
	Started    int64  `json:"started"`
	Ended      int64  `json:"ended"`
	Exit       int    `json:"exit"`
	StdoutHash string `json:"stdout_hash"`
	StderrHash string `json:"stderr_hash"`
	SideEffect string `json:"side_effect"`
}

// ServiceRec is a process a call left alive. Argv[0] is a basename (L1).
type ServiceRec struct {
	PID   int      `json:"pid"`
	Argv  []string `json:"argv"`
	Ports []int    `json:"ports,omitempty"`
}

// ModelCall is one model call of the turn, by hashes.
type ModelCall struct {
	Model        string `json:"model"`
	ParamsHash   string `json:"params_hash"`
	RequestHash  string `json:"request_hash"`
	ResponseHash string `json:"response_hash"`
	TokensIn     uint32 `json:"tokens_in"`
	TokensOut    uint32 `json:"tokens_out"`
	CostMicroUSD int64  `json:"cost_micro_usd"`
}

// Receipt is what a phone needs to render a turn (SCHEMAS.md 5).
type Receipt struct {
	V          uint16       `json:"V"`
	Transcript Range        `json:"transcript_range"`
	Calls      []Call       `json:"calls"`
	Services   []ServiceRec `json:"services,omitempty"`
	ModelCalls []ModelCall  `json:"model_calls,omitempty"`
}

// Executed is one finished call as the device knows it: the receipt row, the
// processes it left, and, for an external call only, its output.
type Executed struct {
	Call     Call         `json:"call"`
	Services []ServiceRec `json:"services,omitempty"`
	Exact    bool         `json:"exact"`
	// Trigger is why the call ran: Setup for a setup turn's call. Empty is an
	// ordinary run.
	Trigger Trigger `json:"trigger,omitempty"`
	// Changed is the workspace paths the call is known to have changed; nil is
	// unknown, which is what any call that could touch anything leaves.
	Changed []string `json:"changed,omitempty"`
	Stdout  []byte   `json:"stdout,omitempty"`
	Stderr  []byte   `json:"stderr,omitempty"`
}

// TurnInfo is everything the caller knows about the turn being sealed.
type TurnInfo struct {
	Trigger Trigger
	Calls   []Executed
	Models  []ModelCall
	// Changed lists the cell-relative paths that changed since the previous
	// seal. Nil means unknown: the engine walks the whole folder, and its stat
	// cache keeps that to one stat per unchanged file. Non-nil is a promise
	// that nothing else changed, and the engine visits only these paths.
	Changed []string
}

// Sealed is a finished seal.
type Sealed struct {
	Turn    Turn
	Receipt Receipt
}

// Store seals a cell. The stage 0 implementation spawns the engine per seal;
// a daemon can replace it without touching callers.
type Store interface {
	Seal(ctx context.Context, c cell.Cell, info TurnInfo) (Sealed, error)
}
