package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestWorkerInitialDeliverySaveFailureKeepsUnsentMirror(t *testing.T) {
	j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report api.WorkerReport
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Error(err)
		}
		if report.Question == nil || report.Cost != 0 || len(report.Steps) != 0 {
			t.Error("unsaved results were sent before local retention")
		}
		w.WriteHeader(http.StatusOK)
	}))
	j.o.DeliveryRoot = filepath.Join(j.o.DeliveryRoot, "not-a-directory")
	if err := os.WriteFile(j.o.DeliveryRoot, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	setDeliverySpend(t, j, .25)
	if !j.flush(context.Background()) || j.blocked == "" || j.sent.Stream.Spent != 0 {
		t.Fatal("a failed initial save did not stop unconfirmed delivery")
	}
	if j.flushFinal(context.Background()) {
		t.Fatal("unsaved results allowed Done")
	}
	j.clearMirror()
	retained, err := j.o.Mirror.Get(j.id)
	if err != nil {
		t.Fatal("cleanup erased the only unsaved result snapshot", err)
	}
	if retained.Stream == nil || retained.Stream.Spent != .25 {
		t.Fatal("cleanup lost unacknowledged spend")
	}
}
