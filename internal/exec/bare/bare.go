package bare

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// BareSubharness is this worker's name, matching the registration in
// cmd/aforge/subharness.go. It lives here so the executor and the wiring agree
// without one importing the other.
const BareSubharness = "bare"

// Bare is a minimal single-agent loop that replicates pi 0.82.1's wire
// behavior exactly: the same system prompt, the same four wire tools, the same
// turn/stop/retry/compaction semantics. It is the cheapest whole-taker for
// small work — one agent, one sitting, no aforge contract/digest/discipline
// blocks, no cache key in the provider context.
//
// It reuses internal/provider's Client for the outbound HTTP path and the
// bare package's own Tools for the four wire tools. The loop itself
// (loop.go) owns message assembly, retry, compaction, and usage accounting.
type Bare struct {
	workspace *exec.Workspace
	model     string
	apiKey    string
	baseURL   string
	deadline  time.Duration
	// history is where this worker's readings of the project's own checks are
	// journaled. It is optional and usually nil — a bare leaf run outside a
	// graph has no journal to write into — and every reading is taken and
	// weighed exactly the same way with or without it. See journal.
	history *store.Store
}

// New builds the bare executor. The arguments mirror exec.NewSWE: the workspace
// the leaf writes into, the model/apiKey/baseURL for the provider client, and
// the wall-clock deadline. The provider client is constructed per-Run rather
// than here, because a client is cheap to build and the loop owns its context.
func New(workspace *exec.Workspace, model, apiKey, baseURL string, deadline time.Duration) *Bare {
	return &Bare{
		workspace: workspace,
		model:     model,
		apiKey:    apiKey,
		baseURL:   baseURL,
		deadline:  deadline,
	}
}

// WithStore installs the run's journal, so the readings this worker takes of the
// project's own checks leave a row that an autopsy can read. It mirrors
// Linear.WithStore, which is the same seam for the same reason.
func (b *Bare) WithStore(history *store.Store) *Bare {
	b.history = history
	return b
}

// Subharness returns the registered name.
func (b *Bare) Subharness() string { return BareSubharness }

// leafKey replicates exec.Task.leafKey (which is unexported): NodeKey when
// set, otherwise NodeID as a decimal string. It is the identity everything
// this leaf writes is filed under.
func leafKey(task exec.Task) string {
	if key := strings.TrimSpace(task.NodeKey); key != "" {
		return key
	}
	return strconv.Itoa(task.NodeID)
}

// Run executes one task as a bare pi-style loop. The task's Brief is the plain
// user message — no aforge contract, digest, or discipline blocks. The outcome
// mirrors exec.Linear's shape: Text is the final assistant text, Usage carries
// prompt/completion/cached/cost, Turns is the number of model requests, and
// Artifacts are whatever the tools wrote.
func (b *Bare) Run(ctx context.Context, task exec.Task) (*exec.Outcome, error) {
	client, err := provider.NewClient(provider.Config{
		APIKey:  b.apiKey,
		BaseURL: b.baseURL,
		Model:   b.model,
		Timeout: b.deadline,
	})
	if err != nil {
		return &exec.Outcome{Stop: exec.StopError, Text: err.Error()}, err
	}

	// The project's own account of whether it still works, read while the tree
	// is still pristine. It is taken before the tree is photographed because a
	// test runner's own droppings belong to the world the leaf arrived in and
	// not to the leaf — see photographBefore.
	reading, inherited := b.photographBefore(ctx, task)

	// The world's own account of what this leaf leaves behind, opened before the
	// first turn. The bare loop's four wire tools are pi's, and one of them is a
	// shell: a leaf here can write a whole deliverable without any write tool
	// hearing about it, which is exactly the evidence gap this closes.
	b.workspace.WatchTree(leafKey(task))

	cwd := b.workspace.Root()
	tools := Tools(cwd)
	system := SystemPrompt(cwd)
	// The brief is the plain task text — pi sends no aforge contract, digest,
	// or discipline blocks. The task's own rendering is its Brief field.
	userText := strings.TrimSpace(task.Brief)
	if userText == "" {
		userText = strings.TrimSpace(task.Goal)
	}

	leaf := leafKey(task)
	loop := &loopState{
		client: client,
		tools:  tools,
		// Every file a tool call leaves behind is filed under this leaf, the
		// same way exec.Toolbox files what a shell command produces. Without
		// it the registry the delivery gate reads was structurally empty for a
		// bare leaf — the file was on disk and the run said it was not.
		sweep: func() func() {
			before := b.workspace.Snapshot()
			return func() { b.workspace.RecordProducedSince(leaf, before) }
		},
		system:   system,
		user:     userText,
		cwd:      cwd,
		deadline: b.deadline,
		// Where an interruption goes so somebody watching the run hears about
		// it. It is the task's own seam, so a bare leaf's cut stream lands in
		// the journal beside a recovered panic's and reads the same way.
		faulted: task.Faulted,
	}

	// The node these calls belong to, for the model-call log. The bare loop
	// deliberately attaches nothing else to the context (see loop.go on why it
	// sends no cache key), and this is not a knob: it changes no request, it
	// only names the work on the way past.
	// The leaf's own closing, armed for the one exit where this loop finishes
	// under its own power. The decision, its bound and its journal are
	// exec.SelfCloser's — shared with the generalist belt, because a mechanism
	// only one worker has is one the run does not. What is local here is the
	// reading itself, and the answer to "is the outcome in hand already
	// photographed", which is what keeps a leaf that lands cleanly from paying
	// for a second reading it does not need.
	closer := exec.NewSelfCloser(b.history, task)
	photographed := false
	closing := func(outcome *exec.Outcome) string {
		b.readFinished(leaf, task, ctx, reading, inherited, outcome)
		// The bare loop meters neither turns nor tokens; its whole envelope is
		// the wall, and what is left of it is what it was granted less what it
		// has spent. A loop running with no deadline at all says so rather than
		// reading as a clock that has run out. The reserve is the loop's own
		// measured landing cost — the room already set aside for this leaf to
		// finish safely, and not one second more.
		wall := exec.NoWall
		if b.deadline > 0 {
			wall = b.deadline - outcome.Elapsed
		}
		note, _ := closer.Close(outcome, exec.RoomLeft(outcome, 0, 0, wall, loop.measured.reserve()))
		photographed = note == ""
		return note
	}

	outcome := loop.runClosing(
		provider.WithCallNode(provider.WithCallTag(ctx, "leaf"), task.NodeKey), closing)

	// Every other way out of the loop — the wall, a provider that stopped
	// answering, a context that ended — reaches here with no reading taken, and
	// a reading nobody took is the silence exec.PhotographAfter exists to end.
	if !photographed {
		b.readFinished(leaf, task, ctx, reading, inherited, outcome)
	}
	return outcome, nil
}

// readFinished is what this worker does with a finished tree, in one place
// because it is now done at two moments: when the loop offers an answer, so the
// leaf can be shown its own finding while it is still standing, and on every
// other way out, where nobody offered anything.
//
// Artifacts are whatever this leaf left in the workspace. The bare loop does not
// own a git substrate, so this is the workspace's own record — the sweep after
// every tool call (the tools' claims) and the before/after read of the tree (the
// world), together. Either alone was narrower than the disk once: the sweep
// misses a file a shell command wrote, the diff cannot say which call wrote it.
//
// Then the second reading, against the same entrypoint. Whether the tree
// actually moved is the workspace's answer, taken from the two tree photographs
// it has already compared — this asks it rather than re-stating the world.
func (b *Bare) readFinished(
	leaf string, task exec.Task, ctx context.Context,
	reading verify.Reading, inherited bool, outcome *exec.Outcome,
) {
	b.workspace.RecordChanges(leaf)
	outcome.Artifacts = b.workspace.Artifacts(leaf)
	b.photographAfter(ctx, task, reading, len(outcome.Artifacts) > 0, inherited, outcome)
}
