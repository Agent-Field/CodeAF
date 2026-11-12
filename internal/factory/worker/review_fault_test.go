package worker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestReviewFailedModelIsAcknowledged(t *testing.T) {
	var ids []string
	j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.Unmarshal([]byte(r.Header.Get(api.SignalAcksHeader)), &ids)
		_ = json.NewEncoder(w).Encode(api.WorkerHeartbeat{})
	}))
	path := j.o.Mirror.ItemPath(j.id)
	if err := os.Rename(path, path+".hidden"); err != nil {
		t.Fatal(err)
	}
	signals := []api.WorkerSignal{{Verb: "model", Words: "chosen", Delivery: "lease/model"}}
	j.signals(t.Context(), signals)
	if err := os.Rename(path+".hidden", path); err != nil {
		t.Fatal(err)
	}
	j.signals(t.Context(), signals)
	j.beat(t.Context())
	it, err := j.o.Mirror.Get(j.id)
	if err != nil {
		t.Fatal(err)
	}
	if it.Model != "chosen" {
		t.Fatalf("failed effect never retried: model=%q, next-heartbeat acknowledgements=%v", it.Model, ids)
	}
}

func TestReviewHistoricalAcknowledgementsBreakHeartbeat(t *testing.T) {
	var calls atomic.Int32
	j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(api.WorkerHeartbeat{})
	}))
	for n := 0; n < 25000; n++ {
		j.ackSignal(fmt.Sprintf("lease_%032x/%026d", 1, n))
	}
	// All of these deliveries were already handled; the factory may have removed them.
	j.beat(t.Context())
	ids := make([]string, 0, len(j.signalIDs))
	for id := range j.signalIDs {
		ids = append(ids, id)
	}
	_, err := j.w.Heartbeat(t.Context(), j.lease, ids)
	raw, _ := json.Marshal(ids)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, j.w.Base()+"/heartbeat", strings.NewReader("{}"))
	req.Header.Set(api.SignalAcksHeader, string(raw))
	resp, wireErr := http.DefaultClient.Do(req)
	if wireErr != nil {
		t.Fatal(wireErr)
	}
	defer resp.Body.Close()
	if calls.Load() == 0 {
		t.Fatalf("heartbeat cannot reach renewal handler: handled IDs=%d; client error=%v; raw HTTP status=%d", len(ids), err, resp.StatusCode)
	}
}

func TestFailedSignalWaitsForNextHeartbeat(t *testing.T) {
	for _, verb := range []string{"model", "autonomy", "budget", "hold", "answer", "stop"} {
		t.Run(verb, func(t *testing.T) {
			var headers [][]string
			signal := api.WorkerSignal{Verb: verb, Words: "chosen", Budget: 20, Yes: true, Delivery: "lease/a"}
			j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var ids []string
				if err := json.Unmarshal([]byte(r.Header.Get(api.SignalAcksHeader)), &ids); err != nil {
					t.Error(err)
				}
				headers = append(headers, ids)
				_ = json.NewEncoder(w).Encode(api.WorkerHeartbeat{Signals: []api.WorkerSignal{signal}})
			}))
			path := j.o.Mirror.ItemPath(j.id)
			if err := os.Rename(path, path+".hidden"); err != nil {
				t.Fatal(err)
			}
			if !j.beat(t.Context()) {
				t.Fatal("failed effect released the worker instead of staying pending")
			}
			if err := os.Rename(path+".hidden", path); err != nil {
				t.Fatal(err)
			}
			j.beat(t.Context())
			if len(headers[1]) != 0 {
				t.Fatalf("failed effect acknowledged: %v", headers[1])
			}
			if verb == "model" {
				it, err := j.o.Mirror.Get(j.id)
				if err != nil || it.Model != "chosen" {
					t.Fatalf("next beat did not retry model: %q %v", it.Model, err)
				}
			}
		})
	}
}

func TestEndedJobAcknowledgesMootSignals(t *testing.T) {
	for _, verb := range []string{"hold", "steer", "answer", "stop"} {
		t.Run(verb, func(t *testing.T) {
			var ids []string
			j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.Unmarshal([]byte(r.Header.Get(api.SignalAcksHeader)), &ids)
				_ = json.NewEncoder(w).Encode(api.WorkerHeartbeat{})
			}))
			if err := j.o.Mirror.Update(j.id, func(it *factory.Item) error { it.State = factory.StateLanded; return nil }); err != nil {
				t.Fatal(err)
			}
			j.signals(t.Context(), []api.WorkerSignal{{Verb: verb, Words: "chosen", Yes: true, Delivery: "lease/moot"}})
			j.beat(t.Context())
			if len(ids) != 1 || ids[0] != "lease/moot" {
				t.Fatalf("ended job kept meaningless %s pending: %v", verb, ids)
			}
		})
	}
}

func TestAcknowledgementsLeaveOnlyUnconfirmedDeliveries(t *testing.T) {
	var headers [][]string
	j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ids []string
		_ = json.Unmarshal([]byte(r.Header.Get(api.SignalAcksHeader)), &ids)
		headers = append(headers, ids)
		if len(headers) == 1 {
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(api.WorkerHeartbeat{})
	}))
	for n := 0; n < 25000; n++ {
		j.ackSignal(fmt.Sprintf("lease_%032x/%026d", 1, n))
	}
	j.beat(t.Context())
	if len(j.signalIDs) != 25000 {
		t.Fatal("failed heartbeat forgot deliveries")
	}
	for n := 0; n < 110 && len(j.signalIDs) > 0; n++ {
		j.beat(t.Context())
	}
	if len(j.signalIDs) != 0 {
		t.Fatalf("confirmed deliveries retained: %d", len(j.signalIDs))
	}
	j.beat(t.Context())
	if len(headers[len(headers)-1]) != 0 {
		t.Fatal("confirmed acknowledgements sent again")
	}
}
