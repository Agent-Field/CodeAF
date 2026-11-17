package serve_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

func TestRemoteThreadAndReplyStayWithTheHoldingWorker(t *testing.T) {
	f := workerSetup(t)
	first, second := f.attach(t, "sam"), f.attach(t, "raj")
	one, two := f.add(t, "sam"), f.add(t, "raj")
	var p, other api.WorkerPull
	f.request(t, "GET", "/v1/workers/pull", first.Credential, nil, 200, &p)
	f.request(t, "GET", "/v1/workers/pull", second.Credential, nil, 200, &other)
	endpoint := "/v1/workers/leases/" + p.Lease.ID
	line := api.Message{ID: "manager-1", At: now, From: "manager", Text: "The plan is ready."}
	report := api.WorkerReport{Talk: "/sam-machine/manager.jsonl", Thread: []api.Message{line}}
	f.request(t, "POST", endpoint+"/report", first.Credential, report, 200, nil)
	f.request(t, "POST", endpoint+"/report", first.Credential, report, 200, nil)
	var thread struct {
		Messages []api.Message `json:"messages"`
	}
	path := fmt.Sprintf("/v1/jobs/%d", one.ID)
	f.request(t, "GET", path+"/thread", "", nil, 200, &thread)
	if !reflect.DeepEqual(thread.Messages, []api.Message{line}) {
		t.Fatalf("remote thread: %+v", thread.Messages)
	}
	f.request(t, "POST", path+"/reply", "", map[string]string{"words": "Keep the old flag."}, 200, nil)
	f.request(t, "GET", path+"/thread", "", nil, 200, &thread)
	if len(thread.Messages) != 2 || thread.Messages[1].State != "pending" {
		t.Fatalf("reply was not immediately pending: %+v", thread.Messages)
	}
	reply := thread.Messages[1]
	var hb api.WorkerHeartbeat
	f.request(t, "POST", "/v1/workers/leases/"+other.Lease.ID+"/heartbeat", second.Credential, nil, 200, &hb)
	if len(hb.Signals) != 0 {
		t.Fatalf("other worker heard the reply: %+v", hb.Signals)
	}
	for range 2 {
		f.request(t, "POST", endpoint+"/heartbeat", first.Credential, nil, 200, &hb)
		if len(hb.Signals) != 1 || hb.Signals[0].Verb != lease.SignalReply || hb.Signals[0].ID != reply.ID || hb.Signals[0].Words != reply.Text {
			t.Fatalf("pending reply signal: %+v", hb.Signals)
		}
	}
	f.request(t, "POST", endpoint+"/report", second.Credential, api.WorkerReport{Delivered: []string{reply.ID}}, 403, nil)
	f.request(t, "POST", endpoint+"/report", first.Credential, api.WorkerReport{Delivered: []string{reply.ID}}, 200, nil)
	f.request(t, "GET", path+"/thread", "", nil, 200, &thread)
	if thread.Messages[1].State != "delivered" {
		t.Fatalf("reply delivery lost: %+v", thread.Messages)
	}
	hb = api.WorkerHeartbeat{}
	f.request(t, "POST", endpoint+"/heartbeat", first.Credential, nil, 200, &hb)
	if len(hb.Signals) != 0 {
		t.Fatalf("acknowledged reply was offered again: %+v", hb.Signals)
	}
	f.request(t, "GET", fmt.Sprintf("/v1/jobs/%d/thread", two.ID), "", nil, 200, &thread)
	if len(thread.Messages) != 0 {
		t.Fatalf("thread crossed jobs: %+v", thread.Messages)
	}
}

func TestIdleReplySurvivesExpiryAndDoesNotRestartSteps(t *testing.T) {
	f := workerSetup(t)
	worker := f.attach(t, "sam")
	job := f.add(t, "sam")
	if err := f.st.Update(job.ID, func(it *factory.Item) error {
		it.State = factory.StateLanded
		it.Talk = "/remote/manager.jsonl"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/v1/jobs/%d", job.ID)
	f.request(t, "POST", path+"/reply", "", map[string]string{"words": "Explain the proof."}, 200, nil)
	var pulled api.WorkerPull
	f.request(t, "GET", "/v1/workers/pull", worker.Credential, nil, 200, &pulled)
	if !pulled.Lease.ThreadOnly || len(pulled.Signals) != 1 {
		t.Fatalf("idle manager pull: %+v", pulled)
	}
	old := pulled.Lease.ID
	f.clock.Add(int64(lease.DefaultTTL + time.Second))
	f.request(t, "GET", "/v1/workers/pull", worker.Credential, nil, 200, &pulled)
	if pulled.Lease.ID == old || !pulled.Lease.ThreadOnly || len(pulled.Signals) != 1 {
		t.Fatalf("reply lost on expiry: %+v", pulled)
	}
	f.request(t, "POST", "/v1/workers/leases/"+old+"/report", worker.Credential, api.WorkerReport{Delivered: []string{pulled.Signals[0].ID}}, 409, nil)
	endpoint := "/v1/workers/leases/" + pulled.Lease.ID
	f.request(t, "POST", endpoint+"/report", worker.Credential, api.WorkerReport{Cost: 1}, 400, nil)
	f.request(t, "POST", endpoint+"/report", worker.Credential, api.WorkerReport{Delivered: []string{pulled.Signals[0].ID}}, 200, nil)
	f.request(t, "POST", endpoint+"/done", worker.Credential, nil, 200, nil)
	stored, err := f.st.Get(job.ID)
	if err != nil || stored.State != factory.StateLanded || stored.Stream != nil {
		t.Fatalf("reply restarted job: %+v %v", stored, err)
	}
	f.request(t, "GET", "/v1/workers/pull", worker.Credential, nil, 204, nil)
}

func TestReportedThreadIsBoundedAndPendingRepliesStay(t *testing.T) {
	f := workerSetup(t)
	worker := f.attach(t, "sam")
	job := f.add(t, "sam")
	var pulled api.WorkerPull
	f.request(t, "GET", "/v1/workers/pull", worker.Credential, nil, 200, &pulled)
	f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/reply", job.ID), "", map[string]string{"words": "Keep this pending."}, 200, nil)
	report := api.WorkerReport{}
	for i := range factory.ThreadMost + 10 {
		report.Thread = append(report.Thread, api.Message{ID: fmt.Sprint(i), From: "manager", Text: "line"})
	}
	report.Thread[len(report.Thread)-1].Text = strings.Repeat("é", factory.MessageTextMost)
	f.request(t, "POST", "/v1/workers/leases/"+pulled.Lease.ID+"/report", worker.Credential, report, 200, nil)
	stored, err := f.st.Get(job.ID)
	if err != nil || len(stored.Thread) != factory.ThreadMost || stored.Thread[0].State != "pending" || len(stored.Thread[len(stored.Thread)-1].Text) != factory.MessageTextMost {
		t.Fatalf("thread bounds: %d %+v %v", len(stored.Thread), stored.Thread[0], err)
	}
}
