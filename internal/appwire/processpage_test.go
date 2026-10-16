package appwire

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

type processEngine struct {
	sendEngine
	mu    sync.Mutex
	page  ProcessPageAsk
	log   ProcessLogAsk
	block chan struct{}
}

func (p *processEngine) PageProcesses(_ context.Context, ask ProcessPageAsk) (*ProcessPageResult, error) {
	p.mu.Lock()
	p.page = ask
	p.mu.Unlock()
	if p.block != nil {
		<-p.block
	}
	return &ProcessPageResult{}, nil
}

func (p *processEngine) ReadProcessLog(_ context.Context, ask ProcessLogAsk) (*ProcessLogResult, error) {
	p.mu.Lock()
	p.log = ask
	p.mu.Unlock()
	return &ProcessLogResult{Text: "x\n", Next: 2, Size: 2, EOF: true}, nil
}

// AN ENGINE WITHOUT THE READS REFUSES THEM AS UNKNOWN AND HELLO DOES NOT NAME
// THEM; ONE WITH THEM NAMES BOTH.
func TestProcessReadsAreFeaturesOfTheEnginesThatServeThem(t *testing.T) {
	for _, name := range []string{"processes.page", "processes.log"} {
		if _, err := methods[name].call(&sendEngine{}, json.RawMessage(`{"agent":"a","id":"a:p1"}`)); codeOf(err) != CodeUnknownMethod {
			t.Fatalf("%s on an engine without it: %v", name, err)
		}
	}
	if h := withProcessFeatures(Hello{}, &sendEngine{}); len(h.Features) != 0 {
		t.Fatalf("hello names %v", h.Features)
	}
	h := withProcessFeatures(Hello{}, &processEngine{})
	if len(h.Features) != 2 || h.Features[0] != FeatureProcessesPage || h.Features[1] != FeatureProcessesLog {
		t.Fatalf("hello names %v", h.Features)
	}
}

// THE BOUNDS ARE CHECKED BEFORE ANYTHING IS READ, A CREW IS REFUSED RATHER THAN
// APPROXIMATED, AND AN EMPTY PAGE STILL SAYS processes: [].
func TestProcessReadsAreBoundedBeforeTheEngineIsAsked(t *testing.T) {
	eng := &processEngine{}
	for _, bad := range []string{`{}`, `{"agent":"a","limit":0}`, `{"agent":"a","limit":101}`, `{"agent":"a","crew":"c"}`, `{"agent":"a","scope":"crew"}`} {
		if _, err := methods["processes.page"].call(eng, json.RawMessage(bad)); codeOf(err) != CodeBadParams {
			t.Fatalf("page %s: %v", bad, err)
		}
	}
	res, err := methods["processes.page"].call(eng, json.RawMessage(`{"agent":"a","limit":100,"cursor":"c"}`))
	raw, _ := json.Marshal(res)
	if err != nil || eng.page.Cursor != "c" || *eng.page.Limit != 100 || string(raw) != `{"processes":[]}` {
		t.Fatalf("page: %s %v", raw, err)
	}
	for _, bad := range []string{`{}`, `{"id":"a:p1","limit_bytes":0}`, `{"id":"a:p1","limit_bytes":524289}`, `{"id":"a:p1","offset":-1}`} {
		if _, err := methods["processes.log"].call(eng, json.RawMessage(bad)); codeOf(err) != CodeBadParams {
			t.Fatalf("log %s: %v", bad, err)
		}
	}
	if _, err := methods["processes.log"].call(eng, json.RawMessage(`{"id":"a:p1","offset":0,"limit_bytes":10}`)); err != nil || *eng.log.Offset != 0 || *eng.log.LimitBytes != 10 {
		t.Fatalf("log: %+v %v", eng.log, err)
	}
}

// READS BEYOND THE ADMISSION ARE ANSWERED BUSY, NOT QUEUED.
func TestProcessReadsBeyondTheAdmissionAreBusy(t *testing.T) {
	eng := &processEngine{block: make(chan struct{})}
	done := make(chan error, ProcessReadsInFlight)
	for i := 0; i < ProcessReadsInFlight; i++ {
		go func() {
			_, err := methods["processes.page"].call(eng, json.RawMessage(`{"agent":"a"}`))
			done <- err
		}()
	}
	for len(processReads) < ProcessReadsInFlight {
		select {
		case err := <-done:
			t.Fatalf("a read ended early: %v", err)
		default:
		}
	}
	if _, err := methods["processes.log"].call(&processEngine{}, json.RawMessage(`{"id":"a:p1"}`)); codeOf(err) != CodeBusy {
		t.Fatalf("over admission: %v", err)
	}
	close(eng.block)
	for i := 0; i < ProcessReadsInFlight; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
