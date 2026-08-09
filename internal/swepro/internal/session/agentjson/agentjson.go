// Package agentjson is a bug-for-bug port of
// src/session/agent-json.ts:1-821 (swe-pro 3b25a1a). It dispatches one baked
// decision agent under a strict JSON-file contract, validates the artifact,
// gives malformed output same-session parse/schema repair turns, retries in a
// fresh child session, and finally returns a caller fallback or exact failure.
//
// Framework dependencies are narrow seams: Resolver returns the source's first
// tier candidate, and Client.Run owns one agent turn. Request.SessionID stays
// constant across repair turns and changes across full retries. Request's
// BetweenStepReminder callback is the source bus watcher's parse-only check.
package agentjson

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/baked"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

const (
	defaultMaxRetries = 1
	defaultTimeoutMS  = 15 * 60_000
	fixTimeoutMS      = 20 * 60_000
	// After the deadline fires, a cancelled client gets this long to finish
	// its artifact write before the caller must treat that artifact as still
	// owned by the live goroutine. Scaled off the call's own timeout so a
	// production 15-minute dispatch is forgiving of CPU pressure — a tight
	// bound there would turn ordinary load into spurious fallbacks — while a
	// short-timeout call still resolves quickly.
	minClientStopGrace = 250 * time.Millisecond
	maxClientStopGrace = 5 * time.Second
	// A provider that ignores cancellation must not create an unbounded number
	// of retained client goroutines. Cooperative calls hold the same slots only
	// for their normal Run lifetime.
	maxActiveClientCalls = 64
)

var (
	activeClientSlots = make(chan struct{}, maxActiveClientCalls)
	orphanedClients   atomic.Int64
)

// ActiveOrphanedClients reports timed-out Client.Run calls that still have not
// returned. Besides observability, invokeClient's global slot cap bounds these
// retained goroutines to maxActiveClientCalls.
func ActiveOrphanedClients() int64 { return orphanedClients.Load() }

func clientStopGrace(timeout time.Duration) time.Duration {
	grace := timeout / 10
	if grace < minClientStopGrace {
		return minClientStopGrace
	}
	if grace > maxClientStopGrace {
		return maxClientStopGrace
	}
	return grace
}

type artifactUnsafeError struct {
	cause error
	grace time.Duration
}

func (err *artifactUnsafeError) Error() string {
	return fmt.Sprintf(
		"agent-json: client did not stop within %s after %v; artifact remains owned by the live client",
		err.grace, err.cause,
	)
}

func (err *artifactUnsafeError) Unwrap() error { return err.cause }

type Model struct {
	ModelID    string `json:"modelID"`
	ProviderID string `json:"providerID"`
}

type Resolver interface {
	CandidatesForTier(tier baked.Tier) []string
}

type ResolverFunc func(tier baked.Tier) []string

func (f ResolverFunc) CandidatesForTier(tier baked.Tier) []string { return f(tier) }

type ToolSetting struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type ToolSettings []ToolSetting

func (settings ToolSettings) MarshalJSON() ([]byte, error) {
	var out strings.Builder
	out.WriteByte('{')
	for i, setting := range settings {
		if i > 0 {
			out.WriteByte(',')
		}
		key, err := jscompat.Stringify(setting.Name)
		if err != nil {
			return nil, err
		}
		out.Write(key)
		out.WriteByte(':')
		if setting.Enabled {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	}
	out.WriteByte('}')
	return []byte(out.String()), nil
}

type Phase string

const (
	PhaseMain      Phase = "main"
	PhaseParseFix  Phase = "parse-fix"
	PhaseSchemaFix Phase = "schema-fix"
)

type Request struct {
	Agent           string
	AgentMarkdown   string
	ParentSessionID string
	SessionID       string
	MessageID       string
	Model           Model
	Workspace       string
	TaskPrompt      string
	Reminder        string
	Tools           ToolSettings
	Phase           Phase
	Attempt         int
	// OutputPath is private to this dispatch. The same path is embedded in the
	// reminders; clients should write only this artifact.
	OutputPath string

	// BetweenStepReminder returns the source watcher injection once per new
	// parse error, clears its de-dup state when JSON becomes valid, and returns
	// "" for missing/empty/unchanged files.
	BetweenStepReminder func() string
}

type Client interface {
	Run(ctx context.Context, request Request) error
}

type ClientFunc func(ctx context.Context, request Request) error

func (f ClientFunc) Run(ctx context.Context, request Request) error { return f(ctx, request) }

type TimeoutContextFunc func(
	context.Context, time.Duration,
) (context.Context, context.CancelFunc)

type Validation[T any] struct {
	Data   T
	Issues []Issue
}

func (v Validation[T]) Success() bool { return len(v.Issues) == 0 }

type Schema[T any] interface {
	SafeParse(raw json.RawMessage) Validation[T]
}

type SchemaFunc[T any] func(raw json.RawMessage) Validation[T]

func (f SchemaFunc[T]) SafeParse(raw json.RawMessage) Validation[T] { return f(raw) }

type Input[T any] struct {
	Agent             string
	ParentSessionID   string
	Workspace         string
	TaskPrompt        string
	OutputPath        string
	Schema            Schema[T]
	Fallback          *T
	MaxRetries        *int
	Tools             []ToolSetting
	TimeoutMS         *int64
	Tier              *baked.Tier
	Label             *string
	ParseFixBudget    int
	SchemaFixBudget   int
	PreserveOnSuccess bool
	// SessionStarted exposes the fresh main-attempt request immediately before
	// Client.Run. Callers own any matching cleanup after DispatchJSON returns.
	SessionStarted func(Request)
}

type Result[T any] struct {
	Data         T    `json:"data"`
	FirstTry     bool `json:"firstTry"`
	UsedFallback bool `json:"usedFallback"`
}

type Dependencies struct {
	Resolver       Resolver
	Client         Client
	NewID          func(prefix string) string
	TimeoutContext TimeoutContextFunc
}

func DispatchJSON[T any](
	ctx context.Context, input Input[T], deps Dependencies,
) (Result[T], error) {
	var zero Result[T]
	label := input.Agent
	if input.Label != nil {
		label = *input.Label
	}
	maxRetries := defaultMaxRetries
	if input.MaxRetries != nil {
		maxRetries = *input.MaxRetries
	}
	timeoutMS := int64(defaultTimeoutMS)
	if input.TimeoutMS != nil {
		timeoutMS = *input.TimeoutMS
	}
	tier := baked.TierHigh
	if input.Tier != nil {
		tier = *input.Tier
	}

	markdown, ok := baked.GetBakedAgent(input.Agent)
	if !ok {
		return zero, fmt.Errorf("agent-json: baked agent not found: %s", input.Agent)
	}
	if deps.Resolver == nil {
		return zero, errors.New("agent-json: nil model resolver")
	}
	candidates := deps.Resolver.CandidatesForTier(tier)
	if len(candidates) == 0 {
		if input.Fallback != nil {
			return fallbackResult(*input.Fallback), nil
		}
		return zero, fmt.Errorf(
			"agent-json: no model available for tier=%s, agent=%s", tier, input.Agent,
		)
	}
	model := splitModel(candidates[0])
	if err := ensureDir(input.OutputPath); err != nil {
		return zero, err
	}
	safeUnlink(input.OutputPath)
	// TS src/session/agent-json.ts:523-563 assumes the timed prompt has ended
	// before reading input.outputPath. Go cancellation is advisory, so isolate
	// that same read/repair lifecycle on a per-dispatch path instead.
	artifactPath, err := privateArtifactPath(input.OutputPath)
	if err != nil {
		return zero, err
	}
	artifactOwnedByDispatch := true
	defer func() {
		if artifactOwnedByDispatch {
			safeUnlink(artifactPath)
		}
	}()
	tools := MergeTools(input.Tools)
	if input.Schema == nil {
		return zero, errors.New("agent-json: nil schema")
	}
	if deps.NewID == nil {
		deps.NewID = nextID
	}
	abortUnsafeArtifact := func(invokeErr error) (Result[T], error, bool) {
		var unsafeErr *artifactUnsafeError
		if !errors.As(invokeErr, &unsafeErr) {
			return zero, nil, false
		}
		// invokeClient has transferred cleanup ownership to the still-running
		// client before returning this error.
		artifactOwnedByDispatch = false
		if input.Fallback != nil {
			return fallbackResult(*input.Fallback), nil, true
		}
		return zero, invokeErr, true
	}

	attempts := 0
	allowedAttempts := maxRetries + 1
	var lastParseError string
	for attempts < allowedAttempts {
		attempts++
		isRetry := attempts > 1
		var retryNote *string
		if isRetry {
			last := lastParseError
			if last == "" {
				last = "unknown error"
			}
			note := "Your previous JSON failed validation: " + last +
				". Rewrite the file at the output path EXACTLY matching the schema. " +
				"Do not add fields not in the schema."
			retryNote = &note
		}
		reminder := BuildSystemReminder(BuildPromptArgs{
			TaskPrompt: input.TaskPrompt, OutputPath: artifactPath,
			Workspace: input.Workspace, Label: label, RetryNote: retryNote,
		})
		sessionID := deps.NewID("session")
		watcher := newOutputWatcher(artifactPath)
		request := Request{
			Agent: input.Agent, AgentMarkdown: markdown,
			ParentSessionID: input.ParentSessionID, SessionID: sessionID,
			MessageID: deps.NewID("message"), Model: model,
			Workspace: input.Workspace, TaskPrompt: input.TaskPrompt,
			Reminder: reminder, Tools: cloneTools(tools), Phase: PhaseMain,
			Attempt: attempts, OutputPath: artifactPath, BetweenStepReminder: watcher,
		}
		if input.SessionStarted != nil {
			input.SessionStarted(request)
		}
		invokeErr := invokeClient(
			ctx, deps.Client, request, durationMS(timeoutMS), deps.TimeoutContext,
		)
		if result, err, stop := abortUnsafeArtifact(invokeErr); stop {
			return result, err
		}
		if err := adoptConfiguredArtifact(input.OutputPath, artifactPath); err != nil {
			return zero, err
		}

		parseFixBudget := input.ParseFixBudget
		if parseFixBudget < 0 {
			parseFixBudget = 0
		}
		schemaFixBudget := input.SchemaFixBudget
		if schemaFixBudget < 0 {
			schemaFixBudget = 0
		}
		state := readFileState(artifactPath)

		if (state.Kind == FileParseError || state.Kind == FileEmpty) && parseFixBudget > 0 {
			for parseAttempt := 1; parseAttempt <= parseFixBudget; parseAttempt++ {
				errMsg := state.Error
				if state.Kind == FileEmpty {
					errMsg = "file is empty — agent did not produce any JSON"
				}
				fix := BuildParseFixReminder(ParseFixReminderArgs{
					OutputPath: artifactPath, ParseError: errMsg,
					Attempt: parseAttempt, Budget: parseFixBudget, Label: label,
				})
				fixRequest := request
				fixRequest.MessageID = deps.NewID("message")
				fixRequest.TaskPrompt = ""
				fixRequest.Reminder = fix
				fixRequest.Phase = PhaseParseFix
				fixRequest.Attempt = parseAttempt
				fixRequest.BetweenStepReminder = nil
				invokeErr = invokeClient(
					ctx, deps.Client, fixRequest,
					durationMS(min64(timeoutMS, fixTimeoutMS)), deps.TimeoutContext,
				)
				if result, err, stop := abortUnsafeArtifact(invokeErr); stop {
					return result, err
				}
				if err := adoptConfiguredArtifact(input.OutputPath, artifactPath); err != nil {
					return zero, err
				}
				state = readFileState(artifactPath)
				if state.Kind == FileValidJSON || state.Kind == FileMissing {
					break
				}
			}
		}

		if state.Kind == FileValidJSON {
			schemaCheck := input.Schema.SafeParse(state.Data)
			if !schemaCheck.Success() && schemaFixBudget > 0 {
				for schemaAttempt := 1; schemaAttempt <= schemaFixBudget; schemaAttempt++ {
					fix := BuildSchemaFixReminder(SchemaFixReminderArgs{
						OutputPath:   artifactPath,
						SchemaErrors: FormatSchemaErrors(schemaCheck.Issues),
						Attempt:      schemaAttempt, Budget: schemaFixBudget, Label: label,
					})
					fixRequest := request
					fixRequest.MessageID = deps.NewID("message")
					fixRequest.TaskPrompt = ""
					fixRequest.Reminder = fix
					fixRequest.Phase = PhaseSchemaFix
					fixRequest.Attempt = schemaAttempt
					fixRequest.BetweenStepReminder = nil
					invokeErr = invokeClient(
						ctx, deps.Client, fixRequest,
						durationMS(min64(timeoutMS, fixTimeoutMS)), deps.TimeoutContext,
					)
					if result, err, stop := abortUnsafeArtifact(invokeErr); stop {
						return result, err
					}
					if err := adoptConfiguredArtifact(input.OutputPath, artifactPath); err != nil {
						return zero, err
					}
					state = readFileState(artifactPath)
					if state.Kind != FileValidJSON {
						break
					}
					schemaCheck = input.Schema.SafeParse(state.Data)
					if schemaCheck.Success() {
						break
					}
				}
			}

			if schemaCheck.Success() {
				if input.PreserveOnSuccess {
					if err := publishArtifact(artifactPath, input.OutputPath); err != nil {
						return zero, err
					}
				} else {
					safeUnlink(artifactPath)
				}
				return Result[T]{
					Data: schemaCheck.Data, FirstTry: attempts == 1,
					UsedFallback: false,
				}, nil
			}
			lastParseError = CompactSchemaErrors(schemaCheck.Issues)
			safeUnlink(artifactPath)
			continue
		}

		switch state.Kind {
		case FileMissing:
			lastParseError = "no output file at " + artifactPath
		case FileEmpty:
			lastParseError = "output file empty at " + artifactPath
		default:
			lastParseError = "parse error after " + intString(parseFixBudget) +
				" fix turn(s): " + state.Error
		}
		safeUnlink(artifactPath)
	}

	if input.Fallback != nil {
		return fallbackResult(*input.Fallback), nil
	}
	return zero, fmt.Errorf(
		"agent-json: %s failed after %d attempts; last error: %s",
		input.Agent, attempts, lastParseError,
	)
}

func privateArtifactPath(outputPath string) (string, error) {
	file, err := os.CreateTemp(
		filepath.Dir(outputPath), "."+filepath.Base(outputPath)+".agentjson-*",
	)
	if err != nil {
		return "", fmt.Errorf("agent-json: reserve private artifact: %w", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		safeUnlink(path)
		return "", fmt.Errorf("agent-json: close private artifact: %w", err)
	}
	// The JSON contract distinguishes missing from empty. Reserve a random name
	// with CreateTemp, then restore the missing initial state before prompting.
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("agent-json: initialize private artifact: %w", err)
	}
	return path, nil
}

func publishArtifact(privatePath, outputPath string) error {
	// Both paths share a directory, so rename is the atomic ownership transfer
	// from a validated private artifact to the caller-visible preserved path.
	safeUnlink(outputPath)
	if err := os.Rename(privatePath, outputPath); err != nil {
		return fmt.Errorf("agent-json: preserve validated artifact: %w", err)
	}
	return nil
}

func adoptConfiguredArtifact(outputPath, privatePath string) error {
	if outputPath == privatePath {
		return nil
	}
	if _, err := os.Lstat(outputPath); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("agent-json: inspect configured artifact: %w", err)
	}
	// Some embedders written against TS src/session/agent-json.ts:563 still
	// publish to input.outputPath. Adoption is safe only after this invocation
	// has joined; artifactUnsafeError returns above without touching either path.
	safeUnlink(privatePath)
	if err := os.Rename(outputPath, privatePath); err != nil {
		return fmt.Errorf("agent-json: adopt configured artifact: %w", err)
	}
	return nil
}

func MergeTools(overrides []ToolSetting) ToolSettings {
	out := ToolSettings{
		{Name: "edit", Enabled: false},
		{Name: "apply_patch", Enabled: false},
		{Name: "task", Enabled: false},
		{Name: "plandb", Enabled: false},
		{Name: "todowrite", Enabled: false},
	}
	index := map[string]int{}
	for i, item := range out {
		index[item.Name] = i
	}
	for _, item := range overrides {
		if at, ok := index[item.Name]; ok {
			out[at].Enabled = item.Enabled
			continue
		}
		index[item.Name] = len(out)
		out = append(out, item)
	}
	return out
}

func SplitModelID(id string) Model { return splitModel(id) }

func splitModel(id string) Model {
	parts := strings.Split(id, "/")
	return Model{ModelID: strings.Join(parts[1:], "/"), ProviderID: parts[0]}
}

func fallbackResult[T any](value T) Result[T] {
	return Result[T]{Data: value, FirstTry: false, UsedFallback: true}
}

func newOutputWatcher(path string) func() string {
	lastErrorReported := ""
	return func() string {
		state := readFileState(path)
		switch state.Kind {
		case FileValidJSON:
			lastErrorReported = ""
		case FileParseError:
			if state.Error == lastErrorReported {
				return ""
			}
			lastErrorReported = state.Error
			return BuildWatcherReminder(path, state.Error)
		}
		return ""
	}
}

func invokeClient(
	ctx context.Context,
	client Client,
	request Request,
	timeout time.Duration,
	timeoutContext TimeoutContextFunc,
) (err error) {
	if client == nil {
		return errors.New("agent-json: nil client")
	}
	if timeoutContext == nil {
		timeoutContext = context.WithTimeout
	}
	callCtx, cancel := timeoutContext(ctx, timeout)
	defer cancel()
	if err := callCtx.Err(); err != nil {
		return err
	}
	select {
	case activeClientSlots <- struct{}{}:
	case <-callCtx.Done():
		return callCtx.Err()
	}
	invocation := &clientInvocation{done: make(chan struct{}), artifactPath: request.OutputPath}
	go func() {
		var runErr error
		defer func() {
			if recovered := recover(); recovered != nil {
				runErr = fmt.Errorf("%v\n%s", recovered, debug.Stack())
			}
			invocation.finish(runErr)
			<-activeClientSlots
		}()
		runErr = client.Run(callCtx, request)
	}()
	select {
	case <-invocation.done:
		return invocation.result()
	case <-callCtx.Done():
	}
	timeoutCause := callCtx.Err()

	// agent-json.ts:523-563 awaits the timeout-wrapped prompt before inspecting
	// its artifact. Give a cancelled client time to finish that ownership
	// transfer; after the bound, the caller must leave the artifact untouched.
	graceWindow := clientStopGrace(timeout)
	grace := time.NewTimer(graceWindow)
	defer grace.Stop()
	select {
	case <-invocation.done:
		return invocation.result()
	case <-grace.C:
		if runErr, finished := invocation.markOrphaned(); finished {
			return runErr
		}
		return &artifactUnsafeError{cause: timeoutCause, grace: graceWindow}
	}
}

type clientInvocation struct {
	mu           sync.Mutex
	done         chan struct{}
	artifactPath string
	err          error
	finished     bool
	orphaned     bool
}

func (invocation *clientInvocation) finish(err error) {
	invocation.mu.Lock()
	invocation.err = err
	invocation.finished = true
	if invocation.orphaned {
		// The client retained exclusive ownership past the caller's deadline.
		// Discard anything it eventually published before making the orphan no
		// longer observable.
		safeUnlink(invocation.artifactPath)
		orphanedClients.Add(-1)
	}
	close(invocation.done)
	invocation.mu.Unlock()
}

func (invocation *clientInvocation) result() error {
	invocation.mu.Lock()
	defer invocation.mu.Unlock()
	return invocation.err
}

func (invocation *clientInvocation) markOrphaned() (error, bool) {
	invocation.mu.Lock()
	defer invocation.mu.Unlock()
	if invocation.finished {
		return invocation.err, true
	}
	invocation.orphaned = true
	orphanedClients.Add(1)
	return nil, false
}

func durationMS(ms int64) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func min64(a int64, b int) int64 {
	if a < int64(b) {
		return a
	}
	return int64(b)
}

func cloneTools(input ToolSettings) ToolSettings {
	return append(ToolSettings(nil), input...)
}

var idCounter atomic.Uint64

func nextID(prefix string) string {
	return fmt.Sprintf("%s_%016x", prefix, idCounter.Add(1))
}
