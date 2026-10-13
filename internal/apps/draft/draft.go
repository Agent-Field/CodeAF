// Package draft is the Builder's draft store: the journal a task worker's
// `codeaf app` verbs write one checked piece at a time, the fold that turns the
// journal into one State, and the pure render of that State into the app's
// definition files (docs/apps/builder-loop/DESIGN.md §4). It imports spec and
// nothing that holds records, so the worker's shell can reach it without any
// service of the running server.
package draft

import (
	"context"
	"errors"
	"io"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

const (
	JournalFile = "draft.jsonl"
	LockFile    = "draft.lock"
	SocketFile  = "builder.sock"
	// DirEnv overrides where the CLI looks for the draft; without it the CLI
	// reads dir($PLANDB_DB), then walks up from its working directory.
	DirEnv = "CODEAF_APP_DRAFT"
)

// Author says who wrote an entry: the worker, the owner from the plan card, or
// codeaf itself (the done phase after Check passed).
type Author string

const (
	AuthorBuilder Author = "builder"
	AuthorOwner   Author = "owner"
	AuthorCodeaf  Author = "codeaf"
)

// Piece names what one entry changed.
type Piece struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}

// Entry is one journal line.
type Entry struct {
	Seq    int      `json:"seq"`
	At     string   `json:"at"`
	Author Author   `json:"author"`
	Verb   string   `json:"verb"`
	Args   []string `json:"args,omitempty"`
	Body   string   `json:"body,omitempty"`
	Piece  Piece    `json:"piece"`
	// Line is the owner-readable sentence for the plan card and the log. It is
	// never a verb and never an error.
	Line string `json:"line"`
}

// Phase is where the draft is in the build.
type Phase string

const (
	PhasePlanning Phase = "planning"
	PhasePlanned  Phase = "planned"
	PhaseBuilding Phase = "building"
	PhaseDone     Phase = "done"
)

// State is the fold of the journal. L1 fills the fields; the shape below is
// the part the Builder reads.
type State struct {
	Name  string
	About string
	Plan  spec.Plan
	Phase Phase
}

// Defaults are the facts Render takes from the create sheet, not the worker.
type Defaults struct {
	AppID       string
	Space       string
	AgentModel  string
	BudgetMonth float64
	Kit         int
}

// Store is one build's draft folder.
type Store struct{ dir string }

var errUnbuilt = errors.New("draft: not built yet")

func Open(dir string) (*Store, error) { return nil, errUnbuilt }
func (s *Store) Dir() string          { return s.dir }
func (s *Store) Append(a Author, verb string, args []string, body string) (Entry, error) {
	return Entry{}, errUnbuilt
}
func (s *Store) Entries(since int) ([]Entry, error) { return nil, errUnbuilt }
func (s *Store) State() (State, error)              { return State{}, errUnbuilt }

func Fold(entries []Entry) State             { return State{} }
func Render(st State, d Defaults) spec.Files { return spec.Files{} }

// Request and Reply are the server verbs' wire over SocketFile.
type Request struct {
	Verb string   `json:"verb"`
	Args []string `json:"args"`
}

type Reply struct {
	OK   bool   `json:"ok"`
	Text string `json:"text"`
}

// Handler answers one server verb.
type Handler func(context.Context, Request) Reply

func Serve(ctx context.Context, dir string, h Handler) (stop func(), err error) {
	return nil, errUnbuilt
}

// Main is `codeaf app <verb>`: exit 0 ok, 1 refused, 2 usage, 3 Builder not running.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int { return 2 }
