package cellsync_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
)

func TestTraceIsOptInAndPreservesBulkAnswersAndErrors(t *testing.T) {
	store := blobstore.NewMemory()
	if cellsync.ObserveStore(store, nil) != store {
		t.Fatal("nil observer changes store identity/capabilities")
	}
	var output bytes.Buffer
	trace := cellsync.NewJSONLTrace(&output)
	observed := cellsync.ObserveStore(store, trace.Observe)
	rid := strings.Repeat("1", 64)
	frame, err := blobstore.Encode(strings.Repeat("2", 32), []blobstore.Object{{RID: rid, Bytes: append([]byte("AGEO\x01"), []byte("encrypted fixture")...)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := observed.PutFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	want, err := store.GetMany(context.Background(), []string{rid})
	if err != nil {
		t.Fatal(err)
	}
	got, err := observed.GetMany(context.Background(), []string{rid})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("tracing changed bulk response")
	}
	_, before := store.Get(context.Background(), strings.Repeat("3", 64))
	_, after := observed.Get(context.Background(), strings.Repeat("3", 64))
	if !errors.Is(after, before) {
		t.Fatal("trace changed error identity")
	}
	if trace.Err() != nil || strings.Contains(output.String(), "encrypted fixture") {
		t.Fatal("trace writes payload or failed")
	}
	if !strings.Contains(output.String(), `"raw_bytes":null`) {
		t.Fatal("unknown raw bytes presented as measured")
	}
	panicking := cellsync.ObserveStore(store, func(cellsync.TraceEvent) { panic("diagnostic failed") })
	if _, err := panicking.GetMany(context.Background(), []string{rid}); err != nil {
		t.Fatal("observer failure changed operation")
	}
}

type failingTraceWriter struct{}

func (failingTraceWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestTraceWriterFailureAndCancellationDoNotReplaceOperationResults(t *testing.T) {
	memory := blobstore.NewMemory()
	rid := strings.Repeat("1", 64)
	trace := cellsync.NewJSONLTrace(failingTraceWriter{})
	store := cellsync.ObserveStore(memory, trace.Observe)
	original := errors.New("same operation error")
	memory.FailAfter(0, original)
	_, err := store.Get(context.Background(), rid)
	if err != original || !errors.Is(trace.Err(), io.ErrClosedPipe) {
		t.Fatal("trace writer replaced error or hid diagnostic failure")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var got cellsync.TraceEvent
	wrapped := cellsync.ObserveStore(cancelledStore{Store: memory}, func(event cellsync.TraceEvent) { got = event })
	_, err = wrapped.GetMany(cellsync.TracePhase(ctx, "warm", 2), []string{rid})
	if err != context.Canceled || got.Outcome != "cancelled" || got.Attempt != 2 {
		t.Fatalf("cancellation/retry trace changed: %+v %v", got, err)
	}
}

type cancelledStore struct{ blobstore.Store }

func (s cancelledStore) GetMany(ctx context.Context, _ []string) ([]blobstore.Object, error) {
	return nil, ctx.Err()
}

func TestHTTPTraceCountsBodyBytesAndDoesNotReadAhead(t *testing.T) {
	const body = "wire fixture body"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	var event cellsync.TraceEvent
	calls := 0
	client := &http.Client{Transport: cellsync.ObserveHTTP(http.DefaultTransport, func(got cellsync.TraceEvent) { event = got; calls++ })}
	request, _ := http.NewRequest("POST", server.URL+"/private-path-not-to-log", strings.NewReader("request"))
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("trace read response before caller")
	}
	got, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(got) != body || calls != 1 || event.SentBytes != 7 || event.ReceivedBytes != int64(len(body)) {
		t.Fatalf("body trace changed stream/counts: %+v %v", event, err)
	}
	encoded, _ := json.Marshal(event)
	if strings.Contains(string(encoded), "private-path") || strings.Contains(string(encoded), body) {
		t.Fatal("wire trace exposed content or path")
	}
}

func TestNilTraceAdaptersPreserveIdentityWithoutAllocations(t *testing.T) {
	memory := blobstore.NewMemory()
	engine := cellsync.NewFakeEngine(t.TempDir())
	transport := http.DefaultTransport
	allocations := testing.AllocsPerRun(100, func() {
		if cellsync.ObserveStore(memory, nil) != memory || cellsync.ObserveEngine(engine, nil) != engine || cellsync.ObserveHTTP(transport, nil) != transport {
			t.Fatal("nil tracing changes capability")
		}
	})
	if allocations != 0 {
		t.Fatalf("disabled tracing allocates: %v", allocations)
	}
}

type traceRoundTrip func(*http.Request) (*http.Response, error)

func (f traceRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type traceBrokenBody struct{ err error }

func (b traceBrokenBody) Read([]byte) (int, error) { return 0, b.err }
func (b traceBrokenBody) Close() error             { return nil }

func TestHTTPTraceDistinguishesServerFailureTruncationAndBodyError(t *testing.T) {
	sentinel := errors.New("private body error must not appear in trace")
	cases := []struct {
		name      string
		status    int
		body      io.ReadCloser
		readAll   bool
		outcome   string
		readError error
	}{
		{"server", 500, io.NopCloser(strings.NewReader("error reply")), true, "http_error", nil},
		{"early-close", 200, io.NopCloser(strings.NewReader("unread reply")), false, "incomplete", nil},
		{"body-error", 200, traceBrokenBody{sentinel}, true, "error", sentinel},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var event cellsync.TraceEvent
			calls := 0
			inner := traceRoundTrip(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Body: test.body, ContentLength: -1, Header: make(http.Header), Request: request}, nil
			})
			client := &http.Client{Transport: cellsync.ObserveHTTP(inner, func(got cellsync.TraceEvent) { event = got; calls++ })}
			response, err := client.Get("http://fixture.invalid/private-path")
			if err != nil {
				t.Fatal(err)
			}
			if test.readAll {
				_, err = io.ReadAll(response.Body)
				if err != test.readError {
					t.Fatalf("trace replaced body error: %v", err)
				}
			}
			response.Body.Close()
			if calls != 1 || event.Outcome != test.outcome || event.HTTPStatus != test.status {
				t.Fatalf("HTTP result misreported: %+v", event)
			}
			raw, _ := json.Marshal(event)
			if bytes.Contains(raw, []byte(sentinel.Error())) {
				t.Fatal("body error text leaked into trace")
			}
		})
	}
}

func TestSavedTraceAnalysisSeparatesWirePayloadAndNativeBarriers(t *testing.T) {
	var data bytes.Buffer
	sink := cellsync.NewJSONLTrace(&data)
	events := []cellsync.TraceEvent{
		{Op: "workload", Seed: 42, Files: 2, LogicalBytes: 90},
		{Op: "engine.want", Phase: "warm", Objects: 2, Outcome: "ok"},
		{Op: "http.request", Phase: "warm", Class: "encrypted_bulk", SentBytes: 20, ReceivedBytes: 120, Outcome: "ok"},
		{Op: "store.get_many", Phase: "warm", Objects: 2, PayloadBytes: 90, Outcome: "ok"},
		{Op: "engine.want", Phase: "warm", Objects: 0, Outcome: "ok"},
		{Op: "http.request", Phase: "warm", Class: "control", SentBytes: 10, ReceivedBytes: 5, Outcome: "http_error"},
	}
	for _, event := range events {
		sink.Observe(event)
	}
	first, err := cellsync.ReadTraceSummary(bytes.NewReader(data.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	second, err := cellsync.ReadTraceSummary(bytes.NewReader(data.Bytes()))
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("offline trace analysis is not repeatable")
	}
	phase := first.Phases["warm"]
	if first.Seed != 42 || first.LogicalBytes != 90 || phase.HTTPRequests != 2 || phase.StorePayloadDown != 90 || phase.HTTPBodies["encrypted_bulk"].Received != 120 || phase.HTTPBodies["control"].Received != 5 || phase.WantCalls != 2 || phase.WantNonempty != 1 || phase.GetManyBatches != 1 || phase.Outcomes["http_error"] != 1 {
		t.Fatalf("mixed layers or lost barriers: %+v", first)
	}
	if _, err := cellsync.ReadTraceSummary(strings.NewReader(`{"v":99}`)); err == nil {
		t.Fatal("unsupported trace silently accepted")
	}
	if _, err := cellsync.ReadTraceSummary(strings.NewReader(`{"v":1`)); err == nil {
		t.Fatal("truncated trace silently accepted")
	}
}

func TestTraceAnalysisRejectsUnboundedOrUnknownMetadataAndPreservesReaderCancellation(t *testing.T) {
	for _, input := range []string{`{"v":1,"op":"private-custom-label"}`, `{"v":1,"unexpected":"private-data"}`, strings.Repeat(" ", 65<<10)} {
		_, err := cellsync.ReadTraceSummary(strings.NewReader(input))
		if err == nil || strings.Contains(err.Error(), "private-") {
			t.Fatal("invalid trace accepted or raw metadata echoed")
		}
	}
	_, err := cellsync.ReadTraceSummary(traceBrokenBody{context.Canceled})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("reader cancellation replaced: %v", err)
	}
}

func TestTraceAnalysisRejectsInvalidIdentityNegativeSizeAndRepeatedSequence(t *testing.T) {
	negative := int64(-1)
	for _, event := range []cellsync.TraceEvent{
		{Op: "engine.want", RID: strings.Repeat("z", 64)},
		{Op: "engine.want", DurationNS: -1},
		{Op: "engine.want", RawBytes: &negative},
	} {
		var data bytes.Buffer
		sink := cellsync.NewJSONLTrace(&data)
		sink.Observe(event)
		if _, err := cellsync.ReadTraceSummary(&data); err == nil {
			t.Fatal("invalid trace metadata accepted")
		}
	}
	var data bytes.Buffer
	sink := cellsync.NewJSONLTrace(&data)
	sink.Observe(cellsync.TraceEvent{Op: "engine.want"})
	repeated := append(append([]byte{}, data.Bytes()...), data.Bytes()...)
	if _, err := cellsync.ReadTraceSummary(bytes.NewReader(repeated)); err == nil {
		t.Fatal("repeated operation sequence counted twice")
	}
}

func TestHTTPTraceCountsReplayedRequestBodyAndPreservesFactoryError(t *testing.T) {
	request, _ := http.NewRequest("POST", "http://fixture.invalid/", strings.NewReader("request"))
	var event cellsync.TraceEvent
	inner := traceRoundTrip(func(r *http.Request) (*http.Response, error) {
		_, _ = io.Copy(io.Discard, r.Body)
		r.Body.Close()
		retry, err := r.GetBody()
		if err != nil {
			return nil, err
		}
		_, _ = io.Copy(io.Discard, retry)
		retry.Close()
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("reply")), ContentLength: 5, Header: make(http.Header)}, nil
	})
	wrapped := cellsync.ObserveHTTP(inner, func(got cellsync.TraceEvent) { event = got })
	response, err := wrapped.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if event.SentBytes != 14 {
		t.Fatalf("transport replay bytes omitted: %+v", event)
	}
	sentinel := errors.New("same GetBody failure")
	request, _ = http.NewRequest("POST", "http://fixture.invalid/", strings.NewReader("request"))
	request.GetBody = func() (io.ReadCloser, error) { return nil, sentinel }
	_, err = wrapped.RoundTrip(request)
	if err != sentinel {
		t.Fatalf("GetBody error identity replaced: %v", err)
	}
}
