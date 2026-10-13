package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

const chatURL = "https://openrouter.ai/api/v1/chat/completions"

// completion is one model answer with what it cost and how long it took.
type completion struct {
	Text    string
	Cost    float64
	Elapsed time.Duration
}

type completer interface {
	complete(ctx context.Context, model, prompt string) (completion, error)
}

type openRouter struct {
	key    string
	client *http.Client
	think  bool
}

// newOpenRouter resolves the key the way the codeaf binary on this machine
// does. The key is never printed.
func newOpenRouter(think bool) (*openRouter, error) {
	dir := config.ProfileDir()
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, ".codeaf")
	}
	key := config.APIKeyAt(dir)
	if key == "" {
		return nil, fmt.Errorf("no OpenRouter key found in %s or OPENROUTER_API_KEY", dir)
	}
	return &openRouter{key: key, client: &http.Client{Timeout: 4 * time.Minute}, think: think}, nil
}

// complete asks with reasoning off; a model that insists on reasoning is asked
// again with the lowest effort it accepts.
func (o *openRouter) complete(ctx context.Context, model, prompt string) (completion, error) {
	out, err := o.once(ctx, o.request(model, prompt, false))
	if err != nil && strings.Contains(err.Error(), "Reasoning is mandatory") {
		retry, err2 := o.once(ctx, o.request(model, prompt, true))
		retry.Elapsed += out.Elapsed
		return retry, err2
	}
	return out, err
}

func (o *openRouter) once(ctx context.Context, request map[string]any) (completion, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return completion{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatURL, bytes.NewReader(body))
	if err != nil {
		return completion{}, err
	}
	req.Header.Set("Authorization", "Bearer "+o.key)
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := o.client.Do(req)
	if err != nil {
		return completion{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return completion{}, err
	}
	out, err := decodeReply(resp.StatusCode, raw)
	out.Elapsed = time.Since(start)
	return out, err
}

func (o *openRouter) request(model, prompt string, lowEffort bool) map[string]any {
	req := map[string]any{
		"model":       model,
		"messages":    []map[string]string{{"role": "user", "content": prompt}},
		"temperature": 0.2,
		"usage":       map[string]bool{"include": true},
	}
	switch {
	case lowEffort:
		req["reasoning"] = map[string]string{"effort": "low"}
	case !o.think:
		req["reasoning"] = map[string]bool{"enabled": false}
	}
	return req
}

func decodeReply(status int, raw []byte) (completion, error) {
	var reply struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
		Usage struct{ Cost float64 }    `json:"usage"`
		Error *struct{ Message string } `json:"error"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return completion{}, fmt.Errorf("openrouter %d: unreadable reply", status)
	}
	if reply.Error != nil {
		return completion{}, fmt.Errorf("openrouter %d: %s", status, reply.Error.Message)
	}
	if len(reply.Choices) == 0 {
		return completion{}, fmt.Errorf("openrouter %d: no choices", status)
	}
	return completion{Text: reply.Choices[0].Message.Content, Cost: reply.Usage.Cost}, nil
}
