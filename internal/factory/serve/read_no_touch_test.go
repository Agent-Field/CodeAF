package serve_test

import (
 "bytes"
 "net/http/httptest"
 "os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/api"
 "github.com/Agent-Field/codeaf/internal/factory/lease"
)

func TestJobReadsDoNotRecoverExpiredLeases(t *testing.T) {
	f := workerSetupAs(t, "sam")
	job := f.add(t, "sam")
	var worker api.WorkerAttached
	f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{Machine: "desk", Name: "desk", Handle: "sam"}, 201, &worker)
	f.pull(t, worker)
	before, err := f.st.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	version, err := f.st.Revision(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.clock.Add(int64(24 * time.Hour))
	for _, path := range []string{"/v1/jobs/" + strconv.Itoa(job.ID), "/v1/queue", "/v1/jobs/" + strconv.Itoa(job.ID) + "/thread", "/v1/me", "/v1/runners"} {
		f.request(t, "GET", path, "", nil, 200, nil)
		after, err := f.st.Get(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		revision, err := f.st.Revision(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) || revision != version {
			t.Fatalf("GET %s changed expired job: state=%s version=%d want=%d", path, after.State, revision, version)
		}
	}
}

func TestWorkerChecksDoNotTouchJobOrLease(t *testing.T) {
 f := workerSetupAs(t, "sam")
 job := f.add(t, "sam")
 worker := f.attach(t, "sam")
 pulled := f.pull(t, worker)
 if err := f.st.Update(job.ID, func(it *factory.Item) error { it.Publishing = true; return nil }); err != nil { t.Fatal(err) }
 desk, err := lease.Open(f.st, lease.Options{})
 if err != nil { t.Fatal(err) }
 f.clock.Add(int64(time.Second))
 for _, check := range []string{"publish-check", "stop-after-step"} {
  before, err := f.st.Get(job.ID)
  if err != nil { t.Fatal(err) }
  version, err := f.st.Revision(job.ID)
  if err != nil { t.Fatal(err) }
  holds, err := os.ReadFile(desk.Path())
  if err != nil { t.Fatal(err) }
  req := httptest.NewRequest("GET", "/v1/workers/leases/"+pulled.Lease.ID+"/"+check, nil)
  req.RemoteAddr = "127.0.0.1:1234"
  req.Header.Set("Authorization", "Bearer "+worker.Credential)
  w := httptest.NewRecorder()
  f.handler.ServeHTTP(w, req)
  if w.Code != 200 { t.Fatalf("%s: %d %s", check, w.Code, w.Body.String()) }
  after, err := f.st.Get(job.ID)
  if err != nil { t.Fatal(err) }
  revision, err := f.st.Revision(job.ID)
  if err != nil { t.Fatal(err) }
  kept, err := os.ReadFile(desk.Path())
  if err != nil { t.Fatal(err) }
  if !reflect.DeepEqual(before, after) || revision != version || !bytes.Equal(holds, kept) { t.Errorf("%s read changed job or lease: version=%d want=%d", check, revision, version) }
 }
}
