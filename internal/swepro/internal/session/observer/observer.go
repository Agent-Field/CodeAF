// Package observer ports src/session/observer.ts:1-831 (swe-pro 3b25a1a).
// It provides the feature-gated intervention sidecar, session projections,
// reminder injection, and a byte-stable NDJSON bus tap.
package observer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/bus"
	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/agentjson"
)

const (
	InterveneThreshold = 0.7
	ObserverMaxFirings = 3

	minAliveMS              = 10 * 60_000
	timeCheckIntervalMS     = 5 * 60_000
	writeStallMS            = 3 * 60_000
	repetitionThreshold     = 4
	recentToolCallLimit     = 10
	recentMessageLimit      = 3
	recentMessageChars      = 300
	toolInputCompressChars  = 100
	toolOutputCompressChars = 200
	sidecarPollInterval     = 30 * time.Second
)

type TriggerKind struct {
	Kind      string
	ElapsedMS float64
	StaleMS   float64
	File      string
	Count     int
}

func (t TriggerKind) MarshalJSON() ([]byte, error) {
	switch t.Kind {
	case "time":
		return jscompat.Stringify(struct {
			Kind      string  `json:"kind"`
			ElapsedMS float64 `json:"elapsedMs"`
		}{t.Kind, t.ElapsedMS})
	case "write-stall":
		return jscompat.Stringify(struct {
			Kind    string  `json:"kind"`
			StaleMS float64 `json:"staleMs"`
		}{t.Kind, t.StaleMS})
	case "repetition":
		return jscompat.Stringify(struct {
			Kind  string `json:"kind"`
			File  string `json:"file"`
			Count int    `json:"count"`
		}{t.Kind, t.File, t.Count})
	default:
		return jscompat.Stringify(struct {
			Kind string `json:"kind"`
		}{t.Kind})
	}
}

func (t *TriggerKind) UnmarshalJSON(raw []byte) error {
	var value struct {
		Kind      string  `json:"kind"`
		ElapsedMS float64 `json:"elapsedMs"`
		StaleMS   float64 `json:"staleMs"`
		File      string  `json:"file"`
		Count     int     `json:"count"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*t = TriggerKind(value)
	return nil
}

type TriggerInputs struct {
	Now               float64  `json:"now"`
	StartedAt         float64  `json:"startedAt"`
	Firings           int      `json:"firings"`
	LastEvalAt        float64  `json:"lastEvalAt"`
	ArtifactMtime     *float64 `json:"artifactMtime"`
	ArtifactCheckAt   float64  `json:"artifactCheckAt"`
	RecentToolTargets []string `json:"recentToolTargets"`
}

// EvaluateTriggers is observer.ts:201-255, including the unused
// ArtifactCheckAt input.
func EvaluateTriggers(input TriggerInputs) TriggerKind {
	if input.Firings >= ObserverMaxFirings {
		return TriggerKind{Kind: "ceiling"}
	}
	elapsed := input.Now - input.StartedAt
	if elapsed < minAliveMS {
		return TriggerKind{Kind: "none"}
	}
	if len(input.RecentToolTargets) >= repetitionThreshold {
		counts := make(map[string]int)
		order := make([]string, 0)
		for _, target := range input.RecentToolTargets {
			if target == "" {
				continue
			}
			if _, exists := counts[target]; !exists {
				order = append(order, target)
			}
			counts[target]++
		}
		for _, file := range order {
			if counts[file] >= repetitionThreshold {
				return TriggerKind{Kind: "repetition", File: file, Count: counts[file]}
			}
		}
	}
	if input.ArtifactMtime != nil {
		stale := input.Now - *input.ArtifactMtime
		if stale >= writeStallMS {
			return TriggerKind{Kind: "write-stall", StaleMS: stale}
		}
	}
	if input.Now-input.LastEvalAt >= timeCheckIntervalMS {
		return TriggerKind{Kind: "time", ElapsedMS: elapsed}
	}
	return TriggerKind{Kind: "none"}
}

type Verdict struct {
	Intervene  bool     `json:"intervene"`
	Reasoning  string   `json:"reasoning"`
	Reminder   *string  `json:"reminder"`
	Confidence *float64 `json:"confidence,omitempty"`
}

// VerdictSchema is ObserverVerdictSchema at observer.ts:130-138. Zod objects
// strip unknown keys by default, so this parser validates the known fields
// without rejecting extras.
type VerdictSchema struct{}

func (VerdictSchema) SafeParse(raw json.RawMessage) agentjson.Validation[Verdict] {
	invalid := func(path, message string) agentjson.Validation[Verdict] {
		return agentjson.Validation[Verdict]{Issues: []agentjson.Issue{{
			Path: []string{path}, Message: message,
		}}}
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return invalid("(root)", "Expected object")
	}
	var verdict Verdict
	if value, ok := object["intervene"]; !ok || json.Unmarshal(value, &verdict.Intervene) != nil {
		return invalid("intervene", "Expected boolean")
	}
	if value, ok := object["reasoning"]; !ok || json.Unmarshal(value, &verdict.Reasoning) != nil {
		return invalid("reasoning", "Expected string")
	}
	reminder, ok := object["reminder"]
	if !ok {
		return invalid("reminder", "Expected string or null")
	}
	if string(reminder) != "null" {
		var text string
		if json.Unmarshal(reminder, &text) != nil {
			return invalid("reminder", "Expected string or null")
		}
		verdict.Reminder = &text
	}
	if confidence, exists := object["confidence"]; exists && string(confidence) != "null" {
		var value float64
		if json.Unmarshal(confidence, &value) != nil || value < 0 || value > 1 {
			return invalid("confidence", "Expected number between 0 and 1 or null")
		}
		verdict.Confidence = &value
	}
	return agentjson.Validation[Verdict]{Data: verdict}
}

type ObservedSession struct {
	SessionID       string
	AgentRole       string
	TaskSummary     string
	StartedAt       float64
	ArtifactPath    string
	Workspace       string
	ParentSessionID string
}

// Tracker is the common lifecycle seam used by the run, auditor, architect,
// and task-tool call sites. Registry implements it directly.
type Tracker interface {
	Track(ObservedSession)
	Untrack(sessionID string)
}

type PriorIntervention struct {
	At        float64
	Reasoning string
}

type RecentToolCall struct {
	Tool   string `json:"tool"`
	Input  string `json:"input"`
	Output string `json:"output"`
	Status string `json:"status"`
}

type RecentActivity struct {
	ToolCalls         []RecentToolCall
	ToolTargets       []string
	AssistantMessages []string
	StepCount         int
}

type ObserverContext struct {
	AgentRole          string                `json:"agent_role"`
	TaskSummary        string                `json:"task_summary"`
	ElapsedMinutes     float64               `json:"elapsed_minutes"`
	StepCount          int                   `json:"step_count"`
	RecentToolCalls    []RecentToolCall      `json:"recent_tool_calls"`
	RecentMessages     []string              `json:"recent_messages"`
	CurrentArtifact    string                `json:"current_artifact"`
	PriorInterventions []ContextIntervention `json:"prior_interventions"`
	Trigger            TriggerKind           `json:"trigger"`
}

type ContextIntervention struct {
	AtMinute  float64 `json:"at_minute"`
	Reasoning string  `json:"reasoning"`
}

type History interface {
	Messages(ctx context.Context, sessionID string) ([]msgmodel.WithParts, error)
}

type ReminderQueuer interface {
	QueueReminder(sessionID, text string)
}

type Dispatcher interface {
	Dispatch(ctx context.Context, observed ObservedSession, prompt, outputPath string) (Verdict, bool, error)
}

type DispatcherFunc func(context.Context, ObservedSession, string, string) (Verdict, bool, error)

func (f DispatcherFunc) Dispatch(ctx context.Context, observed ObservedSession, prompt, outputPath string) (Verdict, bool, error) {
	return f(ctx, observed, prompt, outputPath)
}

type Service struct {
	History    History
	Reminders  ReminderQueuer
	Dispatcher Dispatcher
	Now        func() time.Time
}

type FireInput struct {
	Observed           ObservedSession
	Trigger            TriggerKind
	PriorInterventions []PriorIntervention
	StartedAt          float64
	Override           func(ObserverContext) (Verdict, error)
}

type FireResult struct {
	Verdict    *Verdict `json:"verdict"`
	Intervened bool     `json:"intervened"`
	Reason     string   `json:"reason"`
}

func (s *Service) nowMS() float64 {
	if s.Now != nil {
		return float64(s.Now().UnixMilli())
	}
	return float64(time.Now().UnixMilli())
}

func (s *Service) Fire(ctx context.Context, input FireInput) (FireResult, error) {
	now := s.nowMS()
	recent := s.summarizeRecentActivity(ctx, input.Observed.SessionID)
	artifact := readArtifactIfAny(input.Observed.ArtifactPath)
	interventions := make([]ContextIntervention, 0, len(input.PriorInterventions))
	for _, prior := range input.PriorInterventions {
		interventions = append(interventions, ContextIntervention{
			AtMinute:  jsRound((prior.At - input.StartedAt) / 60_000),
			Reasoning: prior.Reasoning,
		})
	}
	view := ObserverContext{
		AgentRole: input.Observed.AgentRole, TaskSummary: input.Observed.TaskSummary,
		ElapsedMinutes: jsRound((now - input.StartedAt) / 60_000),
		StepCount:      recent.StepCount, RecentToolCalls: nonnilTools(recent.ToolCalls),
		RecentMessages: nonnilStrings(recent.AssistantMessages), CurrentArtifact: artifact,
		PriorInterventions: interventions, Trigger: input.Trigger,
	}
	var verdict Verdict
	var status string
	if input.Override != nil {
		var err error
		verdict, err = input.Override(view)
		if err != nil {
			return FireResult{}, err
		}
		status = "test-override"
	} else {
		if s.Dispatcher == nil {
			return FireResult{}, errors.New("observer: Dispatcher is required")
		}
		outputPath := filepath.Join(input.Observed.Workspace, ".codeaf", "observer",
			fmt.Sprintf("%s-%s.json", input.Observed.SessionID, jscompat.FormatNumber(now)))
		prompt, err := BuildObserverPrompt(view, outputPath)
		if err != nil {
			return FireResult{}, err
		}
		var fallback bool
		verdict, fallback, err = s.Dispatcher.Dispatch(ctx, input.Observed, prompt, outputPath)
		if err != nil {
			return FireResult{}, err
		}
		status = "ok"
		if fallback {
			status = "fallback"
		}
	}
	return s.applyVerdict(input.Observed, verdict, status)
}

func (s *Service) applyVerdict(observed ObservedSession, verdict Verdict, _ string) (FireResult, error) {
	confidence := 0.0
	if verdict.Confidence != nil {
		confidence = *verdict.Confidence
	}
	if !verdict.Intervene {
		return FireResult{Verdict: &verdict, Reason: "intervene=false"}, nil
	}
	if confidence < InterveneThreshold {
		return FireResult{Verdict: &verdict, Reason: fmt.Sprintf("confidence %s < threshold %s",
			jscompat.FormatNumber(confidence), jscompat.FormatNumber(InterveneThreshold))}, nil
	}
	if verdict.Reminder == nil || strings.TrimSpace(*verdict.Reminder) == "" {
		return FireResult{Verdict: &verdict, Reason: "intervene=true but empty reminder"}, nil
	}
	if s.Reminders == nil {
		return FireResult{}, errors.New("observer: ReminderQueuer is required")
	}
	s.Reminders.QueueReminder(observed.SessionID, strings.TrimSpace(*verdict.Reminder))
	return FireResult{Verdict: &verdict, Intervened: true, Reason: "queued"}, nil
}

func (s *Service) summarizeRecentActivity(ctx context.Context, sessionID string) RecentActivity {
	empty := RecentActivity{ToolCalls: []RecentToolCall{}, ToolTargets: []string{}, AssistantMessages: []string{}}
	if s.History == nil {
		return empty
	}
	items, err := s.History.Messages(ctx, sessionID)
	if err != nil {
		return empty
	}
	result := empty
	start := 0
	if len(items) > 40 {
		start = len(items) - 40
	}
	items = items[start:]
	for index := len(items) - 1; index >= 0; index-- {
		message := items[index]
		if message.Info.MessageRole() != "assistant" {
			continue
		}
		for _, part := range message.Parts {
			if part.PartType() == msgmodel.PartTypeStepFinish {
				result.StepCount++
			}
		}
		for partIndex := len(message.Parts) - 1; partIndex >= 0; partIndex-- {
			switch part := message.Parts[partIndex].(type) {
			case msgmodel.ToolPart:
				if len(result.ToolCalls) >= recentToolCallLimit {
					continue
				}
				call, target := projectTool(part)
				result.ToolCalls = append(result.ToolCalls, call)
				if target != "" {
					result.ToolTargets = append(result.ToolTargets, target)
				}
			case msgmodel.TextPart:
				if len(result.AssistantMessages) >= recentMessageLimit ||
					(part.Synthetic != nil && *part.Synthetic) || strings.TrimSpace(part.Text) == "" {
					continue
				}
				result.AssistantMessages = append(result.AssistantMessages, compact(part.Text, recentMessageChars))
			}
		}
		if len(result.ToolCalls) >= recentToolCallLimit && len(result.AssistantMessages) >= recentMessageLimit {
			break
		}
	}
	return result
}

func projectTool(part msgmodel.ToolPart) (RecentToolCall, string) {
	raw, _ := json.Marshal(part.State)
	var state struct {
		Status string          `json:"status"`
		Input  json.RawMessage `json:"input"`
		Output any             `json:"output"`
		Error  any             `json:"error"`
	}
	_ = json.Unmarshal(raw, &state)
	input := state.Input
	if len(input) == 0 || string(input) == "null" {
		input = json.RawMessage(`{}`)
	}
	output := state.Output
	if output == nil {
		output = state.Error
	}
	outputText := ""
	if output != nil {
		outputText = jsString(output)
	}
	target := ""
	var fields map[string]any
	if json.Unmarshal(input, &fields) == nil {
		for _, key := range []string{"file_path", "filePath", "path", "pattern", "command"} {
			if value, exists := fields[key]; exists && jsTruthy(value) {
				target = jsString(value)
				break
			}
		}
	}
	status := state.Status
	if status == "" {
		status = "unknown"
	}
	return RecentToolCall{
		Tool: part.Tool, Input: compact(string(input), toolInputCompressChars),
		Output: compact(outputText, toolOutputCompressChars), Status: status,
	}, target
}

func BuildObserverPrompt(view ObserverContext, outputPath string) (string, error) {
	block, err := jscompat.StringifyIndent(view)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{
		"# Observer evaluation",
		"",
		"Read the JSON below. Decide whether the observed sub-agent is in",
		"a doom-loop (intervene=true with a SPECIFIC reminder) or whether",
		"it's still making real progress (intervene=false). Strongly bias",
		"toward let_it_run.",
		"",
		"```json",
		string(block),
		"```",
		"",
		"Write your verdict as JSON to: " + outputPath,
	}, "\n"), nil
}

func readArtifactIfAny(path string) string {
	if path == "" {
		return "n/a"
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "n/a (artifact not yet written)"
	}
	if err != nil {
		return "n/a (read error: " + compact(err.Error(), 80) + ")"
	}
	return compact(string(raw), 4_000)
}

func artifactMtime(path string) *float64 {
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	value := float64(info.ModTime().UnixNano()) / float64(time.Millisecond)
	return &value
}

type runtimeState struct {
	Firings             int
	LastEvalAt          float64
	LastArtifactMtime   *float64
	LastArtifactCheckAt float64
	PriorInterventions  []PriorIntervention
}

type Supervisor struct {
	Observed ObservedSession
	Service  *Service
	mu       sync.Mutex
	state    runtimeState
}

type TickOptions struct {
	Now      *float64
	Override func(ObserverContext) (Verdict, error)
}

type TickResult struct {
	Fired   bool
	Trigger TriggerKind
	Result  *FireResult
}

func NewSupervisor(observed ObservedSession, service *Service) *Supervisor {
	return &Supervisor{Observed: observed, Service: service}
}

func (s *Supervisor) Tick(ctx context.Context, opts TickOptions) (TickResult, error) {
	if s.Service == nil {
		return TickResult{}, errors.New("observer: Service is required")
	}
	now := s.Service.nowMS()
	if opts.Now != nil {
		now = *opts.Now
	}
	recent := s.Service.summarizeRecentActivity(ctx, s.Observed.SessionID)
	mtime := artifactMtime(s.Observed.ArtifactPath)
	s.mu.Lock()
	if mtime != nil {
		s.state.LastArtifactMtime = mtime
	}
	s.state.LastArtifactCheckAt = now
	trigger := EvaluateTriggers(TriggerInputs{
		Now: now, StartedAt: s.Observed.StartedAt, Firings: s.state.Firings,
		LastEvalAt: s.state.LastEvalAt, ArtifactMtime: s.state.LastArtifactMtime,
		ArtifactCheckAt: s.state.LastArtifactCheckAt, RecentToolTargets: recent.ToolTargets,
	})
	if trigger.Kind == "none" || trigger.Kind == "ceiling" {
		s.mu.Unlock()
		return TickResult{Trigger: trigger}, nil
	}
	s.state.Firings++
	s.state.LastEvalAt = now
	prior := append([]PriorIntervention(nil), s.state.PriorInterventions...)
	s.mu.Unlock()

	result, err := s.Service.Fire(ctx, FireInput{
		Observed: s.Observed, Trigger: trigger, PriorInterventions: prior,
		StartedAt: s.Observed.StartedAt, Override: opts.Override,
	})
	if err != nil {
		return TickResult{}, err
	}
	if result.Intervened {
		reasoning := "(no reasoning)"
		if result.Verdict != nil {
			reasoning = result.Verdict.Reasoning
		}
		s.mu.Lock()
		s.state.PriorInterventions = append(s.state.PriorInterventions, PriorIntervention{At: now, Reasoning: reasoning})
		s.mu.Unlock()
	}
	return TickResult{Fired: true, Trigger: trigger, Result: &result}, nil
}

type Snapshot struct {
	Firings           int
	LastEvalAt        float64
	LastArtifactMtime *float64
	Interventions     int
}

func (s *Supervisor) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{s.state.Firings, s.state.LastEvalAt, s.state.LastArtifactMtime, len(s.state.PriorInterventions)}
}

type Registry struct {
	mu          sync.Mutex
	supervisors map[string]*Supervisor
	order       []string
	service     *Service
	interval    time.Duration
	cancel      context.CancelFunc
	stopped     bool
	polling     bool
}

func NewRegistry(service *Service) *Registry {
	return &Registry{supervisors: map[string]*Supervisor{}, service: service, interval: sidecarPollInterval}
}

func (r *Registry) Track(spec ObservedSession) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.supervisors[spec.SessionID]; !exists {
		r.supervisors[spec.SessionID] = NewSupervisor(spec, r.service)
		r.order = append(r.order, spec.SessionID)
	}
}

func (r *Registry) Untrack(sessionID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if _, exists := r.supervisors[sessionID]; exists {
		delete(r.supervisors, sessionID)
		for index, id := range r.order {
			if id == sessionID {
				r.order = append(r.order[:index], r.order[index+1:]...)
				break
			}
		}
	}
	r.mu.Unlock()
}

func (r *Registry) List() []*Supervisor {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Supervisor, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.supervisors[id])
	}
	return out
}

type PollResult struct {
	SessionID string
	Fired     bool
}

func (r *Registry) PollOnce(ctx context.Context, opts TickOptions) []PollResult {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return []PollResult{}
	}
	list := make([]*Supervisor, 0, len(r.order))
	for _, id := range r.order {
		list = append(list, r.supervisors[id])
	}
	r.mu.Unlock()
	out := make([]PollResult, 0, len(list))
	for _, supervisor := range list {
		result, err := supervisor.Tick(ctx, opts)
		out = append(out, PollResult{SessionID: supervisor.Observed.SessionID, Fired: err == nil && result.Fired})
	}
	return out
}

func (r *Registry) Start(ctx context.Context) {
	r.mu.Lock()
	if r.cancel != nil || r.stopped {
		r.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	interval := r.interval
	r.mu.Unlock()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.mu.Lock()
				if r.polling {
					r.mu.Unlock()
					continue
				}
				r.polling = true
				r.mu.Unlock()
				r.PollOnce(runCtx, TickOptions{})
				r.mu.Lock()
				r.polling = false
				r.mu.Unlock()
			case <-runCtx.Done():
				return
			}
		}
	}()
}

func (r *Registry) Stop() {
	r.mu.Lock()
	r.stopped = true
	cancel := r.cancel
	r.cancel = nil
	clear(r.supervisors)
	r.order = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func IsObserverEnabled() bool {
	value := os.Getenv("CODEAF_OBSERVER")
	return value == "1" || strings.ToLower(value) == "true"
}

var current struct {
	sync.RWMutex
	registry *Registry
}

func TryStart(ctx context.Context, service *Service) *Registry {
	if !IsObserverEnabled() {
		return nil
	}
	registry := NewRegistry(service)
	registry.Start(ctx)
	current.Lock()
	current.registry = registry
	current.Unlock()
	return registry
}

func Current() *Registry {
	current.RLock()
	defer current.RUnlock()
	return current.registry
}

func ClearCurrent() {
	current.Lock()
	current.registry = nil
	current.Unlock()
}

// Tap writes each bus payload as one compact JSON line. Field order is the
// bus payload literal order: id, type, properties.
type Tap struct {
	mu    sync.Mutex
	out   io.Writer
	unsub func()
}

func NewTap(source *bus.Bus, out io.Writer) *Tap {
	tap := &Tap{out: out}
	if source != nil {
		tap.unsub = source.SubscribeAllCallback(tap.write)
	}
	return tap
}

func (t *Tap) write(payload bus.Payload) {
	raw, err := jscompat.Stringify(payload)
	if err != nil {
		raw = []byte("null")
	}
	raw = append(raw, '\n')
	t.mu.Lock()
	if t.out != nil {
		_, _ = t.out.Write(raw)
	}
	t.mu.Unlock()
}

func (t *Tap) Close() {
	t.mu.Lock()
	unsub := t.unsub
	t.unsub = nil
	t.mu.Unlock()
	if unsub != nil {
		unsub()
	}
}

func compact(value string, limit int) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-3]) + "..."
}

func jsRound(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	return math.Floor(value + 0.5)
}

func jsString(value any) string {
	switch item := value.(type) {
	case string:
		return item
	case nil:
		return ""
	case float64:
		return jscompat.FormatNumber(item)
	case bool:
		if item {
			return "true"
		}
		return "false"
	default:
		raw, _ := jscompat.Stringify(item)
		return string(raw)
	}
}

func jsTruthy(value any) bool {
	switch item := value.(type) {
	case nil:
		return false
	case bool:
		return item
	case string:
		return item != ""
	case float64:
		return item != 0 && !math.IsNaN(item)
	default:
		return true
	}
}

func nonnilTools(value []RecentToolCall) []RecentToolCall {
	if value == nil {
		return []RecentToolCall{}
	}
	return value
}

func nonnilStrings(value []string) []string {
	if value == nil {
		return []string{}
	}
	return value
}
