//go:build !windows

package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/bus"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

func assistantPayload(id, agent string, created, completed uint64, tokens uint64, cost float64) bus.Payload {
	message := msgmodel.Assistant{
		Role: "assistant", Agent: agent,
		Time:   msgmodel.AssistantTime{Created: created},
		Tokens: msgmodel.Tokens{Input: tokens, Output: tokens / 10},
		Cost:   float64(cost),
	}
	message.ID = id
	if completed > 0 {
		message.Time.Completed = &completed
	}
	return bus.Payload{
		Type:       msgmodel.EventMessageUpdated,
		Properties: msgmodel.UpdatedEvent{SessionID: "ses", Info: message},
	}
}

func summaryAgents(t *testing.T, summary *agentSummary) map[string]any {
	t.Helper()
	data := summary.data()
	if data == nil {
		t.Fatal("summary is empty")
	}
	agents, ok := data["agents"].(map[string]any)
	if !ok {
		t.Fatalf("data shape = %#v", data)
	}
	return agents
}

// message.updated fires more than once per message — created first, tokens on
// completion. The rollup must count each message once, at its final state.
func TestAgentSummaryDeduplicatesMessageUpdates(t *testing.T) {
	summary := newAgentSummary()
	summary.observeBus(assistantPayload("m1", "coder", 1000, 0, 0, 0))
	summary.observeBus(assistantPayload("m1", "coder", 1000, 5000, 400, 0.02))

	agents := summaryAgents(t, summary)
	coder, ok := agents["coder"].(map[string]any)
	if !ok {
		t.Fatalf("agents = %#v", agents)
	}
	if coder["calls"] != 1 || coder["tokens_in"] != uint64(400) ||
		coder["wall_union_ms"] != uint64(4000) {
		t.Fatalf("coder rollup = %#v", coder)
	}
}

// Concurrent sessions overlap; wall must be a union, never a sum. Two calls
// on [0,10s] and [5s,15s] are 15s of wall, not 20.
func TestAgentSummaryWallIsAUnionOfIntervals(t *testing.T) {
	summary := newAgentSummary()
	summary.observeBus(assistantPayload("m1", "coder", 0, 10_000, 100, 0.01))
	summary.observeBus(assistantPayload("m2", "coder", 5_000, 15_000, 100, 0.01))

	coder := summaryAgents(t, summary)["coder"].(map[string]any)
	if coder["calls"] != 2 || coder["wall_union_ms"] != uint64(15_000) {
		t.Fatalf("coder rollup = %#v", coder)
	}
}

// An in-flight message (no completed time) is an incomplete observation and
// must not enter the rollup — a hard-killed run's dangling turn would
// otherwise contribute a zero-length or negative interval.
func TestAgentSummaryIgnoresIncompleteMessages(t *testing.T) {
	summary := newAgentSummary()
	summary.observeBus(assistantPayload("m1", "compaction", 1000, 0, 0, 0))
	if summary.data() != nil {
		t.Fatalf("incomplete message entered the summary: %#v", summary.data())
	}
}

// The bus publishes the concrete UpdatedEvent; pointer forms and non-message
// events must be tolerated silently — the tap can never panic the writer.
func TestAgentSummaryToleratesForeignPayloads(t *testing.T) {
	summary := newAgentSummary()
	summary.observeBus(bus.Payload{Type: "session.created", Properties: map[string]any{}})
	summary.observeBus(bus.Payload{Type: msgmodel.EventMessageUpdated, Properties: "garbage"})
	event := assistantPayload("m1", "coder", 0, 1_000, 10, 0)
	pointerEvent := event
	updated := pointerEvent.Properties.(msgmodel.UpdatedEvent)
	pointerEvent.Properties = &updated
	summary.observeBus(pointerEvent)

	coder := summaryAgents(t, summary)["coder"].(map[string]any)
	if coder["calls"] != 1 {
		t.Fatalf("pointer payload not counted: %#v", coder)
	}
	var nilSummary *agentSummary
	nilSummary.observeBus(event) // must not panic
	if nilSummary.data() != nil {
		t.Fatal("nil summary produced data")
	}
}

// Wiring test: the busEvent tap must feed the summary, and the rollup must
// survive a round-trip through the real stage() encoder. The unit tests above
// exercise the aggregator directly; this one proves the writer is actually
// plumbed to it, which nothing short of a live run would otherwise check.
func TestBusEventTapFeedsTheEmittedSummary(t *testing.T) {
	output := &bytes.Buffer{}
	writer := newEventWriter(output)

	writer.busEvent(assistantPayload("m1", "coder", 0, 4_000, 300, 0.05))
	writer.busEvent(assistantPayload("m2", "compaction", 1_000, 9_000, 120, 0.02))

	data := writer.summary.data()
	if data == nil {
		t.Fatal("bus tap did not reach the summary")
	}
	writer.stage("agent-summary", "completed", data)

	var seen map[string]any
	for _, line := range strings.Split(output.String(), "\n") {
		if !strings.Contains(line, `"stage":"agent-summary"`) {
			continue
		}
		var record struct {
			Data map[string]any `json:"data"`
		}
		if json.Unmarshal([]byte(line), &record) == nil {
			seen = record.Data
		}
	}
	if seen == nil {
		t.Fatalf("no agent-summary line encoded; output:\n%s", output.String())
	}
	agents, ok := seen["agents"].(map[string]any)
	if !ok || len(agents) != 2 {
		t.Fatalf("agents = %#v", seen)
	}
	compactionAgent, ok := agents["compaction"].(map[string]any)
	if !ok {
		t.Fatalf("compaction agent missing: %#v", agents)
	}
	// JSON round-trips numbers as float64.
	if compactionAgent["wall_union_ms"].(float64) != 8_000 {
		t.Fatalf("compaction wall = %v", compactionAgent["wall_union_ms"])
	}
}

// callPayload is one completed call of a session: how much of its prompt was
// read from the provider cache, and which endpoint served it. A summary flag
// marks a compaction boundary.
func callPayload(id, agent string, created, tokensIn, cacheRead uint64, upstream string, summary bool) bus.Payload {
	message := msgmodel.Assistant{
		Role: "assistant", Agent: agent,
		Time:     msgmodel.AssistantTime{Created: created},
		Tokens:   msgmodel.Tokens{Input: tokensIn, Cache: msgmodel.TokenCache{Read: cacheRead}},
		Upstream: upstream,
	}
	message.ID = id
	message.SessionID = "ses"
	completed := created + 1000
	message.Time.Completed = &completed
	if summary {
		message.Summary = &summary
	}
	return bus.Payload{
		Type:       msgmodel.EventMessageUpdated,
		Properties: msgmodel.UpdatedEvent{SessionID: "ses", Info: message},
	}
}

// A call that reads far less of the previous prompt from cache than the
// prefix it shares is a miss; the first call after a compaction boundary is
// not (its prefix was rebuilt on purpose), nor is a call too small to matter.
// Misses that coincide with an endpoint change are counted separately, and
// the calls per endpoint are reported so the switch rate is visible.
func TestAgentSummaryAttributesCacheMisses(t *testing.T) {
	summary := newAgentSummary()
	summary.observeBus(callPayload("m1", "coder", 1_000, 10_000, 0, "alpha", false))     // first call: no previous prompt
	summary.observeBus(callPayload("m2", "coder", 2_000, 2_000, 10_000, "alpha", false)) // hit
	summary.observeBus(callPayload("m3", "coder", 3_000, 12_000, 0, "beta", false))      // miss, on an endpoint switch
	summary.observeBus(callPayload("m4", "coder", 4_000, 13_000, 1_000, "beta", false))  // miss, same endpoint
	summary.observeBus(callPayload("m5", "compaction", 5_000, 20_000, 0, "beta", true))  // boundary
	summary.observeBus(callPayload("m6", "coder", 6_000, 8_000, 0, "beta", false))       // rebuilt prefix: exempt
	summary.observeBus(callPayload("m7", "coder", 7_000, 500, 8_000, "beta", false))     // hit
	summary.observeBus(callPayload("m8", "coder", 8_000, 3_000, 0, "alpha", false))      // under the size floor: exempt

	agents := summaryAgents(t, summary)
	coder := agents["coder"].(map[string]any)
	if coder["calls"] != 7 || coder["cache_misses"] != 2 ||
		coder["cache_miss_tokens_in"] != uint64(25_000) ||
		coder["cache_misses_after_upstream_switch"] != 1 {
		t.Fatalf("coder rollup = %#v", coder)
	}
	upstreams, ok := coder["upstreams"].(map[string]int)
	if !ok || upstreams["alpha"] != 3 || upstreams["beta"] != 4 {
		t.Fatalf("coder upstreams = %#v", coder["upstreams"])
	}
	compaction := agents["compaction"].(map[string]any)
	if compaction["calls"] != 1 || compaction["cache_misses"] != 0 {
		t.Fatalf("compaction rollup = %#v", compaction)
	}
}

// The spend record is what a caller enforcing a dollar ceiling reads while the
// run is still alive. It fires once per message, on the update that completes
// it, and carries the run's cumulative cost rather than the message's own --
// summing message.updated directly would double-count, because the same
// message arrives more than once.
func TestObserveBusReportsCumulativeSpendOncePerMessage(t *testing.T) {
	summary := newAgentSummary()

	if _, completed := summary.observeBus(
		assistantPayload("m1", "coder", 1000, 0, 0, 0),
	); completed {
		t.Fatal("a created-but-unfinished message reported completion")
	}
	total, completed := summary.observeBus(
		assistantPayload("m1", "coder", 1000, 5000, 400, 0.02),
	)
	if !completed {
		t.Fatal("the update that completed m1 did not report completion")
	}
	if total != 0.02 {
		t.Fatalf("cumulative after m1 = %v, want 0.02", total)
	}

	// A second message, on the compaction agent, adds to the same total.
	total, completed = summary.observeBus(
		assistantPayload("m2", "compaction", 6000, 7000, 100, 0.005),
	)
	if !completed || total != 0.025 {
		t.Fatalf("cumulative after m2 = %v (completed=%v), want 0.025 true", total, completed)
	}

	// A late re-send of an already-complete message must not fire again, or a
	// reader would see the same spend twice.
	if _, completed = summary.observeBus(
		assistantPayload("m1", "coder", 1000, 5000, 400, 0.02),
	); completed {
		t.Fatal("a repeated completed message reported completion twice")
	}
}

// The stream carries the record, not just the accumulator.
func TestBusEventEmitsSpendRecord(t *testing.T) {
	var stream bytes.Buffer
	writer := newEventWriter(&stream)
	writer.busEvent(assistantPayload("m1", "coder", 1000, 0, 0, 0))
	writer.busEvent(assistantPayload("m1", "coder", 1000, 5000, 400, 0.02))

	var spends []float64
	for _, line := range bytes.Split(bytes.TrimSpace(stream.Bytes()), []byte("\n")) {
		var value event
		if err := json.Unmarshal(line, &value); err != nil || value.Type != "spend" {
			continue
		}
		if value.CostUSD == nil {
			t.Fatalf("spend record carries no cost_usd: %s", line)
		}
		spends = append(spends, *value.CostUSD)
	}
	if len(spends) != 1 {
		t.Fatalf("spend records = %d, want 1 (only the completing update)", len(spends))
	}
	if spends[0] != 0.02 {
		t.Fatalf("spend cost_usd = %v, want 0.02", spends[0])
	}
}
