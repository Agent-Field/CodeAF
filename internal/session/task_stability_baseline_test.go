package session

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

// These frozen record schemas come from origin/dev before task placement.
// Comparing real saved bytes against them catches added fields and reordered
// fields without replacing the runtime values (paths, identities or clocks).
type originalTaskDocument struct {
	WorkspaceTransfer *taskcopy.WorkspacePlan `json:"workspace_transfer,omitempty"`
	Type              string                  `json:"type"`
	Version           int                     `json:"version"`
	Seq               uint64                  `json:"seq"`
	Nodes             []taskRecord            `json:"nodes"`
	Runs              []originalRunRecord     `json:"runs,omitempty"`
}

type originalRunRecord struct {
	RecoveredCompletion bool              `json:"recoveredCompletion,omitempty"`
	CrewState           *TaskCrewRecord   `json:"crewState,omitempty"`
	ID                  uint64            `json:"id"`
	Run                 string            `json:"run,omitempty"`
	Node                string            `json:"node,omitempty"`
	Parent              uint64            `json:"parent,omitempty"`
	Title               string            `json:"title,omitempty"`
	Kind                TaskKind          `json:"kind,omitempty"`
	Program             string            `json:"program,omitempty"`
	State               TaskState         `json:"state"`
	Stopped             bool              `json:"stopped,omitempty"`
	Report              string            `json:"report,omitempty"`
	Model               string            `json:"model,omitempty"`
	CostUSD             float64           `json:"costUsd,omitempty"`
	StartedAt           time.Time         `json:"startedAt,omitzero"`
	EndedAt             time.Time         `json:"endedAt,omitzero"`
	WorkspaceSet        *WorkspaceSet     `json:"workspaceSet,omitempty"`
	Copy                *TaskCopyRecord   `json:"copy,omitempty"`
	PendingRun          *PendingRunRecord `json:"pendingRun,omitempty"`
	PlanTask            string            `json:"planTask,omitempty"`
	ElapsedMS           int64             `json:"elapsed_ms,omitempty"`
	Ending              TaskEnding        `json:"ending,omitempty"`
	Branch              string            `json:"branch,omitempty"`
	Merge               string            `json:"merge,omitempty"`
	Result              string            `json:"result,omitempty"`
	Changed             []string          `json:"changed,omitempty"`
}

type originalTaskNotice struct {
	// app/v3 fields that predate remote placement.
	Retryable                                             bool
	Thinking                                              string
	ID                                                    uint64
	Run                                                   string
	Node                                                  string
	Title                                                 string
	Kind                                                  TaskKind
	Program                                               string
	Source                                                *TaskSource
	Dismissed                                             bool
	Ceiling                                               string
	Summary                                               string
	Brief                                                 string
	Acceptance                                            string
	Where                                                 string
	WorkspaceURI, ChatURI, ProjectURI, GroundURI, RootURI string
	Ground                                                string
	Mode                                                  TaskMode
	Rung                                                  GroundRung
	DependsOn                                             []uint64
	Parent                                                uint64
	Deadline                                              time.Time
	Irreversible                                          bool
	Withdrawn                                             string
	Decided                                               *TaskAnswer
	ModelOptions                                          []string
	Elsewhere                                             string
	State                                                 TaskState
	Elapsed                                               time.Duration
	StartedAt                                             time.Time
	EndedAt                                               time.Time
	Report                                                string
	Result                                                string
	ResultWhole                                           string
	ResultCut                                             bool
	ResultHeld                                            bool
	Changed                                               []string
	Branch                                                string
	Copy                                                  *TaskCopyRecord
	WorkspaceSet                                          *WorkspaceSet
	RecoveredCompletion                                   bool
	PendingRun                                            *PendingRunRecord
	PlanTask                                              string
	Merge                                                 string
	Doing                                                 string
	Context                                               string
	Mending                                               string
	Waiting                                               string
	Paused                                                bool
	Settling                                              string
	Stopped                                               bool
	Ending                                                TaskEnding
	Conflicts                                             []string
	Shifted                                               bool
	GroundHeld                                            bool
	Decider                                               TaskAskOwner
	Checked                                               provider.Reading
	Model                                                 string
	NextModel                                             string
	Crew                                                  *crewroute.Decision
	CrewState                                             *TaskCrewRecord `json:"-"`
	CostUSD                                               float64
	Tokens                                                int
}

type originalUsageLine struct {
	ID            string    `json:"id,omitempty"` // app/v3: each row names itself once
	At            time.Time `json:"at"`
	Day           string    `json:"day"`
	Model         string    `json:"model,omitempty"`
	Role          string    `json:"role,omitempty"`
	Seat          Seat      `json:"seat,omitempty"`
	Calls         int       `json:"calls,omitempty"`
	Input         int       `json:"in,omitempty"`
	Output        int       `json:"out,omitempty"`
	USD           float64   `json:"usd,omitempty"`
	Reconciled    bool      `json:"reconciled,omitempty"`
	Unbilled      bool      `json:"unbilled,omitempty"`
	Empty         bool      `json:"empty,omitempty"`
	Session       string    `json:"session,omitempty"`
	Task          string    `json:"task,omitempty"`
	Root          string    `json:"root,omitempty"`
	Standing      string    `json:"standing,omitempty"`
	Automation    string    `json:"automation,omitempty"`
	Workspace     string    `json:"workspace,omitempty"`
	Lane          string    `json:"lane,omitempty"`
	TTFTms        int64     `json:"ttft_ms,omitempty"`
	TPS           float64   `json:"tps,omitempty"`
	Hedged        bool      `json:"hedged,omitempty"`
	HedgeWasteUSD float64   `json:"hedge_waste_usd,omitempty"`
}

type originalJournalUsage struct {
	Model      string  `json:"model,omitempty"`
	Input      int     `json:"input,omitempty"`
	Output     int     `json:"output,omitempty"`
	CacheRead  int     `json:"cacheRead,omitempty"`
	CacheWrite int     `json:"cacheWrite,omitempty"`
	CostUSD    float64 `json:"costUsd,omitempty"`
	Calls      int     `json:"calls,omitempty"`
	DurationMS int64   `json:"durationMs,omitempty"`
	Aux        bool    `json:"aux,omitempty"`
	Empty      bool    `json:"empty,omitempty"`
	Role       string  `json:"role,omitempty"`
}

// The schema is the historical fixture, while raw is the actual persisted
// record. Unknown fields and different ordering therefore fail byte equality.
func assertOriginalLocalJSON(t *testing.T, raw []byte, original any, indented bool) {
	t.Helper()
	if err := json.Unmarshal(raw, original); err != nil {
		t.Fatal(err)
	}
	var expected []byte
	var err error
	if indented {
		expected, err = json.MarshalIndent(original, "", "  ")
	} else {
		expected, err = json.Marshal(original)
	}
	if err != nil {
		t.Fatal(err)
	}
	if indented {
		expected = append(expected, '\n')
	}
	if !bytes.Equal(raw, expected) {
		t.Fatalf("local record changed from the original schema:\n%s\nwant:\n%s", raw, expected)
	}
}
