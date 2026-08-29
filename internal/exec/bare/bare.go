package bare

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/provider"
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
	reading := b.photographBefore(ctx)

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
	}

	// The node these calls belong to, for the model-call log. The bare loop
	// deliberately attaches nothing else to the context (see loop.go on why it
	// sends no cache key), and this is not a knob: it changes no request, it
	// only names the work on the way past.
	outcome := loop.run(provider.WithCallNode(provider.WithCallTag(ctx, "leaf"), task.NodeKey))

	// Artifacts: whatever this leaf left in the workspace. The bare loop does not
	// own a git substrate, so this is the workspace's own record — the sweep
	// after every tool call above (the tools' claims) and the before/after
	// read of the tree (the world), together. Either alone was narrower than
	// the disk once: the sweep misses a file a shell command wrote, the diff
	// cannot say which call wrote it.
	b.workspace.RecordChanges(leaf)
	outcome.Artifacts = b.workspace.Artifacts(leaf)

	// The second reading, against the same entrypoint. Whether the tree actually
	// moved is the workspace's answer, taken from the two tree photographs it
	// has already compared — this asks it rather than re-stating the world.
	reading.photographAfter(ctx, cwd, len(outcome.Artifacts) > 0, outcome)
	return outcome, nil
}
