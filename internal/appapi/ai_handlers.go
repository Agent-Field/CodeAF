package appapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
	validator "github.com/santhosh-tekuri/jsonschema/v5"
)

var aiContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/ai/call", Request: "AICallRequest", Response: "AICallResponse"}, handle: (*Server).aiHandler},
}

// AICallRuntime carries the engine's routing configuration, never an app model id.
// Tests replace the provider transport here and exercise the real token funnel.
type AICallRuntime struct {
	Source       roles.Source
	Default      string
	ClientConfig func(string) provider.Config
}
type aiRuntimeKey struct{}

// WithAICallRuntime supplies the owning engine's routing configuration.
func WithAICallRuntime(ctx context.Context, runtime AICallRuntime) context.Context {
	return context.WithValue(ctx, aiRuntimeKey{}, runtime)
}
func (s *Server) aiRuntime() (AICallRuntime, error) {
	if s.options.Context != nil {
		if runtime, ok := s.options.Context.Value(aiRuntimeKey{}).(AICallRuntime); ok {
			return runtime, nil
		}
	}
	settings, _, err := config.AppSettingsAt(s.options.ProfileDir)
	if err != nil {
		return AICallRuntime{}, err
	}
	pins, err := config.ParseModelRoles(config.ModelRolesAt(s.options.ProfileDir))
	if err != nil {
		return AICallRuntime{}, err
	}
	source := func(key string) (string, bool) {
		if strings.HasPrefix(key, "roles.") {
			value, ok := pins[strings.TrimPrefix(key, "roles.")]
			return value, ok
		}
		value := config.TierModelAt(s.options.ProfileDir, strings.TrimPrefix(key, "tiers."))
		return value, value != ""
	}
	fallback, _ := settings["models.default"].(string)
	cfg := config.Config{ProfileDir: s.options.ProfileDir, Sources: config.ResolveSources(s.options.ProfileDir, "", config.DefaultBaseURL), Models: catalog.Recall(catalog.Options{Dir: s.options.ProfileDir})}
	return AICallRuntime{Source: source, Default: fallback, ClientConfig: cfg.ClientConfig}, nil
}

func (s *Server) aiHandler(w http.ResponseWriter, r *http.Request) {
	s.aiIdempotent(s.aiAction)(w, r)
}
func (s *Server) aiAction(w http.ResponseWriter, r *http.Request) {
	var body AICallRequest
	if !decodeResourceObject(w, r, &body) {
		return
	}
	// The role is checked before credentials, ledger writes or any network work.
	tier, registered := roles.TierOf(roles.Role(body.Role))
	if !registered || !strings.HasPrefix(body.Role, "app.") {
		fail(w, 422, "not_allowed", "this app AI role is not registered")
		return
	}
	if body.Tier != "" && body.Tier != "reflex" && body.Tier != "low" && body.Tier != "worker" {
		fail(w, 422, "bad_input", "app tier must be reflex, low or worker")
		return
	}
	if body.Expect != nil {
		fail(w, 422, "bad_input", "AI calls have no mutable state or version to expect")
		return
	}
	budget, err := moneyValue(body.Budget)
	if err != nil || budget <= 0 || math.IsInf(budget, 0) {
		fail(w, 422, "bad_input", "AI calls need a positive budget in two-decimal USD")
		return
	}
	if body.MaxTokens < 1 || body.MaxTokens > 32768 || len(body.Messages) == 0 || len(body.Messages) > 128 {
		fail(w, 422, "bad_input", "AI calls need 1–128 messages and max_tokens from 1 to 32768")
		return
	}
	messages := make([]ai.Message, 0, len(body.Messages))
	for _, m := range body.Messages {
		if (m.Role != "system" && m.Role != "user" && m.Role != "assistant") || strings.TrimSpace(m.Content) == "" {
			fail(w, 422, "bad_input", "messages need system, user or assistant roles and nonempty content")
			return
		}
		messages = append(messages, ai.Message{Role: m.Role, Content: []ai.ContentPart{{Type: "text", Text: m.Content}}})
	}
	options := []ai.Option{ai.WithMaxTokens(body.MaxTokens)}
	var outputSchema *validator.Schema
	if len(body.JSONSchema) > 0 {
		var schema map[string]json.RawMessage
		if json.Unmarshal(body.JSONSchema, &schema) != nil || schema == nil {
			fail(w, 422, "bad_input", "json_schema must be an object")
			return
		}
		compiler := validator.NewCompiler()
		compiler.LoadURL = func(string) (io.ReadCloser, error) {
			return nil, errors.New("external schema references are unavailable")
		}
		if err := compiler.AddResource("app-response.json", bytes.NewReader(body.JSONSchema)); err == nil {
			outputSchema, err = compiler.Compile("app-response.json")
		}
		if outputSchema == nil {
			fail(w, 422, "bad_input", "json_schema must be valid and use only local references")
			return
		}
		options = append(options, func(req *ai.Request) error {
			req.ResponseFormat = &ai.ResponseFormat{Type: "json_schema", JSONSchema: &ai.JSONSchema{Name: "app_response", Strict: true, Schema: body.JSONSchema}}
			return nil
		})
	}
	runtime, err := s.aiRuntime()
	if err != nil || runtime.ClientConfig == nil {
		fail(w, 503, "not_allowed", "AI routing is unavailable")
		return
	}
	call, err := roles.ResolveAppCall(runtime.Source, roles.Role(body.Role), roles.Tier(body.Tier), runtime.Default)
	if err != nil {
		fail(w, 422, "bad_input", err.Error())
		return
	}
	if body.Tier != "" {
		tier = roles.Tier(body.Tier)
	}
	ctx, cancel := context.WithTimeout(r.Context(), roles.Patience(tier))
	defer cancel()
	if s.options.Context != nil {
		stop := context.AfterFunc(s.options.Context, cancel)
		defer stop()
	}
	ctx = provider.WithRole(ctx, lane.RoleAuxiliary)
	ctx = provider.WithCallTag(ctx, string(lane.RoleAuxiliary)+":"+body.Role)
	ctx = provider.WithConfiguredReasoningEffort(ctx, provider.Effort(call.Effort))
	// Serialize app calls across launches sharing a home. Existing agent in-flight
	// accounting retains its own SpendGuard, as documented by the spending lane.
	lock, err := s.aiCallLock()
	if err != nil {
		fail(w, 409, "conflict", "another app AI call is running, or its ledger is unavailable")
		return
	}
	defer func() { _ = filelock.Unlock(lock); _ = lock.Close() }()
	ledger, err := s.openAIUsage()
	if err != nil {
		fail(w, 503, "not_allowed", "the AI spending ledger could not be opened")
		return
	}
	_ = ledger.Close()
	available, err := s.aiRemaining(ctx)
	if err != nil {
		resourceFailure(w, err)
		return
	}
	if available <= 0 {
		fail(w, 402, "at_cap", "today's spending reached the day cap")
		return
	}
	limit := math.Min(budget, available)
	id, err := Token()
	if err != nil {
		fail(w, 503, "not_allowed", "could not create an AI ledger id")
		return
	}
	id = "ai_" + id
	var mu sync.Mutex
	var reserved float64
	initialLimit := limit
	result := AICallResponse{Model: call.Model, LedgerID: id}
	var ledgerErr error
	var receipts int
	var unknownCost bool
	bank := func(b provider.Billed, reconciled bool) {
		mu.Lock()
		defer mu.Unlock()
		receipts++
		result.Usage.In += b.PromptTokens
		result.Usage.Out += b.CompletionTokens
		if b.Model != "" {
			result.Model = b.Model
		}
		if b.Cost < 0 || math.IsNaN(b.Cost) || math.IsInf(b.Cost, 0) {
			ledgerErr = errors.New("invalid provider cost")
			return
		}
		limit -= b.Cost
		now := s.resourceNow()
		line := session.UsageLine{ID: id, At: now, Day: now.Format("2006-01-02"), Model: b.Model, Role: body.Role, Seat: session.Seat(tier), Calls: 1, Input: b.PromptTokens, Output: b.CompletionTokens, USD: b.Cost, Reconciled: reconciled}
		if err := s.appendAIUsage(line); err != nil {
			ledgerErr = err
		}
	}
	ctx = provider.WithBilling(ctx, func(b provider.Billed) { bank(b, false) })
	ctx = provider.WithReconcile(ctx, func(b provider.Reconciled) {
		if b.Found {
			bank(b.Billed, true)
		} else {
			mu.Lock()
			unknownCost = true
			now := s.resourceNow()
			if err := s.appendAIUsage(session.UsageLine{ID: id, At: now, Day: now.Format("2006-01-02"), Model: b.Model, Role: body.Role, Calls: 1, Input: b.PromptTokens, Output: b.CompletionTokens, Unbilled: true}); err != nil {
				ledgerErr = err
			}
			mu.Unlock()
		}
	})
	ctx = provider.WithRequiredCostReceipt(ctx)
	ctx = provider.WithCostAdmission(ctx, func(estimate float64, known bool) error {
		mu.Lock()
		defer mu.Unlock()
		if !known {
			return agentError(503, "not_allowed", "this model has no published price or output ceiling for budget admission")
		}
		if ledgerErr != nil {
			return agentError(503, "not_allowed", "the AI spending receipt could not be saved")
		}
		if estimate > limit || reserved+estimate > initialLimit {
			return agentError(402, "at_cap", "the AI call would exceed its budget or the day cap")
		}
		reserved += estimate
		return nil
	})
	var pending sync.WaitGroup
	ctx = provider.WithReceiptPending(ctx, func() func() { pending.Add(1); return pending.Done })
	cfg := runtime.ClientConfig(call.String())
	oldGate := cfg.RouteGate
	cfg.RouteGate = func(gateCtx context.Context, model, tag string) error {
		if oldGate != nil {
			if err := oldGate(gateCtx, model, tag); err != nil {
				return err
			}
		}
		priced := runtime.ClientConfig(model)
		probe, err := provider.NewClient(priced)
		if err != nil {
			return err
		}
		estimate, known, err := probe.EstimateCompletionCost(gateCtx, messages, options...)
		if err != nil {
			return err
		}
		if !known {
			return agentError(503, "not_allowed", "this model has no published price for budget admission")
		}
		mu.Lock()
		left := limit
		fault := ledgerErr
		mu.Unlock()
		if fault != nil {
			return agentError(503, "not_allowed", "the AI spending receipt could not be saved")
		}
		if estimate > left || left <= 0 {
			return agentError(402, "at_cap", "the AI call would exceed its budget or the day cap")
		}
		return nil
	}
	// Refuse before committing SSE headers so ordinary cap errors remain HTTP 402.
	if err = cfg.RouteGate(ctx, call.Model, body.Role); err != nil {
		resourceFailure(w, err)
		return
	}
	client, err := provider.NewClient(cfg)
	if err != nil {
		fail(w, 503, "not_allowed", redact.Secrets(err.Error()))
		return
	}
	streamed := false
	emit := func(event string, value any) {
		raw, _ := json.Marshal(value)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
	if body.Stream {
		if _, ok := w.(http.Flusher); !ok {
			fail(w, 503, "not_allowed", "streaming is unavailable")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		streamed = true
		ctx = provider.WithStreamObserver(ctx, func(event provider.StreamEvent) {
			if event.Kind == provider.StreamDelta {
				emit("ai.delta", AIDeltaEvent{Text: event.Delta})
			}
		})
	}
	response, callErr := client.CompleteWithMessages(ctx, messages, options...)
	pending.Wait()
	mu.Lock()
	spent := math.Min(budget, available) - limit
	result.Cost = Money{USD: fmt.Sprintf("%.8f", spent)}
	if response != nil {
		result.Text = response.Text()
	}
	fault := ledgerErr
	measured := receipts > 0
	unknown := unknownCost
	mu.Unlock()
	if callErr == nil && (!measured || unknown) {
		callErr = agentError(502, "not_allowed", "the provider supplied no usage receipt; its cost is unknown")
	}
	if callErr == nil && fault != nil {
		callErr = agentError(503, "not_allowed", "the AI spending receipt could not be saved")
	}
	if callErr == nil && len(body.JSONSchema) > 0 {
		var value any
		if json.Unmarshal([]byte(result.Text), &value) != nil || outputSchema.Validate(value) != nil {
			callErr = agentError(502, "bad_input", "the model returned JSON that does not match the requested schema")
		} else {
			result.JSON = json.RawMessage(result.Text)
		}
	}
	if callErr != nil {
		if streamed {
			emit("error", map[string]string{"error": redact.Secrets(callErr.Error()), "ledger_id": id})
		} else {
			var resource *agentReplyError
			if errors.As(callErr, &resource) {
				resourceFailure(w, callErr)
			} else {
				fail(w, 502, "not_allowed", redact.Secrets(callErr.Error()))
			}
		}
		return
	}
	if streamed {
		emit("ai.done", result)
	} else {
		write(w, 200, result)
	}
}

func (s *Server) aiRemaining(ctx context.Context) (float64, error) {
	rows, err := s.spendingBackend().SpendingRows(ctx)
	if err != nil {
		return 0, err
	}
	money, err := s.spendingBackend().SpendingCap(ctx, "day", "")
	if err != nil {
		return 0, err
	}
	cap, err := moneyValue(money)
	if err != nil {
		return 0, err
	}
	if cap == 0 {
		return math.Inf(1), nil
	}
	now := s.resourceNow()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, row := range rows {
		if !row.At.Before(start) && !row.At.After(now) {
			cost, err := measuredCost(row)
			if err != nil {
				return 0, err
			}
			cap -= cost
		}
	}
	return cap, nil
}
func (s *Server) aiCallLock() (*os.File, error) {
	dir := filepath.Join(s.resourceHome(), "v3", "app")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "ai.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = filelock.Lock(f, true, true); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func (s *Server) appendAIUsage(line session.UsageLine) error {
	f, err := s.openAIUsage()
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := json.Marshal(line)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(raw, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func (s *Server) openAIUsage() (*os.File, error) {
	return os.OpenFile(filepath.Join(s.resourceHome(), "v3", session.UsageLedgerName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
}
