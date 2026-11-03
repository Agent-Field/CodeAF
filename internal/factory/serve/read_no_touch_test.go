package serve_test

import (
 "reflect"
 "strconv"
 "testing"
 "time"

 "github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestJobReadsDoNotRecoverExpiredLeases(t *testing.T) {
 f := workerSetupAs(t, "sam")
 job := f.add(t, "sam")
 var worker api.WorkerAttached
 f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{Machine: "desk", Name: "desk", Handle: "sam"}, 201, &worker)
 f.pull(t, worker)
 before, err := f.st.Get(job.ID)
 if err != nil { t.Fatal(err) }
 version, err := f.st.Revision(job.ID)
 if err != nil { t.Fatal(err) }
 f.clock.Add(int64(24*time.Hour))
 for _, path := range []string{"/v1/jobs/"+strconv.Itoa(job.ID), "/v1/queue", "/v1/jobs/"+strconv.Itoa(job.ID)+"/thread", "/v1/me", "/v1/runners"} {
  f.request(t, "GET", path, "", nil, 200, nil)
  after, err := f.st.Get(job.ID)
  if err != nil { t.Fatal(err) }
  revision, err := f.st.Revision(job.ID)
  if err != nil { t.Fatal(err) }
  if !reflect.DeepEqual(before, after) || revision != version { t.Fatalf("GET %s changed expired job: state=%s version=%d want=%d", path, after.State, revision, version) }
 }
}
