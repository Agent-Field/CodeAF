package knowledge

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	body, err := json.Marshal(document{"model": m.model, "messages": []document{{"role": "system", "content": prompt}, {"role": "user", "content": mustJSON(payload)}}, "temperature": 0, "max_tokens": 8000, "reasoning": reasoning, "response_format": document{"type": "json_object"}, "provider": document{"allow_fallbacks": true}})
	if err != nil {
		return nil, err
	}
	started := time.Now()
	var data document
	var attempt int
	for attempt = 0; attempt < 5; attempt++ {
		if err = m.ctx.Err(); err != nil {
			return nil, err
		}
		requestCtx, cancel := context.WithTimeout(m.ctx, 240*time.Second)
		request, e := http.NewRequestWithContext(requestCtx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
		if e != nil {
			cancel()
			return nil, e
		}
		request.Header.Set("Authorization", "Bearer "+m.key)
		request.Header.Set("Content-Type", "application/json")
		response, e := http.DefaultClient.Do(request)
		retry := false
		if e != nil {
			err = errors.New("knowledge provider connection failed; pending sources retained")
			retry = true
		} else {
			raw, readErr := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024+1))
			response.Body.Close()
			if readErr != nil {
				err = errors.New("knowledge provider response interrupted")
				retry = true
			} else if response.StatusCode != http.StatusOK {
				err = fmt.Errorf("knowledge provider returned HTTP %d; no model fallback used", response.StatusCode)
				retry = response.StatusCode == 429 || response.StatusCode == 502 || response.StatusCode == 503 || response.StatusCode == 504
			} else if len(raw) > 8*1024*1024 {
				err = errors.New("knowledge provider response exceeds 8 MiB")
			} else {
				err = json.Unmarshal(raw, &data)
				if err != nil {
					err = errors.New("knowledge provider returned invalid JSON")
				}
			}
		}
		cancel()
		if err == nil {
			break
		}
		if !retry || attempt == 4 {
			return nil, err
		}
		delay := min(40, 10<<attempt)
		timer := time.NewTimer(time.Duration(delay) * time.Second)
		select {
		case <-m.ctx.Done():
			timer.Stop()
			return nil, m.ctx.Err()
		case <-timer.C:
		}
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
	receipt := document{"model": m.model, "role": role, "request_id": requestID, "provider": data["provider"], "reasoning_requested": reasoning, "max_tokens": 8000, "generation_id": data["id"], "usage": data["usage"], "attempts": attempt + 1, "seconds": time.Since(started).Seconds(), "result": result}
	if _, err = m.db.Exec("INSERT OR IGNORE INTO receipts(id,model,role,cost,payload) VALUES(?,?,?,?,?)", requestID, m.model, role, number(object(data["usage"])["cost"]), mustJSON(receipt)); err != nil {
		return nil, err
	}
	return result, nil
}
func mustJSON(value any) string { b, _ := json.Marshal(value); return string(b) }
