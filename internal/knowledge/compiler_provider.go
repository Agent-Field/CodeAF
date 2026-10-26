package knowledge

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
	"net/http"
	"strings"
	"time"
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
	reasoning := document{"effort": "none"}
	requestID := identity([]any{m.model, "knowledge-3", prompt, payload})
	requestID = identity([]any{requestID, reasoning})
	requestID = identity([]any{requestID, document{"max_tokens": 8000}})
	var cached string
	cacheErr := m.db.QueryRow("SELECT payload FROM receipts WHERE id=?", requestID).Scan(&cached)
	if cacheErr == nil {
		var prior document
		if err := json.Unmarshal([]byte(cached), &prior); err != nil {
			return nil, err
		}
		if textValue(prior["model"]) != m.model {
			return nil, errors.New("knowledge cached model does not match selected model")
		}
		result := object(prior["result"])
		if err := validateCompilerResponse(role, result); err != nil {
			return nil, err
		}
		return result, nil
	} else if cacheErr != sql.ErrNoRows {
		return nil, cacheErr
	}
	if m.key == "" {
		return nil, errors.New("knowledge compilation requires an OpenRouter API key")
	}
	client, err := provider.NewClient(provider.Config{APIKey: m.key, BaseURL: config.DefaultBaseURL, Model: m.model, Timeout: 240 * time.Second, HTTPClient: http.DefaultClient, RouteGate: func(ctx context.Context, model, tag string) error {
		if model != m.model {
			return fmt.Errorf("knowledge requested model %q; refusing fallback %q", m.model, model)
		}
		return nil
	}})
	if err != nil {
		return nil, err
	}
	served := &provider.ServedEndpoint{}
	ctx := provider.WithRole(m.ctx, lanes.RoleMemory)
	ctx = provider.WithServedEndpoint(ctx, served)
	ctx = provider.WithCallTag(ctx, "knowledge."+role)
	ctx = provider.WithRequiredReasoningEffort(ctx, provider.Effort("none"))
	messages := []ai.Message{{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: prompt}}}, {Role: "user", Content: []ai.ContentPart{{Type: "text", Text: mustJSON(payload)}}}}
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
		return nil, errors.New("knowledge provider response incomplete; cached progress retained")
	}
	var result document
	if err = json.Unmarshal([]byte(textValue(object(choices[0]["message"])["content"])), &result); err != nil || result == nil {
		return nil, errors.New("knowledge provider returned invalid result JSON")
	}
	if err = validateCompilerResponse(role, result); err != nil {
		return nil, err
	}
	receipt := document{"model": m.model, "role": role, "request_id": requestID, "provider": data["provider"], "reasoning_requested": reasoning, "max_tokens": 8000, "generation_id": data["id"], "usage": data["usage"], "transport": "provider_funnel", "seconds": time.Since(started).Seconds(), "result": result}
	if _, err = m.db.Exec("INSERT OR IGNORE INTO receipts(id,model,role,cost,payload) VALUES(?,?,?,?,?)", requestID, m.model, role, number(object(data["usage"])["cost"]), mustJSON(receipt)); err != nil {
		return nil, err
	}
	return result, nil
}
func mustJSON(value any) string { b, _ := json.Marshal(value); return string(b) }
