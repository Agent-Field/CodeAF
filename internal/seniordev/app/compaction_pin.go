//go:build !windows

package app

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/calc"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/retrysched"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/overflow"
)

// The compaction budget is the model's advertised window (capped by
// capacity_tokens). When a request is routed to an endpoint that serves a
// smaller window than the catalog advertises, the provider rejects it and the
// step loop compacts (ResultCompact). That already recovers the run; what it
// does not do is remember the smaller limit, so the context grows back toward
// the window and can be rejected again on the same route. A pin records the
// limit the provider named, for the rest of the session, as a capacity_tokens
// minimum.
//
// Only a limit stated in the rejection text is pinned. A rejection that names
// no number is recorded as such and pins nothing: the compaction still
// happens, and a pin that cannot be justified from the error would be a
// silent behaviour change.

// overflowLimitPatterns are the context-overflow messages (see
// retrysched.contextOverflowPatterns) that carry the endpoint's limit.
var overflowLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)maximum context length is (\d+) tokens`),
	regexp.MustCompile(`(?i)maximum prompt length is (\d+)`),
	regexp.MustCompile(`(?i)context length is only (\d+) tokens`),
	regexp.MustCompile(`(?i)exceeds the limit of (\d+)`),
	regexp.MustCompile(`(?i)too large for model with (\d+) maximum context length`),
}

// parseContextLimit extracts the limit an overflow rejection names.
func parseContextLimit(text string) (float64, bool) {
	for _, pattern := range overflowLimitPatterns {
		if match := pattern.FindStringSubmatch(text); match != nil {
			if value, err := strconv.ParseFloat(match[1], 64); err == nil && value > 0 {
				return value, true
			}
		}
	}
	return 0, false
}

// overflowText joins every text a classified provider error carries: the
// message and, when present, the response body the provider sent.
func overflowText(err error) string {
	classified := retrysched.FromError(err)
	parts := []string{err.Error()}
	for _, extra := range []*string{classified.Data.Message, classified.Data.ResponseBody} {
		if extra == nil || *extra == "" {
			continue
		}
		duplicate := false
		for _, have := range parts {
			if strings.Contains(have, *extra) || strings.Contains(*extra, have) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			parts = append(parts, *extra)
		}
	}
	return strings.Join(parts, "\n")
}

// pinnedCapacityFor is the session's pinned capacity, if a rejection set one.
func (backend *modelAPIBackend) pinnedCapacityFor(sessionID string) (float64, bool) {
	if backend == nil {
		return 0, false
	}
	backend.pinMu.Lock()
	defer backend.pinMu.Unlock()
	value, ok := backend.pinnedCapacity[sessionID]
	return value, ok
}

// overflowConfigFor is the compaction config a session runs under: the project
// config, with a pinned capacity folded in as a minimum.
func (backend *modelAPIBackend) overflowConfigFor(sessionID string) (overflow.Config, error) {
	cfg, err := backend.config.overflowConfig()
	if err != nil {
		return cfg, err
	}
	return backend.withPinnedCapacity(cfg, sessionID), nil
}

// withPinnedCapacity folds the session's pin into a compaction config as a
// capacity_tokens minimum. Unpinned sessions get cfg back as is.
func (backend *modelAPIBackend) withPinnedCapacity(cfg overflow.Config, sessionID string) overflow.Config {
	pinned, ok := backend.pinnedCapacityFor(sessionID)
	if !ok {
		return cfg
	}
	block := overflow.CompactionConfig{}
	if cfg.Compaction != nil {
		block = *cfg.Compaction
	}
	if block.CapacityTokens == nil || *block.CapacityTokens > pinned {
		block.CapacityTokens = &pinned
	}
	cfg.Compaction = &block
	return cfg
}

// pinCapacityOnOverflow inspects a failed request. A context-overflow
// rejection that names a limit pins the session's capacity to that limit
// minus the output reservation; one that does not is recorded and pins
// nothing. Every path emits an event, so a pinned run is visible in the
// stream.
func (backend *modelAPIBackend) pinCapacityOnOverflow(
	sessionID, agent, providerID, modelID string, err error,
) {
	if backend == nil || err == nil {
		return
	}
	cfg, cfgErr := backend.config.overflowConfig()
	if cfgErr != nil {
		return
	}
	if !retrysched.IsContextOverflow(retrysched.FromError(err)) {
		return
	}
	text := overflowText(err)
	excerpt := text
	if len(excerpt) > 240 {
		excerpt = excerpt[:240]
	}
	data := map[string]any{
		"agent": agent, "session_id": sessionID,
		"provider_id": providerID, "model_id": modelID,
		"message": excerpt,
	}
	limit, ok := parseContextLimit(text)
	if !ok {
		data["source"] = "unparsed"
		backend.emitStage("compaction-capacity", "overflow-unpinned", data)
		return
	}
	reservation := float64(0)
	if _, model, projErr := (seniorDevModels{backend: backend, agent: agent}).projection(providerID, modelID); projErr == nil {
		reservation = calc.MaxOutputTokens(model)
	}
	if cfg.Compaction != nil && cfg.Compaction.Reserved != nil {
		reservation = *cfg.Compaction.Reserved
	}
	pinned := math.Floor(limit - reservation)
	if pinned <= 0 || math.IsNaN(pinned) || math.IsInf(pinned, 0) {
		data["source"] = "unparsed"
		data["limit_tokens"] = limit
		data["reason"] = "the named limit leaves no input space after the output reservation"
		backend.emitStage("compaction-capacity", "overflow-unpinned", data)
		return
	}
	backend.pinMu.Lock()
	if backend.pinnedCapacity == nil {
		backend.pinnedCapacity = map[string]float64{}
	}
	if previous, exists := backend.pinnedCapacity[sessionID]; exists && previous < pinned {
		pinned = previous
	}
	backend.pinnedCapacity[sessionID] = pinned
	backend.pinMu.Unlock()
	data["source"] = "parsed"
	data["limit_tokens"] = limit
	data["reservation_tokens"] = reservation
	data["pinned_capacity_tokens"] = pinned
	if pinnedCfg, err := backend.overflowConfigFor(sessionID); err == nil {
		if _, model, projErr := (seniorDevModels{backend: backend, agent: agent}).projection(providerID, modelID); projErr == nil {
			marks := overflow.Watermarks(overflow.UsableInput{Cfg: pinnedCfg, Model: model})
			data["capacity_tokens"] = marks.Capacity
			data["high_tokens"] = marks.High
			data["low_tokens"] = marks.Low
		}
	}
	backend.emitStage("compaction-capacity", "pinned", data)
}

func (backend *modelAPIBackend) emitStage(stage, status string, data map[string]any) {
	if backend == nil || backend.events == nil {
		return
	}
	backend.events.stage(stage, status, data)
}
