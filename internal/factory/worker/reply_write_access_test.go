package worker

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A worker refuses a manager reply before opening its conversation.
func TestWorkerReplyWithoutWriteAccessOpensNothing(t *testing.T) {
	j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected service write") }))
	if err := j.o.Mirror.Update(j.id, func(it *factory.Item) error { it.Tier = factory.TierStranger; return nil }); err != nil {
		t.Fatal(err)
	}
	var opened atomic.Int32
	j.o.Reply = func(context.Context, factory.Item, string) error { opened.Add(1); return nil }
	j.reply(context.Background(), api.WorkerSignal{ID: "reply", Words: "Explain this job."})
	deadline := time.Now().Add(2 * time.Second)
	for j.replyingNow() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if j.replyingNow() || opened.Load() != 0 {
		t.Fatalf("replying=%v conversations=%d", j.replyingNow(), opened.Load())
	}
	it, err := j.o.Mirror.Get(j.id)
	if err != nil {
		t.Fatal(err)
	}
	if factory.JobSpent(it) != 0 {
		t.Fatal("refused reply spent money")
	}
}
