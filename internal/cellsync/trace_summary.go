package cellsync

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

// TraceSummary analyzes saved metadata without replaying network operations.
// Seeded workload metadata replays the fixture; opaque IDs are not payloads.
type TraceSummary struct {
	Phases       map[string]*TracePhaseSummary `json:"phases"`
	LogicalBytes int64                         `json:"workload_logical_bytes"`
	Seed         int64                         `json:"seed"`
	Files        int                           `json:"files"`
}
type TracePhaseSummary struct {
	NativeWallNS     int64                     `json:"native_operation_wall_ns"`
	HTTPWallNS       int64                     `json:"http_operation_wall_ns_not_additive_with_parallel_requests"`
	Operations       map[string]int            `json:"operations"`
	HTTPRequests     int                       `json:"http_requests"`
	HTTPBodies       map[string]TraceBodyBytes `json:"http_body_bytes_by_class"`
	StorePayloadUp   int64                     `json:"encrypted_store_payload_up"`
	StorePayloadDown int64                     `json:"encrypted_store_payload_down"`
	WantCalls        int                       `json:"native_want_calls"`
	WantNonempty     int                       `json:"nonempty_native_frontiers"`
	GetManyBatches   int                       `json:"get_many_batches"`
	GetManyObjects   int                       `json:"get_many_objects"`
	Outcomes         map[string]int            `json:"outcomes"`
}
type TraceBodyBytes struct {
	Sent     int64 `json:"sent"`
	Received int64 `json:"received"`
}

// ReadTraceSummary streams JSONL. HTTP body and decoded Store payload counts
// are different layers and intentionally never added into a misleading total.
func ReadTraceSummary(reader io.Reader) (TraceSummary, error) {
	result := TraceSummary{Phases: map[string]*TracePhaseSummary{}}
	var previousSequence uint64
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		event, err := decodeTraceEvent(scanner.Bytes())
		if err != nil {
			return result, err
		}
		if event.Sequence <= previousSequence {
			return result, errors.New("trace sequence is not strictly increasing")
		}
		previousSequence = event.Sequence
		if event.Op == "workload" {
			result.Seed = event.Seed
			result.Files = event.Files
			result.LogicalBytes = event.LogicalBytes
			continue
		}
		phase := result.Phases[event.Phase]
		if phase == nil {
			phase = &TracePhaseSummary{Operations: map[string]int{}, HTTPBodies: map[string]TraceBodyBytes{}, Outcomes: map[string]int{}}
			result.Phases[event.Phase] = phase
		}
		phase.add(event)
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("read operation trace: %w", err)
	}
	return result, nil
}

func decodeTraceEvent(line []byte) (TraceEvent, error) {
	var event TraceEvent
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&event) != nil {
		return event, errors.New("invalid operation trace event")
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) || !validTraceEvent(event) {
		return event, errors.New("unsupported operation trace schema or metadata")
	}
	return event, nil
}

func validTraceEvent(event TraceEvent) bool {
	if event.DurationNS < 0 || event.RawBytes != nil && *event.RawBytes < 0 {
		return false
	}
	if event.Version != 1 || event.Objects < 0 || event.PayloadBytes < 0 || event.SentBytes < 0 || event.ReceivedBytes < 0 || event.LogicalBytes < 0 || event.Files < 0 {
		return false
	}
	switch event.Op {
	case "workload", "http.request", "store.put_frame", "store.get", "store.get_frame", "store.get_many", "store.has", "store.locate", "engine.want", "engine.export", "engine.published", "engine.materialize", "engine.import", "engine.importprimed", "engine.importpartial":
	default:
		return false
	}
	switch event.Phase {
	case "", "unknown", "cold", "warm", "publish", "calibration":
	default:
		return false
	}
	switch event.Layer {
	case "", "fixture", "store", "engine", "http_body":
	default:
		return false
	}
	switch event.Class {
	case "unknown", "control", "encrypted_bulk", "calibration", "encrypted_frame", "encrypted_frames", "encrypted_object", "encrypted_objects", "unknown_encrypted_or_control", "mixed_fixture":
	default:
		return false
	}
	switch event.Outcome {
	case "ok", "error", "cancelled", "incomplete", "http_error":
	default:
		return false
	}
	switch event.Gate {
	case "", "G3_native_materialized":
	default:
		return false
	}
	return event.RID == "" || blobstore.ValidRID(event.RID)
}

func (phase *TracePhaseSummary) add(event TraceEvent) {
	if event.Layer == "engine" {
		phase.NativeWallNS += event.DurationNS
	}
	phase.Operations[event.Op]++
	phase.Outcomes[event.Outcome]++
	switch event.Op {
	case "http.request":
		phase.HTTPRequests++
		phase.HTTPWallNS += event.DurationNS
		body := phase.HTTPBodies[event.Class]
		body.Sent += event.SentBytes
		body.Received += event.ReceivedBytes
		phase.HTTPBodies[event.Class] = body
	case "store.put_frame":
		phase.StorePayloadUp += event.PayloadBytes
	case "store.get", "store.get_frame", "store.get_many":
		phase.StorePayloadDown += event.PayloadBytes
	}
	if event.Op == "engine.want" {
		phase.WantCalls++
		if event.Objects > 0 {
			phase.WantNonempty++
		}
	}
	if event.Op == "store.get_many" {
		phase.GetManyBatches++
		phase.GetManyObjects += event.Objects
	}
}
