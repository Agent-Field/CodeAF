package codexauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Model is one model the signed-in account may choose, with how many tokens it
// accepts and the reasoning levels the request translator is allowed to send
// for it.
type Model struct {
	ID string `json:"id"`
	// ContextLength is the listing's own `context_window`, zero when the row
	// carried none. It is what the session compacts against and what the status
	// line's meter is a share of, so a Codex model that dropped it kept whatever
	// window the conversation's previous model had (#1383).
	ContextLength   int      `json:"context_length,omitempty"`
	ReasoningLevels []string `json:"reasoning_levels,omitempty"`
}

// FallbackContextWindow is the window the account's own model list gave every
// visible model when it was last observed (2026-09-21, a Pro account:
// `"context_window": 272000` on each row). It is ONE figure because the
// fallback rows below are one observation, and a row that carried no window
// would leave a conversation that moved onto it compacting at its previous
// model's figure.
const FallbackContextWindow = 272000

// FallbackModels is the last observed public list used when a fresh account
// listing cannot be reached during connection.
var FallbackModels = []Model{
	{ID: "gpt-5.5", ContextLength: FallbackContextWindow},
	{ID: "gpt-5.6-sol", ContextLength: FallbackContextWindow},
	{ID: "gpt-5.6-terra", ContextLength: FallbackContextWindow},
	{ID: "gpt-5.6-luna", ContextLength: FallbackContextWindow},
}

// List asks the account's own backend which models are visible and remembers
// the reasoning levels needed by later turns.
func List(ctx context.Context, profileDir string, options Options) ([]Model, error) {
	if _, bounded := ctx.Deadline(); !bounded {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}
	endpoint := options.backend() + "/models?client_version=" + clientVersion
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := ClientWithOptions(profileDir, options).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return nil, fmt.Errorf("codex model list answered %s", response.Status)
	}
	var answer struct {
		Models []struct {
			Slug          string `json:"slug"`
			Visibility    string `json:"visibility"`
			ContextWindow int    `json:"context_window"`
			Levels        []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&answer); err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(answer.Models))
	for _, row := range answer.Models {
		if row.Visibility != "list" || strings.TrimSpace(row.Slug) == "" {
			continue
		}
		model := Model{ID: strings.TrimSpace(row.Slug), ContextLength: max(row.ContextWindow, 0)}
		for _, level := range row.Levels {
			if effort := strings.TrimSpace(level.Effort); effort != "" {
				model.ReasoningLevels = append(model.ReasoningLevels, effort)
			}
		}
		models = append(models, model)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("codex model list carried no visible models")
	}
	if err := saveModels(profileDir, models); err != nil {
		return nil, err
	}
	return models, nil
}

func saveModels(profileDir string, models []Model) error {
	raw, err := json.Marshal(models)
	if err != nil {
		return err
	}
	path := ModelsPath(profileDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func loadModels(profileDir string) []Model {
	raw, err := os.ReadFile(ModelsPath(profileDir))
	if err != nil {
		return nil
	}
	var models []Model
	if json.Unmarshal(raw, &models) != nil {
		return nil
	}
	return models
}

func clampEffort(profileDir, model, effort string) string {
	effort = strings.TrimSpace(effort)
	if effort == "" {
		return ""
	}
	var supported []string
	for _, row := range loadModels(profileDir) {
		if strings.EqualFold(strings.TrimSpace(row.ID), strings.TrimSpace(model)) {
			supported = row.ReasoningLevels
			break
		}
	}
	if len(supported) == 0 {
		return effort
	}
	for _, level := range supported {
		if strings.EqualFold(level, effort) {
			return level
		}
	}
	order := map[string]int{"none": 0, "minimal": 1, "low": 2, "medium": 3, "high": 4, "xhigh": 5, "max": 6}
	want, known := order[strings.ToLower(effort)]
	if !known {
		return supported[0]
	}
	best, distance := supported[0], 100
	for _, level := range supported {
		position, ok := order[strings.ToLower(level)]
		if !ok {
			continue
		}
		delta := position - want
		if delta < 0 {
			delta = -delta
		}
		if delta < distance {
			best, distance = level, delta
		}
	}
	return best
}
