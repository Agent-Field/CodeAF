package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

func TestRetainedRejectedReportRecoversBeforeNewWorkWithoutChangingRequest(t *testing.T) {
	recoveries := 0
	original := ""
	j, now := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/workers/recover/") {
			var recovery api.WorkerRecovery
			if err := json.NewDecoder(r.Body).Decode(&recovery); err != nil {
				t.Fatal(err)
			}
			if recovery.OriginalLease != "lease_test" || string(recovery.Body) != original {
				t.Error("retained report changed")
			}
			recoveries++
			_ = json.NewEncoder(w).Encode(api.WorkerRecovered{Job: api.Job{ID: 1}})
			return
		}
		var report api.WorkerReport
		_ = json.NewDecoder(r.Body).Decode(&report)
		if report.Question != nil {
			w.WriteHeader(200)
			return
		}
		raw, _ := json.Marshal(report)
		original = string(raw)
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"error":"follow-up identity changed"}`))
	}))
	setDeliverySpend(t, j, .25)
	if !j.flush(context.Background()) || j.blocked == "" || j.pending == nil {
		t.Fatal("rejected report not retained")
	}
	if !strings.Contains(j.blocked, "follow-up identity changed") {
		t.Fatal("server rejection detail discarded")
	}
	original = string(j.pending.Request.Body)
	// Simulate a new worker process using only the persisted artifact, without
	// opening a conversation or running a step.
	j.o.Now = func() time.Time { return *now }
	j.o.recoverRetained(context.Background(), j.w)
	if recoveries != 1 {
		t.Fatalf("recoveries=%d", recoveries)
	}
	if _, err := os.Stat(j.deliveryPath()); !os.IsNotExist(err) {
		t.Fatal("accepted artifact remains active", err)
	}
}

func TestRecoveryIsBoundedAndManualRetryReopensOnlyDelivery(t *testing.T) {
	attempts := 0
	accept := false
	j, now := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/workers/recover/") {
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(api.WorkerRecovered{})
				return
			}
			attempts++
			if accept {
				_ = json.NewEncoder(w).Encode(api.WorkerRecovered{})
				return
			}
			w.WriteHeader(422)
			return
		}
		var report api.WorkerReport
		_ = json.NewDecoder(r.Body).Decode(&report)
		if report.Question != nil {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(422)
	}))
	setDeliverySpend(t, j, .25)
	j.flush(context.Background())
	for range 8 {
		j.recoverBlocked(context.Background(), false)
		*now = now.Add(time.Minute)
	}
	if attempts != deliveryRecoveryLimit || j.pending == nil || j.blocked == "" {
		t.Fatal("recovery is not bounded")
	}
	accept = true
	if j.signals(context.Background(), []api.WorkerSignal{{Verb: lease.SignalReconcile}}) {
		t.Fatal("worker continued under released lease")
	}
	if attempts != deliveryRecoveryLimit+1 || j.pending != nil || j.blocked != "" {
		t.Fatal("manual delivery retry failed")
	}
}

func TestRetainedRecoveryRejectsCorruptArtifactAndDifferentFactory(t *testing.T) {
	attempts := 0
	j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { attempts++; w.WriteHeader(422) }))
	setDeliverySpend(t, j, .25)
	cur, _ := j.o.Mirror.Get(j.id)
	p, err := j.prepareDelivery(cur, api.ToWorkerReport(api.WorkerReport{Cost: .25}))
	if err != nil {
		t.Fatal(err)
	}
	p.Reason = "HTTP 422"
	p.Request.Body = []byte(`{"cost":1000}`)
	if err = writeRecovery(j.deliveryPath(), p); err != nil {
		t.Fatal(err)
	}
	j.o.recoverRetained(context.Background(), j.w)
	if attempts != 0 {
		t.Fatal("corrupt immutable request replayed")
	}
}

func TestLegacyLocalRecoveryAllowsOnlyHostOwnedLoopbackPortChanges(t *testing.T) {
	local := Options{LocalFactory: true}
	if !local.sameRecoveryFactory("http://127.0.0.1:59800", "http://127.0.0.1:62944") {
		t.Fatal("local upgrade cannot recover old port")
	}
	for _, o := range []Options{{}, local} {
		for _, saved := range []string{"http://example.test:59800", "http://127.0.0.1:59800/other", "https://127.0.0.1:59800", "http://user@127.0.0.1:59800"} {
			if o.sameRecoveryFactory(saved, "http://127.0.0.1:62944") {
				t.Fatal("different factory accepted", saved)
			}
		}
	}
	if (Options{}).sameRecoveryFactory("http://127.0.0.1:59800", "http://127.0.0.1:62944") {
		t.Fatal("generic worker adopted another local factory")
	}
}
func TestExhaustedRecoveryAcceptsNewTypedRetryRequest(t *testing.T) {
	var token time.Time
	attempts := 0
	j, now := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(api.WorkerRecovered{RetryRequestedAt: token})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/workers/recover/") {
			attempts++
			w.WriteHeader(422)
			return
		}
		var report api.WorkerReport
		_ = json.NewDecoder(r.Body).Decode(&report)
		if report.Question != nil {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(422)
	}))
	setDeliverySpend(t, j, .25)
	j.flush(context.Background())
	for range 5 {
		j.o.recoverRetained(context.Background(), j.w)
		*now = now.Add(time.Minute)
	}
	if attempts != 3 {
		t.Fatal("unbounded startup recovery", attempts)
	}
	token = *now
	j.o.recoverRetained(context.Background(), j.w)
	if attempts != 4 {
		t.Fatal("manual retry token was ignored")
	}
	for range 5 {
		*now = now.Add(time.Minute)
		j.o.recoverRetained(context.Background(), j.w)
	}
	if attempts != 6 {
		t.Fatal("retry request did not get exactly one bounded window", attempts)
	}
}

func TestBlockedRecoveryWaitsForManagerCleanupAndIncludesLateUsage(t *testing.T) {
	additional := -1.0
	j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/workers/recover/") {
			var v api.WorkerRecovery
			_ = json.NewDecoder(r.Body).Decode(&v)
			additional = v.AdditionalCost
			_ = json.NewEncoder(w).Encode(api.WorkerRecovered{})
			return
		}
		var report api.WorkerReport
		_ = json.NewDecoder(r.Body).Decode(&report)
		if report.Question != nil {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(422)
	}))
	setDeliverySpend(t, j, .25)
	j.flush(context.Background())
	j.replying = true
	if j.recoverBlocked(context.Background(), false) || additional >= 0 {
		t.Fatal("recovered before deferred usage cleanup")
	}
	setDeliverySpend(t, j, .5)
	j.replying = false
	if !j.recoverBlocked(context.Background(), false) || additional != .25 {
		t.Fatalf("late usage lost: %v", additional)
	}
	if _, err := os.Stat(j.deliveryPath() + ".recovered"); err != nil {
		t.Fatal("recovery evidence discarded", err)
	}
}
