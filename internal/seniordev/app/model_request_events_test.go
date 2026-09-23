//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/bus"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
)

type observedFakeStream struct {
	parts         []orclient.StreamPart
	err, closeErr error
	closed        int
}

func (s *observedFakeStream) Next() (orclient.StreamPart, error) {
	if len(s.parts) == 0 {
		return nil, s.err
	}
	p := s.parts[0]
	s.parts = s.parts[1:]
	return p, nil
}
func (s *observedFakeStream) Close() error { s.closed++; return s.closeErr }

func TestModelRequestObservationOnceOnly(t *testing.T) {
	for _, tc := range []struct {
		name          string
		parts         []orclient.StreamPart
		err, closeErr error
		cancel        bool
		status, stage string
	}{
		{"normal", []orclient.StreamPart{orclient.FinishPart{FinishReason: orclient.FinishReason{Unified: "stop"}}}, io.EOF, nil, false, "finished", ""},
		{"tool", []orclient.StreamPart{orclient.FinishPart{FinishReason: orclient.FinishReason{Unified: "tool-calls"}}}, io.EOF, nil, false, "finished", ""},
		{"failed", nil, errors.New("private transport message"), nil, false, "error", "stream"},
		{"provider-error", []orclient.StreamPart{orclient.ErrorPart{Error: json.RawMessage(`{"message":"secret"}`)}}, io.EOF, nil, false, "provider-error", "stream"},
		{"canceled-read", nil, context.Canceled, nil, false, "canceled", "stream"},
		{"deadline-read", nil, context.DeadlineExceeded, nil, false, "deadline", "stream"},
		{"canceled-close", nil, io.EOF, nil, true, "canceled", "close"},
		{"close-error", nil, io.EOF, errors.New("private close message"), false, "error", "close"},
		{"finish-close-error", []orclient.StreamPart{orclient.FinishPart{FinishReason: orclient.FinishReason{Unified: "stop"}}}, io.EOF, errors.New("private close message"), false, "error", "close"},
		{"abort", []orclient.StreamPart{orclient.AbortPart{}}, io.EOF, nil, false, "aborted", "stream"},
		{"eof", nil, io.EOF, nil, false, "eof-without-finish", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var events []modelRequestEvent
			o := beginModelRequest(ctx, func(e modelRequestEvent) { events = append(events, e) }, "ses-test", "coder", "openrouter", "vendor/model")
			inner := &observedFakeStream{parts: append([]orclient.StreamPart{}, tc.parts...), err: tc.err, closeErr: tc.closeErr}
			ledger := &turnLedger{}
			stream := &costPartStream{inner: inner, ledger: ledger, call: ledger.begin(false), observation: o}
			for range tc.parts {
				if _, err := stream.Next(); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := stream.Next(); err != tc.err {
				t.Fatalf("Next error changed: %v", err)
			}
			if tc.cancel {
				cancel()
			}
			for range 2 {
				if err := stream.Close(); err != tc.closeErr {
					t.Fatalf("Close error changed: %v", err)
				}
			}
			o.finish("begin", errors.New("duplicate"))
			if len(events) != 2 || events[0].Phase != "begin" || events[1].Phase != "end" || events[0].RequestID != events[1].RequestID || events[1].Status != tc.status || events[1].ErrorStage != tc.stage {
				t.Fatalf("events = %+v", events)
			}
			if inner.closed != 2 {
				t.Fatalf("underlying Close calls changed: %d", inner.closed)
			}
			raw, _ := json.Marshal(events)
			if strings.Contains(string(raw), "private") || strings.Contains(string(raw), "secret") {
				t.Fatalf("error leaked: %s", raw)
			}
		})
	}
}

func TestModelRequestMetadataWhitelistAndClock(t *testing.T) {
	var events []modelRequestEvent
	o := beginModelRequest(context.Background(), func(e modelRequestEvent) { events = append(events, e) }, "ses", "compaction", "openrouter", "vendor/model")
	provider := "Provider (fast)"
	for range 2 {
		o.observe(orclient.ResponseMetadataPart{ID: "gen-123"}, nil)
		o.observe(orclient.ResponseMetadataPart{ModelID: "vendor/served", IsModel: true}, nil)
	}
	o.observe(orclient.TextDeltaPart{Delta: "PRIVATE PROMPT"}, nil)
	o.observe(orclient.ReasoningDeltaPart{Delta: "思考"}, nil)
	o.observe(orclient.TextDeltaPart{Delta: ""}, nil)
	o.observe(orclient.FinishPart{FinishReason: orclient.FinishReason{Unified: "stop"}, Metadata: orclient.OpenRouterMetadata{Provider: &provider}}, nil)
	// Pin clock boundaries directly: tool settlement / Close must not count.
	o.start = time.Unix(10, 0)
	o.end = o.start.Add(1234 * time.Millisecond)
	o.finish("close", nil)
	end := events[1]
	if end.ResponseID != "gen-123" || end.ServedModel != "vendor/served" || end.Provider != provider || end.Agent != "compaction" || end.ElapsedMS != 1234 {
		t.Fatalf("end=%+v", end)
	}
	if end.TextCharacters != 14 || end.ReasoningCharacters != 2 || end.SubstantiveDeltas != 2 || end.FirstDeltaMS == nil || end.LastDeltaMS == nil || *end.LastDeltaMS < *end.FirstDeltaMS {
		t.Fatalf("delta counters = %+v", end)
	}
	raw, _ := json.Marshal(events)
	if strings.Contains(string(raw), "PRIVATE") {
		t.Fatal("text leaked")
	}
	for _, value := range []string{strings.Repeat("x", 201), "line\nbreak", "{\"secret\":1}", "credential=secret"} {
		if modelRequestLabel(value) != "" {
			t.Fatalf("unsafe label accepted: %q", value)
		}
	}
	if modelRequestFinish("raw private reason") != "unknown" {
		t.Fatal("raw finish leaked")
	}
	second := beginModelRequest(context.Background(), func(modelRequestEvent) {}, "ses", "coder", "openrouter", "model")
	if second.event.RequestID == o.event.RequestID {
		t.Fatal("correlation reused")
	}
}

// Real request assembly and HTTP transport, without sockets or model calls.
// Compare all outbound bytes/headers and returned stream parts with nil,
// recording, and panicking sinks, for both coder and actual summary clients.
func TestModelRequestTelemetryLeavesWireAndResultsUnchanged(t *testing.T) {
	for _, summary := range []bool{false, true} {
		for _, reply := range []string{chatReply("answer", 10), toolCallReply("bash", `{"command":"true"}`)} {
			var baseBody []byte
			var baseHeader http.Header
			var baseParts []string
			var baseCalls []turnCall
			for mode := 0; mode < 3; mode++ {
				var body []byte
				var header http.Header
				var events []modelRequestEvent
				backend := &openRouterBackend{apiKey: "not-a-real-key", variant: "high", client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					body, _ = io.ReadAll(r.Body)
					header = r.Header.Clone()
					// Metadata already in OpenRouter's supported stream format.
					prefix := "data: {\"id\":\"gen-123\",\"model\":\"vendor/served\",\"provider\":\"Provider A\",\"choices\":[]}\n\n"
					return recordedResponse(r, 200, "text/event-stream", prefix+reply), nil
				})}}
				ledger := &turnLedger{}
				client := newSeniorDevLLM(backend, "ses-fixed", "openrouter", "vendor/model", "coder", "high", nil, ledger, false)
				if mode == 1 {
					client.modelRequests = func(e modelRequestEvent) { events = append(events, e) }
				}
				if mode == 2 {
					client.modelRequests = func(modelRequestEvent) { panic("telemetry unavailable") }
				}
				params := orclient.RequestParams{ModelID: "vendor/model", Prompt: []msgmodel.ModelMessage{msgmodel.UserText("PRIVATE TASK")}}
				var stream steploop.PartStream
				var err error
				if summary {
					stream, err = (seniorDevSummaryClient{owner: client}).Stream(context.Background(), params)
				} else {
					stream, err = client.Stream(context.Background(), params)
				}
				if err != nil {
					t.Fatal(err)
				}
				var parts []string
				for {
					p, e := stream.Next()
					if e == io.EOF {
						break
					}
					if e != nil {
						t.Fatal(e)
					}
					raw, _ := json.Marshal(p)
					parts = append(parts, string(raw))
				}
				if err := stream.Close(); err != nil {
					t.Fatal(err)
				}
				if mode == 0 {
					baseBody = body
					baseHeader = header
					baseParts = parts
					baseCalls = ledger.snapshot()
				} else if !reflect.DeepEqual(body, baseBody) || !reflect.DeepEqual(header, baseHeader) || !reflect.DeepEqual(parts, baseParts) || !reflect.DeepEqual(ledger.snapshot(), baseCalls) {
					t.Fatalf("telemetry changed wire/parts/costs: summary=%v mode=%d", summary, mode)
				}
				if mode == 1 {
					if len(events) != 2 || events[1].Provider != "Provider A" || events[1].ServedModel != "vendor/served" || events[1].Status != "finished" {
						t.Fatalf("events=%+v", events)
					}
					wantAgent := "coder"
					if summary {
						wantAgent = "compaction"
					}
					if events[1].Agent != wantAgent {
						t.Fatal("wrong agent")
					}
				}
			}
		}
	}
}

func TestModelRequestBeginFailureAndCancellation(t *testing.T) {
	for _, failure := range []error{errors.New("PRIVATE HTTP FAILURE"), context.Canceled, context.DeadlineExceeded} {
		var events []modelRequestEvent
		backend := &openRouterBackend{apiKey: "test", client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, failure })}}
		client := newSeniorDevLLM(backend, "ses", "openrouter", "vendor/model", "coder", "", nil, &turnLedger{}, false)
		client.modelRequests = func(e modelRequestEvent) { events = append(events, e) }
		_, err := client.Stream(context.Background(), orclient.RequestParams{})
		if err == nil || len(events) != 2 || events[1].ErrorStage != "begin" {
			t.Fatalf("err=%v events=%+v", err, events)
		}
		want := "error"
		if failure == context.Canceled {
			want = "canceled"
		}
		if failure == context.DeadlineExceeded {
			want = "deadline"
		}
		if events[1].Status != want {
			t.Fatalf("status=%s want %s", events[1].Status, want)
		}
		raw, _ := json.Marshal(events)
		if strings.Contains(string(raw), "PRIVATE") {
			t.Fatal("raw error leaked")
		}
	}
}

func TestModelRequestCanceledBeforeReadAndNilSinkClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var events []modelRequestEvent
	o := beginModelRequest(ctx, func(e modelRequestEvent) { events = append(events, e) }, "ses", "coder", "openrouter", "model")
	cancel()
	inner := &observedFakeStream{}
	stream := &costPartStream{inner: inner, observation: o}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Status != "canceled" {
		t.Fatalf("events=%+v", events)
	}
	plain := &costPartStream{inner: inner}
	if err := plain.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestModelRequestRuntimeWiring(t *testing.T) {
	backend := &openRouterBackend{apiKey: "test", client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return recordedResponse(r, 200, "text/event-stream", chatReply("done", 10)), nil
	})}}
	runtime := newRuntime(t.TempDir(), backend)
	defer runtime.Close()
	var events []modelRequestEvent
	runtime.bus.SubscribeCallback(modelRequestEventDefinition, func(p bus.Payload) { events = append(events, p.Properties.(modelRequestEvent)) })
	result, err := runtime.runTurn(context.Background(), turn{Agent: "coder", ProviderID: "openrouter", ModelID: "vendor/model", Prompt: "PRIVATE TASK", RawModelCall: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "done" || len(events) != 2 || events[1].Status != "finished" || events[1].SessionID != result.SessionID {
		t.Fatalf("result=%+v events=%+v", result, events)
	}
}

func TestModelRequestBusSink(t *testing.T) {
	if newModelRequestSink(nil) != nil || beginModelRequest(context.Background(), nil, "", "", "", "") != nil {
		t.Fatal("nil sink not inert")
	}
	instance := bus.New(bus.Context{})
	var got []bus.Payload
	instance.SubscribeCallback(modelRequestEventDefinition, func(p bus.Payload) { got = append(got, p) })
	o := beginModelRequest(context.Background(), newModelRequestSink(instance), "ses", "coder", "openrouter", "model")
	o.finish("close", nil)
	if len(got) != 2 || got[0].Type != "session.model.request" {
		t.Fatalf("got=%+v", got)
	}
}

func TestModelRequestResolutionFailure(t *testing.T) {
	var events []modelRequestEvent
	backend := &openRouterBackend{catalog: seniorDevCatalogFixture(t)}
	client := newSeniorDevLLM(backend, "ses", "openrouter", "missing/model", "coder", "", nil, &turnLedger{}, false)
	client.modelRequests = func(e modelRequestEvent) { events = append(events, e) }
	_, err := client.Stream(context.Background(), orclient.RequestParams{})
	if err == nil || len(events) != 2 || events[1].Status != "error" || events[1].ErrorStage != "resolve" {
		t.Fatalf("err=%v events=%+v", err, events)
	}
}
