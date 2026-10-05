package backing

// The App the audit runs on: sec-af's four verbs answered by codeaf.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/secaf/appx"
)

// Config is how the App runs: where, on which models, and how many sessions
// at once.
type Config struct {
	// Root is the repository under audit, absolute. No session reads outside
	// it, whatever a caller's options say.
	Root string
	// SessionModel answers the agent sessions — the hunters, tracers and
	// provers that read the code — and AIModel the single structured calls:
	// the duplicate checks, the exploit reasoning, the compliance mapping.
	// Empty leaves the choice to the model API, which answers on the run's
	// work seat.
	SessionModel, AIModel string
	// Sessions is how many agent sessions run at once across the whole audit,
	// and Calls how many single calls. sec-af's own fan-outs multiply — four
	// hunters each enriching five locations — and the old harness capped its
	// processes at eight for the same reason.
	Sessions, Calls int
	// MaxTurns bounds one session's turns; sec-af's own default was fifty.
	MaxTurns int
	// SessionWall bounds one session's time; sec-af's harness waited thirty
	// minutes.
	SessionWall time.Duration
	// Watch hears every session, call and note as it ends, for the task's
	// page. Nil hears nothing.
	Watch *Watch
}

// Watch is what the audit's progress is told to.
type Watch struct {
	// Session is one agent session that ended: its label (the agent that ran
	// it, such as `hunt-scan`), how it went, and the error that stopped it
	// before it could answer.
	Session func(label string, result SessionResult, err error)
	// Note is one of sec-af's progress notes, with its tags.
	Note func(message string, tags []string)
}

// App is [appx.App] on codeaf.
type App struct {
	config   Config
	client   *Client
	sessions chan struct{}
	calls    chan struct{}
	threads  atomic.Int64
	mu       sync.Mutex
	cost     float64
	count    struct{ sessions, calls int }
}

var _ appx.App = (*App)(nil)

// New opens the App over the run's model API.
func New(client *Client, config Config) *App {
	if config.Sessions <= 0 {
		config.Sessions = 8
	}
	if config.Calls <= 0 {
		config.Calls = 8
	}
	if config.MaxTurns <= 0 {
		config.MaxTurns = 50
	}
	if config.SessionWall <= 0 {
		config.SessionWall = 30 * time.Minute
	}
	return &App{config: config, client: client,
		sessions: make(chan struct{}, config.Sessions), calls: make(chan struct{}, config.Calls)}
}

// Spent is what the audit's calls have cost so far, as the replies priced
// them, and how many sessions and single calls it made. The model API's own
// meter is what bills the run; this is the audit's own account of itself for
// its report.
func (a *App) Spent() (cost float64, sessions, calls int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cost, a.count.sessions, a.count.calls
}

func (a *App) spend(cost float64, session bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cost += cost
	if session {
		a.count.sessions++
	} else {
		a.count.calls++
	}
}

// Harness runs one agent session over the repository. A session that ran and
// gave no answer of its schema is a result with IsError, as the old harness
// reported one; a session that could not run is the error.
func (a *App) Harness(ctx context.Context, prompt string, schema map[string]any, dest any, opts appx.HarnessOptions) (*appx.HarnessResult, error) {
	label := sessionLabel(opts.Label, opts.Cwd)
	select {
	case a.sessions <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-a.sessions }()
	root := a.config.Root
	// THE REPOSITORY IS THE APP'S, NOT THE CALLER'S. Every agent names the
	// folder it was handed; one that named another would be read nowhere.
	if project := strings.TrimSpace(opts.ProjectDir); project != "" && filepath.Clean(project) != filepath.Clean(root) {
		if real, err := filepath.EvalSymlinks(project); err != nil || !within(root, real) {
			return nil, fmt.Errorf("%s asked to read %s, which is not the repository under audit", label, project)
		}
	}
	thread := fmt.Sprintf("%s-%d", strings.ReplaceAll(label, " ", "-"), a.threads.Add(1))
	result, err := RunSession(ctx, a.client, SessionOrder{
		Model: a.config.SessionModel, Thread: thread, Root: root, Prompt: prompt, Schema: schema,
		MaxTurns: a.config.MaxTurns, Wall: a.config.SessionWall,
	})
	a.spend(result.CostUSD, true)
	if watch := a.config.Watch; watch != nil && watch.Session != nil {
		watch.Session(label, result, err)
	}
	if err != nil {
		return nil, err
	}
	cost := result.CostUSD
	out := &appx.HarnessResult{Result: result.Text, NumTurns: result.Turns,
		DurationMS: result.Duration.Milliseconds(), CostUSD: &cost}
	if result.Failed != "" {
		out.IsError, out.ErrorMessage = true, result.Failed
		return out, nil
	}
	if schema != nil && dest != nil {
		if err := json.Unmarshal(result.JSON, dest); err != nil {
			out.IsError, out.ErrorMessage = true, "the answer met its schema but does not decode: "+err.Error()
			return out, nil
		}
		out.Parsed = dest
	}
	return out, nil
}

// AI makes one structured call. A served model that refuses a schema-bound
// answer is asked once more with the schema in words, which every model
// follows and sec-af's tolerant reading (internal/secaf/aix) parses.
func (a *App) AI(ctx context.Context, prompt string, opts ...ai.Option) (*ai.Response, error) {
	request := &ai.Request{Messages: []ai.Message{textMessage("user", prompt)}}
	for _, option := range opts {
		if err := option(request); err != nil {
			return nil, err
		}
	}
	select {
	case a.calls <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-a.calls }()
	model := strings.TrimPrefix(strings.TrimSpace(request.Model), "openrouter/")
	if model == "" {
		model = a.config.AIModel
	}
	call := Request{Model: model, Thread: fmt.Sprintf("call-%d", a.threads.Add(1)),
		Messages: request.Messages, ResponseFormat: request.ResponseFormat}
	response, err := a.client.Complete(ctx, call)
	var refused *RefusedError
	if err != nil && call.ResponseFormat != nil && !errors.As(err, &refused) && ctx.Err() == nil {
		call.Messages = withSchemaInWords(call.Messages, call.ResponseFormat)
		call.ResponseFormat = nil
		response, err = a.client.Complete(ctx, call)
	}
	a.spend(costOf(response), false)
	return response, err
}

// Note is one of sec-af's progress notes.
func (a *App) Note(_ context.Context, message string, tags ...string) {
	if watch := a.config.Watch; watch != nil && watch.Note != nil {
		watch.Note(message, tags)
	}
}

// Call is never answered here: the audit's calls between its own reasoners
// stay in the process (internal/secaf/audit's local calls), which wrap this
// App. A call that reached it is a reasoner nobody registered.
func (a *App) Call(_ context.Context, target string, _ map[string]any) (map[string]any, error) {
	return nil, fmt.Errorf("no reasoner %s is carried in this audit", target)
}

// withSchemaInWords puts a schema the served model would not take as a
// response format into the last user message, as words.
func withSchemaInWords(messages []ai.Message, format *ai.ResponseFormat) []ai.Message {
	if format == nil || format.JSONSchema == nil {
		return messages
	}
	out := append([]ai.Message(nil), messages...)
	out = append(out, textMessage("user", "Reply with ONLY one JSON object that meets this JSON Schema — no prose and no code fence:\n"+string(format.JSONSchema.Schema)))
	return out
}

// tempSuffix is the random tail os.MkdirTemp puts on a scratch folder's name.
var tempSuffix = regexp.MustCompile(`-?\d+$`)

// camelBoundary is where a CamelCase agent name's words meet.
var camelBoundary = regexp.MustCompile(`([a-z])([A-Z])`)

// sessionLabel is the agent a session ran for, in a person's words: the label
// sec-af's call gave it (`DataFlowTracer` is `data flow tracer`), else what
// the scratch folder sec-af made for it says (`secaf-hunt-scan-123456` is
// `hunt scan`), else `agent`.
func sessionLabel(label, cwd string) string {
	if label = strings.TrimSpace(label); label != "" {
		return strings.ToLower(camelBoundary.ReplaceAllString(label, "$1 $2"))
	}
	name, scratch := strings.CutPrefix(filepath.Base(strings.TrimSpace(cwd)), "secaf-")
	if !scratch {
		return "agent"
	}
	name = strings.Trim(tempSuffix.ReplaceAllString(name, ""), "-")
	if name == "" || strings.ContainsAny(name, `/\`) {
		return "agent"
	}
	return strings.ReplaceAll(name, "-", " ")
}
