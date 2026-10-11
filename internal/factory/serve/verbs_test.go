package serve_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

type verbCall struct {
	name string
	args []any
}

func call(name string, args ...any) verbCall { return verbCall{name, args} }

type verbCase struct {
	name, method, path, body string
	want                     []verbCall
	status                   int
	event                    bool
}

// These requests cross the public HTTP door over FixtureSeam. Each spy records
// the exact seam arguments, so a correct status cannot hide the wrong action.
var verbCases = []verbCase{
	{"run", "POST", "/v1/jobs/2/run", "", []verbCall{call("Launch", 2)}, 200, false},
	{"stop", "POST", "/v1/jobs/2/stop", "", []verbCall{call("Stop", 2)}, 200, false},
	{"pause", "POST", "/v1/jobs/2/pause", "", []verbCall{call("Hold", 2, true)}, 200, false},
	{"resume", "POST", "/v1/jobs/2/resume", "", []verbCall{call("Hold", 2, false)}, 200, false},
	{"drop", "POST", "/v1/jobs/2/drop", "", []verbCall{call("Dismiss", 2)}, 200, false},
	{"answer yes", "POST", "/v1/jobs/1/answer", `{"yes":true}`, []verbCall{call("Answer", 1, true, "")}, 200, false},
	{"answer no", "POST", "/v1/jobs/1/answer", `{"yes":false,"words":" revise the plan "}`, []verbCall{call("Answer", 1, false, " revise the plan ")}, 200, false},
	{"answer words", "POST", "/v1/jobs/1/answer", `{"words":"keep the tests"}`, []verbCall{call("Answer", 1, false, "keep the tests")}, 200, false},
	{"steer", "POST", "/v1/jobs/2/steer", `{"words":"keep the tests"}`, []verbCall{call("Steer", 2, "keep the tests")}, 200, false},
	{"approve plan", "POST", "/v1/jobs/1/approve", `{}`, []verbCall{call("Answer", 1, true, "")}, 200, true},
	{"approve proof", "POST", "/v1/jobs/9/approve", "", []verbCall{call("SignOff", 9, false)}, 200, true},
	{"approve edited", "POST", "/v1/jobs/9/approve", `{"edited":true}`, []verbCall{call("SignOff", 9, true)}, 200, true},
	{"request_changes", "POST", "/v1/jobs/9/request_changes", `{"words":"prove restart survival"}`, []verbCall{call("SendBack", 9, "prove restart survival")}, 200, false},
	{"recheck", "POST", "/v1/jobs/9/recheck", "", []verbCall{call("Reverify", 9)}, 200, false},
	{"prioritize", "POST", "/v1/jobs/7/prioritize", `{"to":"top"}`, []verbCall{call("Mark", []int{7}, true)}, 200, false},
	{"reply", "POST", "/v1/jobs/2/reply", `{"words":"keep the tests"}`, []verbCall{call("Say", 2, "keep the tests")}, 200, false},
	{"new", "POST", "/v1/jobs", `{"repo":"owner/repo","title":"fix tests","text":"keep the fixture"}`, []verbCall{call("New", "owner/repo", "fix tests\n\nkeep the fixture")}, 201, false},
	{"new title only", "POST", "/v1/jobs", `{"repo":"owner/repo","title":"fix tests"}`, []verbCall{call("New", "owner/repo", "fix tests")}, 201, false},
	{"new autostart", "POST", "/v1/jobs", `{"repo":"owner/repo","title":"fix tests","autostart":true}`, []verbCall{call("New", "owner/repo", "fix tests"), call("Launch", 42)}, 201, false},
	{"repos", "PUT", "/v1/repos", `{"watched":["owner/repo","owner/other"]}`, []verbCall{call("SetRepos", []string{"owner/repo", "owner/other"})}, 200, false},
	{"repos clear", "PUT", "/v1/repos", `{"watched":[]}`, []verbCall{call("SetRepos", []string{})}, 200, false},
	{"connect", "POST", "/v1/github/connect", `{"token":"github-token"}`, []verbCall{call("ConnectGitHub", "github-token")}, 200, false},
	{"connect gh", "POST", "/v1/github/connect", `{"token":""}`, []verbCall{call("ConnectGitHub", "")}, 200, false},
}

type verbContextKey struct{}

func verbRequest(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	ctx := context.WithValue(context.Background(), verbContextKey{}, "request")
	r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func spyVerbs(t *testing.T, failure error) (factory.Seam, *[]verbCall) {
	t.Helper()
	s := factory.FixtureSeam(now)
	var calls []verbCall
	record := func(name string, args ...any) error {
		calls = append(calls, call(name, args...))
		return failure
	}
	checkContext := func(ctx context.Context) {
		t.Helper()
		if ctx.Value(verbContextKey{}) != "request" {
			t.Fatal("request context did not reach the seam")
		}
	}
	s.Launch = func(id int) error { return record("Launch", id) }
	s.Stop = func(id int) error { return record("Stop", id) }
	s.Hold = func(id int, on bool) error { return record("Hold", id, on) }
	s.Dismiss = func(id int) error { return record("Dismiss", id) }
	s.Answer = func(id int, yes bool, words string) error { return record("Answer", id, yes, words) }
	s.Steer = func(id int, words string) error { return record("Steer", id, words) }
	s.SignOff = func(id int, edited bool) (bool, error) { return true, record("SignOff", id, edited) }
	s.SendBack = func(id int, words string) error { return record("SendBack", id, words) }
	s.Reverify = func(id int) error { return record("Reverify", id) }
	s.Mark = func(ids []int, on bool) error { return record("Mark", ids, on) }
	s.Say = func(ctx context.Context, id int, words string) error {
		checkContext(ctx)
		return record("Say", id, words)
	}
	s.New = func(repo, words string) (int, error) { return 42, record("New", repo, words) }
	s.SetRepos = func(repos []string) error { return record("SetRepos", repos) }
	s.ConnectGitHub = func(ctx context.Context, token string) error {
		checkContext(ctx)
		return record("ConnectGitHub", token)
	}
	return s, &calls
}

func TestVerbsCallSeamAndMapErrors(t *testing.T) {
	for _, tc := range verbCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, failure := range []struct {
				name   string
				err    error
				status int
			}{
				{"success", nil, tc.status},
				{"failure", errors.New("seam unavailable"), 500},
				{"missing", fmt.Errorf("missing: %w", fs.ErrNotExist), 404},
				{"permission", fmt.Errorf("permission: %w", fs.ErrPermission), 403},
				{"deadline", fmt.Errorf("deadline: %w", context.DeadlineExceeded), 504},
				{"canceled", fmt.Errorf("canceled: %w", context.Canceled), 408},
			} {
				t.Run(failure.name, func(t *testing.T) {
					s, calls := spyVerbs(t, failure.err)
					cfg := config
					cfg.RecordApproval = func(context.Context, int, bool) (string, error) { return "evt_1234abcd", nil }
					w := verbRequest(serve.New(s, cfg), tc.method, tc.path, tc.body)
					checkJSON(t, w, failure.status)
					want := tc.want
					if failure.err != nil {
						want = want[:1]
					}
					if !reflect.DeepEqual(*calls, want) {
						t.Fatalf("seam calls = %#v, want %#v", *calls, want)
					}
					var body struct {
						ID    int    `json:"id"`
						Event string `json:"event"`
						Error string `json:"error"`
					}
					if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
						t.Fatal(err)
					}
					if failure.err != nil {
						if body.Error != failure.err.Error() || body.Event != "" {
							t.Fatalf("error response = %+v", body)
						}
					} else if tc.event && body.Event != "evt_1234abcd" {
						t.Fatalf("approval receipt = %+v", body)
					} else if tc.status == 201 && body.ID != 42 {
						t.Fatalf("new job = %+v", body)
					}
				})
			}
		})
	}
}

func TestRunOnARunningJobAnswers409(t *testing.T) {
	s, calls := spyVerbs(t, nil)
	refusal := run.AlreadyRunning("#2")
	s.Launch = func(id int) error {
		*calls = append(*calls, call("Launch", id))
		return refusal
	}
	w := verbRequest(serve.New(s, config), "POST", "/v1/jobs/2/run", "")
	checkJSON(t, w, http.StatusConflict)
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error != refusal.Error() || len(*calls) != 1 {
		t.Fatalf("response = %s calls = %v", w.Body, *calls)
	}
}

func TestVerbsRefuseAbsentDoorsAndWrongMethods(t *testing.T) {
	for _, tc := range verbCases {
		t.Run(tc.name, func(t *testing.T) {
			w := verbRequest(serve.New(factory.FixtureSeam(now), config), tc.method, tc.path, tc.body)
			checkJSON(t, w, 501)
			s, calls := spyVerbs(t, nil)
			w = verbRequest(serve.New(s, config), "DELETE", tc.path, tc.body)
			checkJSON(t, w, 405)
			if !strings.Contains(w.Header().Get("Allow"), tc.method) || len(*calls) != 0 {
				t.Fatalf("wrong method touched seam or omitted Allow: calls=%v headers=%v", *calls, w.Header())
			}
		})
	}
}

func TestVerbInputRefusalsDoNotTouchSeam(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/v1/jobs/0/run", "", 400},
		{"POST", "/v1/jobs/-1/stop", "", 400},
		{"POST", "/v1/jobs/no/drop", "", 400},
		{"POST", "/v1/jobs/999999999999999999999/run", "", 400},
		{"POST", "/v1/jobs/999/run", "", 404},
		{"POST", "/v1/jobs/2/approve", `{}`, 409},
		{"POST", "/v1/jobs/1/answer", `{}`, 400},
		{"POST", "/v1/jobs/1/answer", `{"yes":"yes"}`, 400},
		{"POST", "/v1/jobs/2/steer", `{"words":" "}`, 400},
		{"POST", "/v1/jobs/9/request_changes", `{}`, 400},
		{"POST", "/v1/jobs/2/reply", "", 400},
		{"POST", "/v1/jobs/7/prioritize", `{"to":"bottom"}`, 400},
		{"POST", "/v1/jobs/2/reply", `{"words":"ok","typo":true}`, 400},
		{"POST", "/v1/jobs/2/reply", `{"words":"ok"} {}`, 400},
		{"POST", "/v1/jobs/2/reply", `{"words":"ok"} trailing`, 400},
		{"POST", "/v1/jobs/2/reply", `null`, 400},
		{"POST", "/v1/jobs/2/reply", `[]`, 400},
		{"POST", "/v1/jobs/2/reply", `{"words":"` + strings.Repeat("x", 1<<20) + `"}`, 413},
		{"POST", "/v1/jobs/9/approve", `{"edited":"yes"}`, 400},
		{"POST", "/v1/jobs", `{"repo":"owner/repo"}`, 400},
		{"POST", "/v1/jobs", `{"title":"work"}`, 400},
		{"PUT", "/v1/repos", `{}`, 400},
		{"PUT", "/v1/repos", `{"watched":null}`, 400},
		{"POST", "/v1/github/connect", `{}`, 400},
		{"POST", "/v1/jobs/2/unknown", `{}`, 404},
	} {
		t.Run(tc.path+tc.body[:min(len(tc.body), 40)], func(t *testing.T) {
			s, calls := spyVerbs(t, nil)
			w := verbRequest(serve.New(s, config), tc.method, tc.path, tc.body)
			checkJSON(t, w, tc.status)
			if len(*calls) != 0 {
				t.Fatalf("invalid request called seam: %v", *calls)
			}
		})
	}
}

func TestApprovalRecordsBeforeConsentAndStopsOnRecordError(t *testing.T) {
	for _, id := range []int{1, 9} {
		for _, failRecord := range []bool{false, true} {
			t.Run(fmt.Sprintf("job%d/failure%t", id, failRecord), func(t *testing.T) {
				s, calls := spyVerbs(t, nil)
				cfg := config
				cfg.RecordApproval = func(ctx context.Context, got int, edited bool) (string, error) {
					if got != id || !edited || ctx.Value(verbContextKey{}) != "request" || len(*calls) != 0 {
						t.Fatal("record did not receive approval before the seam")
					}
					if failRecord {
						return "", errors.New("record unavailable")
					}
					return "evt_1234abcd", nil
				}
				w := verbRequest(serve.New(s, cfg), "POST", fmt.Sprintf("/v1/jobs/%d/approve", id), `{"edited":true}`)
				if failRecord {
					checkJSON(t, w, 500)
					if len(*calls) != 0 {
						t.Fatal("approval happened without its record")
					}
					return
				}
				checkJSON(t, w, 200)
				var body struct{ Event string }
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Event != "evt_1234abcd" || len(*calls) != 1 {
					t.Fatalf("record receipt: %s calls=%v err=%v", w.Body, *calls, err)
				}
			})
		}
	}
}

func TestAutostartChecksLaunchBeforeCreatingAndReportsLaunchFailure(t *testing.T) {
	for _, missing := range []bool{true, false} {
		s, calls := spyVerbs(t, nil)
		status := 500
		if missing {
			s.Launch = nil
			status = 501
		} else {
			s.Launch = func(id int) error {
				*calls = append(*calls, call("Launch", id))
				return errors.New("launch unavailable")
			}
		}
		w := verbRequest(serve.New(s, config), "POST", "/v1/jobs", `{"repo":"owner/repo","title":"work","autostart":true}`)
		checkJSON(t, w, status)
		if missing && len(*calls) != 0 || !missing && len(*calls) != 2 {
			t.Fatalf("autostart calls = %v", *calls)
		}
	}
}

func TestVerbAccessCheckedBeforeMutation(t *testing.T) {
	for _, tc := range verbCases {
		s, calls := spyVerbs(t, nil)
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		serve.New(s, config).ServeHTTP(w, r)
		checkJSON(t, w, 401)
		if len(*calls) != 0 {
			t.Fatalf("unauthorized %s called seam: %v", tc.name, *calls)
		}
	}
}
