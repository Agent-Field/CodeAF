package api

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestPublishingIsAdditiveContractOneAndRoundTrips(t *testing.T) {
	if Contract != 2 {
		t.Fatal("publishing changed the contract")
	}
	for _, state := range []string{"queued", "pushing", "opened", "failed"} {
		it := factory.Item{ID: 7, State: factory.StateLanded, HeadCommit: "1111111111111111111111111111111111111111", Publishing: true,
			PublishStatus: &factory.Publishing{State: state, Attempt: 2, At: time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)}}
		if state == "failed" {
			it.PublishStatus.Code, it.PublishStatus.Detail = "no_push_permission", "permission denied"
		}
		if state == "opened" {
			it.PublishStatus.PR = &factory.PublishedPR{Number: 8, URL: "https://github.com/owner/repo/pull/8"}
		}
		raw, err := json.Marshal(FromItem(it))
		if err != nil {
			t.Fatal(err)
		}
		var job Job
		if err := json.Unmarshal(raw, &job); err != nil {
			t.Fatal(err)
		}
		back := ToItem(job)
		if back.HeadCommit != it.HeadCommit || !back.Publishing || !reflect.DeepEqual(back.PublishStatus, it.PublishStatus) {
			t.Fatalf("lost publishing: %+v", back)
		}
		var old struct {
			ID   int `json:"document_id"`
			Desk struct{ Publishing bool }
		}
		if err := json.Unmarshal(raw, &old); err != nil || old.ID != 7 || !old.Desk.Publishing {
			t.Fatalf("old client = %+v, %v", old, err)
		}
		if state == "failed" && (job.State != StateNeedsYou || job.Gate != "publish_failed") {
			t.Fatalf("failed publish is not Needs you: %+v", job)
		}
		report := factory.RunReport{HeadCommit: it.HeadCommit, Publishing: it.PublishStatus}
		raw, _ = json.Marshal(FromRunReport(report))
		var worker WorkerReport
		if err := json.Unmarshal(raw, &worker); err != nil || !reflect.DeepEqual(ToWorkerReport(worker), report) {
			t.Fatalf("worker lost publishing: %s, %v", raw, err)
		}
		if state == "opened" {
			var fields map[string]any
			jobRaw, _ := json.Marshal(job)
			_ = json.Unmarshal(jobRaw, &fields)
			pr := fields["publishing"].(map[string]any)["pr"].(map[string]any)
			if pr["number"] != float64(8) || pr["url"] == nil {
				t.Fatalf("PR wire = %v", pr)
			}
		}
	}
	fixture := factory.Fixture(time.Now())
	if fixture.Items[9].HeadCommit == "" || fixture.Items[9].PublishStatus == nil {
		t.Fatal("fixture lacks publishing facts")
	}
}
