package appwire

import (
	"context"
	"encoding/json"
	"testing"
)

type madeEngine struct {
	sendEngine
	ask   OrdersMadeAsk
	block chan struct{}
}

func (m *madeEngine) PageOrdersMadeIn(_ context.Context, ask OrdersMadeAsk) (*OrdersMadeResult, error) {
	m.ask = ask
	if m.block != nil {
		<-m.block
	}
	return &OrdersMadeResult{}, nil
}

// AN ENGINE WITHOUT THE READ REFUSES IT AS UNKNOWN AND HELLO DOES NOT NAME IT.
func TestOrdersMadeInIsAFeatureOfTheEnginesThatServeIt(t *testing.T) {
	if _, err := methods["orders.made_in"].call(&sendEngine{}, json.RawMessage(`{"crew":"abc"}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatalf("an engine without it: %v", err)
	}
	if h := withOrdersMadeFeature(Hello{}, &sendEngine{}); len(h.Features) != 0 {
		t.Fatalf("hello names %v", h.Features)
	}
	if h := withOrdersMadeFeature(Hello{}, &madeEngine{}); len(h.Features) != 1 || h.Features[0] != FeatureOrdersMadeIn {
		t.Fatalf("hello names %v", h.Features)
	}
}

// THE BOUNDS ARE CHECKED BEFORE THE ENGINE IS ASKED, AND AN EMPTY PAGE STILL
// SAYS orders: [] AND A REAL unknown.
func TestOrdersMadeInIsBoundedBeforeTheEngineIsAsked(t *testing.T) {
	eng := &madeEngine{}
	long := make([]byte, OrdersMadeCursorMax+1)
	for i := range long {
		long[i] = 'a'
	}
	for _, bad := range []string{`{}`, `{"crew":"../x"}`, `{"crew":"a b"}`, `{"crew":"abc","limit":-1}`, `{"crew":"abc","limit":101}`, `{"crew":"abc","cursor":"a\nb"}`, `{"crew":"abc","cursor":"` + string(long) + `"}`} {
		if _, err := methods["orders.made_in"].call(eng, json.RawMessage(bad)); codeOf(err) != CodeBadParams {
			t.Fatalf("%s: %v", bad, err)
		}
	}
	if eng.ask.Crew != "" {
		t.Fatalf("a refused ask reached the engine: %+v", eng.ask)
	}
	raw, err := methods["orders.made_in"].call(eng, json.RawMessage(`{"crew":"abc","limit":100,"cursor":"id-7"}`))
	if err != nil || eng.ask.Limit != 100 || eng.ask.Cursor != "id-7" {
		t.Fatalf("a good ask: %+v %v", eng.ask, err)
	}
	body, _ := json.Marshal(raw)
	if string(body) != `{"orders":[],"unknown":0}` {
		t.Fatalf("empty page = %s", body)
	}
}

// READS BEYOND THE ADMISSION ARE ANSWERED BUSY, NOT QUEUED, AND THE SLOT IS HELD
// UNTIL THE READ ENDS.
func TestOrdersMadeInBeyondTheAdmissionIsBusy(t *testing.T) {
	eng := &madeEngine{block: make(chan struct{})}
	done := make(chan error, OrdersMadeReadsInFlight)
	for i := 0; i < OrdersMadeReadsInFlight; i++ {
		go func() {
			_, err := methods["orders.made_in"].call(eng, json.RawMessage(`{"crew":"abc"}`))
			done <- err
		}()
	}
	for len(ordersMadeReads) < OrdersMadeReadsInFlight {
		select {
		case err := <-done:
			t.Fatalf("a read ended early: %v", err)
		default:
		}
	}
	if _, err := methods["orders.made_in"].call(&madeEngine{}, json.RawMessage(`{"crew":"abc"}`)); codeOf(err) != CodeBusy {
		t.Fatalf("over admission: %v", err)
	}
	close(eng.block)
	for i := 0; i < OrdersMadeReadsInFlight; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := methods["orders.made_in"].call(&madeEngine{}, json.RawMessage(`{"crew":"abc"}`)); err != nil {
		t.Fatalf("the slots were not given back: %v", err)
	}
}
