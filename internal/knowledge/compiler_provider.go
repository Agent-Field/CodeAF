package knowledge

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
)

//go:embed compiler_prompts.json
var compilerPromptJSON []byte

var compilerPrompts = func() map[string]string {
	var prompts map[string]string
	if err := json.Unmarshal(compilerPromptJSON, &prompts); err != nil {
		panic(err)
	}
	return prompts
}()

type compilerModel struct {
	ctx              context.Context
	root, key, model string
	db               *sql.DB
}

func (m compilerModel) call(role string, payload document) (document, error) {
	prompt, ok := compilerPrompts[role]
	if !ok {
		return nil, fmt.Errorf("unknown knowledge role %q", role)
	}
	if result, found, err := m.cached(role, payload); found || err != nil {
		return result, err
	}
	requestID := compilerRequestID(m.model, role, payload)
	reasoning := document{"effort": "none"}
	if m.key == "" {
		return nil, errors.New("knowledge compilation requires an OpenRouter API key")
	}
	wire, err := newCompilerReferenceWire(role, payload)
	if err != nil {
		return nil, err
	}
	sources := modelsource.Set{}.OrDefault(m.key, config.DefaultBaseURL)
	settings := config.ClientConfigFor(sources, m.model)
	settings.Timeout = 240 * time.Second
	settings.HTTPClient = http.DefaultClient
	settings.RouteGate = func(ctx context.Context, model, tag string) error {
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
	ctx := provider.WithRole(m.ctx, lanes.RoleMemory)
	ctx = provider.WithCacheKey(ctx, "knowledge:"+identity([]any{m.root, m.model, role}))
	ctx = provider.WithServedEndpoint(ctx, served)
	ctx = provider.WithCallTag(ctx, "memory")
	ctx = provider.WithRequiredReasoningEffort(ctx, provider.Effort("none"))
	messages := []ai.Message{{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: prompt}}}, {Role: "user", Content: []ai.ContentPart{{Type: "text", Text: mustJSON(wire.payload)}}}}
	started := time.Now()
	response, err := client.CompleteWithMessages(ctx, messages, ai.WithTemperature(0), ai.WithMaxTokens(8000), ai.WithJSONMode())
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
	var result document
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
	receipt := document{"started_at": started.UTC().Format(time.RFC3339Nano), "completed_at": time.Now().UTC().Format(time.RFC3339Nano), "model": m.model, "role": role, "request_id": requestID, "provider": data["provider"], "reasoning_requested": reasoning, "max_tokens": 8000, "generation_id": data["id"], "usage": data["usage"], "transport": "provider_funnel", "seconds": time.Since(started).Seconds(), "result": result}
	if _, err = m.db.Exec("INSERT OR IGNORE INTO receipts(id,model,role,cost,payload) VALUES(?,?,?,?,?)", requestID, m.model, role, number(object(data["usage"])["cost"]), mustJSON(receipt)); err != nil {
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
	requestID = identity([]any{requestID, document{"max_tokens": 8000}})
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
	return nil, false, nil
}
