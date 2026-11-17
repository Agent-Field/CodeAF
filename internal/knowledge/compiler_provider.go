package knowledge

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
)

//go:embed compiler_prompts.json
var compilerPromptJSON []byte

const (
	compilerAnswerTokens = 8000
	compilerRepeatGuard  = 2 * time.Second
)

var compilerPrompts = func() map[string]string {
	var prompts map[string]string
	if err := json.Unmarshal(compilerPromptJSON, &prompts); err != nil {
		panic(err)
	}
	return prompts
}()

type compilerModel struct {
	// Explicit reads may answer a question while capture is paused, never compile new knowledge.
	explicitRead     bool
	ctx              context.Context
	root, key, model string
	db               *sql.DB
	calls            func(context.Context) (context.Context, error)
	repeats          *compilerRepeats
}

// compilerRepeats keeps paid failures in memory so deliberate retries can proceed
// after two seconds, below the build's five-second backoff and an owner's next pass.
// The persisted attempt count, rather than this guard, bounds retries on reopening.
type compilerRepeats struct {
	sync.Mutex
	failed map[string]compilerRepeat
}

type compilerRepeat struct {
	after  time.Time
	reason string
}

func (m compilerModel) call(role string, payload document) (result document, err error) {
	if m.explicitRead && role != "query" && role != "recall" {
		return nil, ErrDisabled
	}
	prompt, ok := compilerPrompts[role]
	if !ok {
		return nil, fmt.Errorf("unknown knowledge role %q", role)
	}
	if result, found, err := m.cached(role, payload); found || err != nil {
		return result, err
	}
	requestID := compilerRequestID(m.model, role, payload)
	reasoning := document{"enabled": false}
	if m.key == "" {
		return nil, errors.New("knowledge compilation requires an OpenRouter API key")
	}
	wire, err := newCompilerReferenceWire(role, payload)
	if err != nil {
		return nil, err
	}
	ctx, err := m.callContext(withLibraryActivity(m.ctx, role))
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	sources := modelsource.Set{}.OrDefault(m.key, config.DefaultBaseURL)
	settings := config.ClientConfigFor(sources, m.model)
	settings.Timeout = 240 * time.Second
	settings.HTTPClient = http.DefaultClient
	settings.RouteGate = func(ctx context.Context, model, tag string) error {
		if _, err := m.callContext(ctx); err != nil {
			return err
		}
		if model != m.model {
			return fmt.Errorf("knowledge requested model %q; refusing fallback %q", m.model, model)
		}
		return nil
	}
	client, err := provider.NewClient(settings)
	if err != nil {
		return nil, err
	}
	served := &provider.ServedEndpoint{}
	ctx = provider.WithRole(ctx, lanes.RoleMemory)
	ctx = memoryBilling(ctx)
	ctx = provider.WithCacheKey(ctx, "knowledge:"+identity([]any{m.root, m.model, role}))
	ctx = provider.WithServedEndpoint(ctx, served)
	ctx = provider.WithCallTag(ctx, string(lanes.RoleMemory))
	ctx = provider.WithRequiredReasoningEffort(ctx, provider.EffortOff)
	messages := []ai.Message{{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: prompt}}}, {Role: "user", Content: []ai.ContentPart{{Type: "text", Text: mustJSON(wire.payload)}}}}
	started := time.Now()
	var receiptsMu sync.Mutex
	var receiptIDs []string
	var receiptErr error
	record := provider.BillingSinkFrom(ctx)
	ctx = provider.WithBilling(ctx, func(b provider.Billed) {
		if reported := strings.TrimSpace(b.ReportedModel); reported != "" {
			b.Model = reported
		}
		if record != nil {
			record(b)
		}
		receiptsMu.Lock()
		defer receiptsMu.Unlock()
		id := identity([]any{requestID, started.UnixNano(), len(receiptIDs)})
		receiptIDs = append(receiptIDs, id)
		receipt := document{"started_at": started.UTC().Format(time.RFC3339Nano), "completed_at": time.Now().UTC().Format(time.RFC3339Nano), "model": b.Model, "role": role, "request_id": requestID, "reasoning_requested": reasoning, "max_tokens": compilerAnswerTokens, "usage": document{"cost": b.Cost, "prompt_tokens": b.PromptTokens, "completion_tokens": b.CompletionTokens}, "transport": "provider_funnel"}
		if _, writeErr := m.db.Exec("INSERT INTO receipts(id,model,role,cost,payload) VALUES(?,?,?,?,?)", id, b.Model, role, b.Cost, mustJSON(receipt)); writeErr != nil && receiptErr == nil {
			receiptErr = writeErr
		}
	})
	defer func() {
		receiptsMu.Lock()
		defer receiptsMu.Unlock()
		if receiptErr != nil && err == nil {
			err = receiptErr
		}
		if err != nil && len(receiptIDs) > 0 && m.repeats != nil {
			m.repeats.Lock()
			m.repeats.failed[requestID] = compilerRepeat{after: time.Now().Add(compilerRepeatGuard), reason: err.Error()}
			m.repeats.Unlock()
		}
	}()
	response, err := client.CompleteWithMessages(ctx, messages, ai.WithTemperature(0), ai.WithMaxTokens(compilerAnswerTokens), ai.WithJSONMode())
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	var data document
	if err = json.Unmarshal(encoded, &data); err != nil {
		return nil, err
	}
	data["provider"] = served.Name()
	if len(response.Choices) == 1 {
		var content strings.Builder
		for _, part := range response.Choices[0].Message.Content {
			if part.Type == "text" {
				content.WriteString(part.Text)
			}
		}
		object(items(data["choices"])[0]["message"])["content"] = content.String()
	}
	if textValue(data["model"]) != m.model {
		return nil, fmt.Errorf("knowledge provider recorded model %q; required %q; result rejected", textValue(data["model"]), m.model)
	}
	choices := items(data["choices"])
	if len(choices) != 1 || textValue(choices[0]["finish_reason"]) != "stop" {
		reason := "missing choice"
		if len(choices) == 1 {
			reason = textValue(choices[0]["finish_reason"])
		}
		return nil, fmt.Errorf("knowledge %s response incomplete (%s); cached progress retained", role, reason)
	}
	if err = json.Unmarshal([]byte(textValue(object(choices[0]["message"])["content"])), &result); err != nil || result == nil {
		return nil, errors.New("knowledge provider returned invalid result JSON")
	}
	if err = validateCompilerResponse(role, result); err != nil {
		return nil, err
	}
	if err = wire.decode(result); err != nil {
		return nil, err
	}
	if err = validateCompilerReferences(role, payload, result); err != nil {
		return nil, err
	}
	receipt := document{"started_at": started.UTC().Format(time.RFC3339Nano), "completed_at": time.Now().UTC().Format(time.RFC3339Nano), "model": m.model, "role": role, "request_id": requestID, "provider": data["provider"], "reasoning_requested": reasoning, "max_tokens": compilerAnswerTokens, "generation_id": data["id"], "usage": data["usage"], "transport": "provider_funnel", "seconds": time.Since(started).Seconds(), "result": result}
	receiptsMu.Lock()
	if len(receiptIDs) > 0 {
		// A usable answer becomes the cache entry without counting its bill twice.
		_, err = m.db.Exec("UPDATE receipts SET id=?,payload=? WHERE id=?", requestID, mustJSON(receipt), receiptIDs[len(receiptIDs)-1])
	} else {
		_, err = m.db.Exec("INSERT OR IGNORE INTO receipts(id,model,role,cost,payload) VALUES(?,?,?,?,?)", requestID, m.model, role, number(object(data["usage"])["cost"]), mustJSON(receipt))
	}
	receiptsMu.Unlock()
	if err != nil {
		return nil, err
	}
	return result, nil
}
func mustJSON(value any) string { b, _ := json.Marshal(value); return string(b) }

func compilerRequestID(model, role string, payload document) string {
	prompt := compilerPrompts[role]
	requestVersion := "knowledge-3"
	if role == "discover" || role == "organize_subjects" || role == "consolidate_subjects" {
		requestVersion = "knowledge-5"
	}
	if role == "discover" || role == "organize_subjects" {
		requestVersion = "knowledge-6"
	}
	requestID := identity([]any{model, requestVersion, prompt, payload})
	requestID = identity([]any{requestID, document{"effort": "none"}})
	requestID = identity([]any{requestID, document{"max_tokens": compilerAnswerTokens}})
	return requestID
}

func (m compilerModel) cached(role string, payload document) (document, bool, error) {
	var cached string
	cacheErr := m.db.QueryRow("SELECT payload FROM receipts WHERE id=?", compilerRequestID(m.model, role, payload)).Scan(&cached)
	if cacheErr == nil {
		var prior document
		if err := json.Unmarshal([]byte(cached), &prior); err != nil {
			return nil, false, err
		}
		if textValue(prior["model"]) != m.model {
			return nil, false, errors.New("knowledge cached model does not match selected model")
		}
		result := object(prior["result"])
		if err := validateCompilerResponse(role, result); err != nil {
			return nil, false, err
		}
		if err := validateCompilerReferences(role, payload, result); err != nil {
			return nil, false, err
		}
		return result, true, nil
	} else if cacheErr != sql.ErrNoRows {
		return nil, false, cacheErr
	}
	if m.repeats != nil {
		requestID := compilerRequestID(m.model, role, payload)
		m.repeats.Lock()
		retry := m.repeats.failed[requestID]
		if !time.Now().Before(retry.after) {
			delete(m.repeats.failed, requestID)
		}
		m.repeats.Unlock()
		if time.Now().Before(retry.after) {
			return nil, false, fmt.Errorf("knowledge retry paused: %s", retry.reason)
		}
	}
	return nil, false, nil
}

// callContext gates every memory model step, including recall and provider retries.
func (m compilerModel) callContext(ctx context.Context) (context.Context, error) {
	// A job's memory compiles on a context carried over from the turn that
	// wrote it. Its answer is a document, never a reply, so the chat's stream
	// must not hear it.
	ctx = provider.WithoutStream(ctx)
	if !m.explicitRead && !config.MemoryEnabledAt(filepath.Dir(m.root)) {
		return nil, ErrDisabled
	}
	if m.calls != nil {
		var err error
		ctx, err = m.calls(ctx)
		if err != nil {
			return nil, err
		}
	}
	if before := callsFrom(ctx).before; before != nil {
		if err := before(); err != nil {
			return nil, err
		}
	}
	return ctx, nil
}
