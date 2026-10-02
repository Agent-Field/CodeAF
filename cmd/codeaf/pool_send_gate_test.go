package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/pool/judge"
	"github.com/Agent-Field/codeaf/internal/pool/record"
	"github.com/Agent-Field/codeaf/internal/telemetry"
)

// Turning the switch off while the judge holds a sendable snapshot must keep
// both its new scores and the outbox's older rows from reaching the relay.
func TestSettingsOffStopsPoolRowsAlreadyBeingJudged(t *testing.T) {
	t.Setenv("CODEAF_TELEMETRY", "")
	t.Setenv("DO_NOT_TRACK", "")
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_HOME", t.TempDir())
	telemetry.Configure(false)
	t.Cleanup(func() { telemetry.Configure(false) })
	var requests atomic.Int64
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(relay.Close)
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", relay.URL)
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", relay.URL)
	dir := seedOutbox(t)
	before := config.ModelPoolAt(dir)
	if !before.CanSend() {
		t.Fatal("fixture cannot send before the setting changes")
	}
	row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: dir}).Row(config.KeyTelemetry)
	if !ok {
		t.Fatal("telemetry row is absent")
	}
	var asked int
	ask := func(string) judge.Ask {
		return func(context.Context, string, string) (string, error) {
			// The hook has resolved the pool before it asks its first judge,
			// and no pool request has started when the switch changes here.
			asked++
			if err := row.Apply("off"); err != nil {
				t.Fatal(err)
			}
			return `{"score": 88, "reason": "the delivered work answers the brief"}`, nil
		}
	}
	poolJudgeLandingContext(context.Background(), config.Config{}, dir, poolTestCatalog, ask,
		func() time.Time { return time.Unix(100, 0) }, "task", poolTestLanding())
	if asked != 2 {
		t.Fatalf("the local judge answered %d seats, want both", asked)
	}
	if telemetry.OffReason() != telemetry.OffConfig || config.ModelPoolAt(dir).CanSend() {
		t.Fatal("off did not close the usage gate and a freshly resolved pool")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("settings off still allowed %d new pool POSTs from a retained configuration", got)
	}
	sheet, err := record.LoadSheet(record.OwnSheetPath(configuredPoolDir(dir)))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(record.Cells(sheet)); got != 2 {
		t.Fatalf("the local sheet holds %d cells, want both scored seats", got)
	}
	box, err := outboxOpenForTest(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer box.Close()
	if got := len(box.Pending()); got != 2 {
		t.Fatalf("the quieted outbox holds %d pending rows, want the two older rows", got)
	}
}

// A push can need more than one POST. The first request may finish after the
// switch goes off, but the rows still waiting must not start another request.
func TestPoolPushStopsLaterBatchesWhenSettingsTurnOff(t *testing.T) {
	t.Setenv("CODEAF_TELEMETRY", "")
	t.Setenv("DO_NOT_TRACK", "")
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_HOME", t.TempDir())
	telemetry.Configure(false)
	t.Cleanup(func() { telemetry.Configure(false) })
	dir := seedOutbox(t)
	box, err := outboxOpenForTest(dir)
	if err != nil {
		t.Fatal(err)
	}
	// One row past a full batch makes the second POST observable without a
	// timer or a race between the setting write and the next request.
	for i := len(box.Pending()); i < 201; i++ {
		if err := box.Append([]byte(`{"model":"crew/worker"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := box.Close(); err != nil {
		t.Fatal(err)
	}
	row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: dir}).Row(config.KeyTelemetry)
	if !ok {
		t.Fatal("telemetry row is absent")
	}
	var requests atomic.Int64
	applied := make(chan error, 1)
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			applied <- row.Apply("off")
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(relay.Close)
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", relay.URL)
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", relay.URL)
	before := config.ModelPoolAt(dir)
	if !before.CanSend() {
		t.Fatal("fixture cannot send before the setting changes")
	}
	poolPush(context.Background(), dir, before, poolPushBudget)
	select {
	case err := <-applied:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("the first batch did not reach the relay")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("settings off allowed %d pool POSTs, want only the request already on the wire", got)
	}
	box, err = outboxOpenForTest(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer box.Close()
	if got := len(box.Pending()); got != 1 {
		t.Fatalf("the quieted outbox holds %d pending rows, want the unsent second batch", got)
	}
}
