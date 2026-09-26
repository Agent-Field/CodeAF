//go:build !windows

package app

import (
	"sort"
	"sync"

	"github.com/Agent-Field/codeaf/internal/seniordev/bus"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

// agentSummary aggregates per-agent model activity from the bus events the
// run already emits, so the terminal can report where the wall and the cost
// went without anyone re-deriving it from the raw event stream afterwards.
// Per-agent wall is a union of intervals over message.updated, computed the
// same way every time and read as one `agent-summary` stage event.
//
// Purely observational: it taps busEvent's existing write path, holds its own
// lock (never the writer's), and nothing reads it back into a prompt.
type agentSummary struct {
	mu       sync.Mutex
	messages map[string]agentMessage
}

type agentMessage struct {
	id        string
	sessionID string
	agent     string
	summary   bool
	upstream  string
	created   uint64
	completed uint64 // 0 until the turn finishes
	tokensIn  uint64
	tokensOut uint64
	reasoning uint64
	cacheRead uint64
	cost      float64
}

// Cache-miss attribution. A call whose prompt is the previous prompt plus one
// step should read nearly all of it from the provider cache; a call that reads
// well under that has lost the prefix. The first call after a compaction
// boundary is exempt (the prefix was rebuilt on purpose), as is any call too
// small for a miss to matter. The summary makes the miss count a field, and
// `cache_misses_after_upstream_switch` says how many of them coincided with
// OpenRouter changing the serving endpoint.
const (
	cacheMissMinPrompt = 4_096
	// cacheMissReadRatio: cache read below this fraction of the previous
	// prompt is a miss.
	cacheMissReadRatio = 0.6
)

func newAgentSummary() *agentSummary {
	return &agentSummary{messages: map[string]agentMessage{}}
}

// observeBus records assistant-message state. message.updated fires more than
// once per message (created, then completed with tokens), so the map keeps the
// LAST state per message ID and the rollup counts each message once.
//
// It returns the run's cumulative cost and whether THIS payload is the one
// that completed a message. Those two drive the `spend` record: a reader
// holding the run to a dollar limit needs a rising total during the run, and
// summing message.updated itself would double-count, since the same message
// arrives more than once.
func (summary *agentSummary) observeBus(value bus.Payload) (float64, bool) {
	if summary == nil || value.Type != msgmodel.EventMessageUpdated {
		return 0, false
	}
	var info msgmodel.Info
	switch properties := value.Properties.(type) {
	case msgmodel.UpdatedEvent:
		info = properties.Info
	case *msgmodel.UpdatedEvent:
		if properties != nil {
			info = properties.Info
		}
	default:
		return 0, false
	}
	var assistant *msgmodel.Assistant
	switch message := info.(type) {
	case msgmodel.Assistant:
		assistant = &message
	case *msgmodel.Assistant:
		assistant = message
	}
	if assistant == nil || assistant.ID == "" {
		return 0, false
	}
	record := agentMessage{
		id:        assistant.ID,
		sessionID: assistant.SessionID,
		agent:     assistant.Agent,
		summary:   assistant.Summary != nil && *assistant.Summary,
		upstream:  assistant.Upstream,
		created:   assistant.Time.Created,
		tokensIn:  assistant.Tokens.Input,
		tokensOut: assistant.Tokens.Output,
		reasoning: assistant.Tokens.Reasoning,
		cacheRead: assistant.Tokens.Cache.Read,
		cost:      float64(assistant.Cost),
	}
	if assistant.Time.Completed != nil {
		record.completed = *assistant.Time.Completed
	}
	summary.mu.Lock()
	previous, seen := summary.messages[assistant.ID]
	summary.messages[assistant.ID] = record
	completed := record.completed != 0 && (!seen || previous.completed == 0)
	total := 0.0
	for _, message := range summary.messages {
		total += message.cost
	}
	summary.mu.Unlock()
	return total, completed
}

// data rolls the per-message records up to one map per agent, with wall time
// as a union of that agent's [created, completed] intervals — concurrent
// sessions overlap, so a plain sum would overcount.
func (summary *agentSummary) data() map[string]any {
	if summary == nil {
		return nil
	}
	summary.mu.Lock()
	defer summary.mu.Unlock()
	type rollup struct {
		calls      int
		intervals  [][2]uint64
		tokensIn   uint64
		tokensOut  uint64
		reasoning  uint64
		cacheRead  uint64
		cost       float64
		misses     int
		missTokens uint64
		switchMiss int
		upstreams  map[string]int
	}
	byAgent := map[string]*rollup{}
	agentOf := func(message agentMessage) *rollup {
		agent := message.agent
		if agent == "" {
			agent = "(unattributed)"
		}
		roll := byAgent[agent]
		if roll == nil {
			roll = &rollup{upstreams: map[string]int{}}
			byAgent[agent] = roll
		}
		return roll
	}
	bySession := map[string][]agentMessage{}
	for _, message := range summary.messages {
		if message.completed == 0 || message.completed < message.created {
			continue
		}
		roll := agentOf(message)
		roll.calls++
		roll.intervals = append(roll.intervals, [2]uint64{message.created, message.completed})
		roll.tokensIn += message.tokensIn
		roll.tokensOut += message.tokensOut
		roll.reasoning += message.reasoning
		roll.cacheRead += message.cacheRead
		roll.cost += message.cost
		if message.upstream != "" {
			roll.upstreams[message.upstream]++
		}
		bySession[message.sessionID] = append(bySession[message.sessionID], message)
	}
	// Misses are a property of consecutive calls in one session, so they are
	// attributed on a per-session walk in call order.
	for _, calls := range bySession {
		sort.Slice(calls, func(i, j int) bool {
			if calls[i].created != calls[j].created {
				return calls[i].created < calls[j].created
			}
			return calls[i].id < calls[j].id
		})
		var previous *agentMessage
		afterBoundary := false
		for index := range calls {
			call := calls[index]
			if call.summary {
				afterBoundary = true
				continue
			}
			prompt := call.tokensIn + call.cacheRead
			if previous != nil && !afterBoundary && prompt > cacheMissMinPrompt &&
				float64(call.cacheRead) < cacheMissReadRatio*float64(previous.tokensIn+previous.cacheRead) {
				roll := agentOf(call)
				roll.misses++
				roll.missTokens += call.tokensIn
				if call.upstream != "" && previous.upstream != "" && call.upstream != previous.upstream {
					roll.switchMiss++
				}
			}
			afterBoundary = false
			previous = &calls[index]
		}
	}
	if len(byAgent) == 0 {
		return nil
	}
	agents := map[string]any{}
	names := make([]string, 0, len(byAgent))
	for name := range byAgent {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		roll := byAgent[name]
		agents[name] = map[string]any{
			"calls":                              roll.calls,
			"wall_union_ms":                      unionMillis(roll.intervals),
			"tokens_in":                          roll.tokensIn,
			"tokens_out":                         roll.tokensOut,
			"tokens_reasoning":                   roll.reasoning,
			"cache_read":                         roll.cacheRead,
			"cost_usd":                           roll.cost,
			"cache_misses":                       roll.misses,
			"cache_miss_tokens_in":               roll.missTokens,
			"cache_misses_after_upstream_switch": roll.switchMiss,
			"upstreams":                          roll.upstreams,
		}
	}
	return map[string]any{"agents": agents}
}

func unionMillis(intervals [][2]uint64) uint64 {
	if len(intervals) == 0 {
		return 0
	}
	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i][0] < intervals[j][0]
	})
	var total, start, end uint64 = 0, intervals[0][0], intervals[0][1]
	for _, interval := range intervals[1:] {
		if interval[0] > end {
			total += end - start
			start, end = interval[0], interval[1]
		} else if interval[1] > end {
			end = interval[1]
		}
	}
	return total + end - start
}
