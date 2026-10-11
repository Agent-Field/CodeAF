package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

var floorNow = time.Date(2026, 10, 11, 15, 4, 0, 0, time.UTC)

const (
	actionSentence = "this factory does not provide that action"
	readSentence   = "this factory does not provide that read"
)

type hit struct {
	method, path, body, auth string
}

type capture struct {
	mu         sync.Mutex
	last       hit
	queue, job int
}

func (c *capture) record(r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = hit{r.Method, r.URL.Path, string(raw), r.Header.Get("Authorization")}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/queue" {
		c.queue++
	}
	if r.Method == http.MethodGet && jobPath(r.URL.Path) {
		c.job++
	}
}

func (c *capture) take() hit {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func (c *capture) reads() (queue, job int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.queue, c.job
}

func (c *capture) reset() {
	c.mu.Lock()
	c.queue, c.job = 0, 0
	c.mu.Unlock()
}

func jobPath(path string) bool {
	id, ok := strings.CutPrefix(path, "/v1/jobs/")
	if !ok || id == "" || strings.Contains(id, "/") {
		return false
	}
	n, err := strconv.Atoi(id)
	return err == nil && n > 0
}

func newFloor(t *testing.T) (factory.Seam, *capture) {
	t.Helper()
	h := serve.New(factory.FixtureSeam(floorNow), serve.Config{Address: "http://factory.test", Version: "test"})
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		srv.Close()
		h.Close()
	})
	return client.Open(srv.URL, "floor-secret").Seam(), cap
}

func TestClientLoadMatchesFixtureSeam(t *testing.T) {
	seam, cap := newFloor(t)
	cap.reset()
	got, err := seam.Load()
	if err != nil {
		t.Fatal(err)
	}
	queue, jobs := cap.reads()
	if queue != 1 || jobs != len(got.Items) || jobs != 10 {
		t.Fatalf("Load reads: queue %d, jobs %d, items %d", queue, jobs, len(got.Items))
	}
	if cap.take().auth != "Bearer floor-secret" {
		t.Fatalf("authorization = %q", cap.take().auth)
	}
	want, err := factory.FixtureSeam(floorNow).Load()
	if err != nil {
		t.Fatal(err)
	}
	sameFloor(t, got, throughWire(t, want))
}

func TestVerbsPostAndLeaveTheFixtureFloor(t *testing.T) {
	seam, cap := newFloor(t)
	direct, err := factory.FixtureSeam(floorNow).Load()
	if err != nil {
		t.Fatal(err)
	}
	want := throughWire(t, direct)
	for _, tc := range verbCases(seam) {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil || err.Error() != tc.sentence {
				t.Fatalf("error = %v, want %q", err, tc.sentence)
			}
			got := cap.take()
			if got.method != tc.method || got.path != tc.path || got.body != tc.body {
				t.Fatalf("request = %s %s %s", got.method, got.path, got.body)
			}
			snap, err := seam.Load()
			if err != nil {
				t.Fatal(err)
			}
			sameFloor(t, snap, want)
		})
	}
}

type verbCase struct {
	name, method, path, body, sentence string
	call                               func() error
}

func verbCases(seam factory.Seam) []verbCase {
	ctx := context.Background()
	return []verbCase{
		{"run", "POST", "/v1/jobs/2/run", "", actionSentence, func() error { return seam.Launch(2) }},
		{"stop", "POST", "/v1/jobs/2/stop", "", actionSentence, func() error { return seam.Stop(2) }},
		{"pause", "POST", "/v1/jobs/2/pause", "", actionSentence, func() error { return seam.Hold(2, true) }},
		{"resume", "POST", "/v1/jobs/2/resume", "", actionSentence, func() error { return seam.Hold(2, false) }},
		{"drop", "POST", "/v1/jobs/2/drop", "", actionSentence, func() error { return seam.Dismiss(2) }},
		{"answer", "POST", "/v1/jobs/1/answer", `{"yes":false,"words":"revise the plan"}`, actionSentence,
			func() error { return seam.Answer(1, false, "revise the plan") }},
		{"steer", "POST", "/v1/jobs/2/steer", `{"words":"keep the tests"}`, actionSentence,
			func() error { return seam.Steer(2, "keep the tests") }},
		{"approve", "POST", "/v1/jobs/9/approve", `{"edited":true}`, actionSentence, func() error {
			due, err := seam.SignOff(9, true)
			if due {
				return errDue
			}
			return err
		}},
		{"request_changes", "POST", "/v1/jobs/9/request_changes", `{"words":"prove restart survival"}`, actionSentence,
			func() error { return seam.SendBack(9, "prove restart survival") }},
		{"recheck", "POST", "/v1/jobs/9/recheck", "", actionSentence, func() error { return seam.Reverify(9) }},
		{"prioritize", "POST", "/v1/jobs/7/prioritize", `{"to":"top"}`, actionSentence,
			func() error { return seam.Mark([]int{7}, true) }},
		{"reply", "POST", "/v1/jobs/2/reply", `{"words":"keep the tests"}`, actionSentence,
			func() error { return seam.Say(ctx, 2, "keep the tests") }},
		{"new", "POST", "/v1/jobs", `{"repo":"owner/repo","title":"fix tests","text":"keep the fixture"}`, actionSentence,
			func() error {
				id, err := seam.New("owner/repo", "fix tests\n\nkeep the fixture")
				if id != 0 {
					return errDue
				}
				return err
			}},
		{"repos", "PUT", "/v1/repos", `{"watched":[]}`, actionSentence, func() error { return seam.SetRepos(nil) }},
		{"connect", "POST", "/v1/github/connect", `{"token":""}`, actionSentence,
			func() error { return seam.ConnectGitHub(ctx, "") }},
		{"repos read", "GET", "/v1/repos", "", readSentence, func() error {
			_, _, err := seam.Repos(ctx)
			return err
		}},
	}
}

var errDue = errString("verb returned a result the wire does not carry")

type errString string

func (e errString) Error() string { return string(e) }

func TestGitHubReadIsAZeroLink(t *testing.T) {
	seam, cap := newFloor(t)
	link, err := seam.GitHub(context.Background())
	if err != nil || link != (factory.GitHubLink{}) {
		t.Fatalf("github = %+v, %v", link, err)
	}
	got := cap.take()
	if got.method != http.MethodGet || got.path != "/v1/github" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
}

func TestClientLeavesTUIOnlyDoorsNil(t *testing.T) {
	seam, _ := newFloor(t)
	wired := map[string]bool{}
	for _, v := range api.Verbs {
		for _, name := range v.Seam {
			wired[name] = true
		}
	}
	v := reflect.ValueOf(seam)
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if f.Type.Kind() != reflect.Func {
			continue
		}
		isNil := v.Field(i).IsNil()
		_, only := api.TUIOnly[f.Name]
		switch {
		case only && !isNil:
			t.Errorf("%s is TUI-only and the client set it", f.Name)
		case !only && isNil:
			t.Errorf("%s is nil; it is on the wire or missing from api.TUIOnly", f.Name)
		}
		if !wired[f.Name] && !only {
			t.Errorf("%s is neither wired nor TUI-only", f.Name)
		}
	}
	if seam.Has("pause") || seam.Has("clone") || !seam.Has("launch") || !seam.Has("hold") {
		t.Fatalf("Has: pause %v clone %v launch %v hold %v", seam.Has("pause"), seam.Has("clone"), seam.Has("launch"), seam.Has("hold"))
	}
}

func TestChangedFiresWhenTheFloorChanges(t *testing.T) {
	root := t.TempDir()
	h := serve.New(factory.FixtureSeam(floorNow), serve.Config{Address: "http://factory.test", Version: "test", Root: root})
	srv := httptest.NewServer(h)
	sub := client.Open(srv.URL, "").Subscribe(context.Background())
	t.Cleanup(func() {
		sub.Close()
		srv.Close()
		h.Close()
	})

	deadline := time.Now().Add(4 * time.Second)
	for i := 0; time.Now().Before(deadline); i++ {
		if err := os.WriteFile(filepath.Join(root, "1.json"), []byte(strconv.Itoa(i)), 0o644); err != nil {
			t.Fatal(err)
		}
		select {
		case <-sub.Changed():
			sub.Close()
			if !closed(sub.Changed()) {
				t.Fatal("Close left Changed open")
			}
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("a store write produced no changed signal")
}

func closed(ch <-chan struct{}) bool {
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// throughWire is the floor as Load rebuilds it: the codec, then JSON, then
// the codec again. Fields api.Lost names are the ones that move.
func throughWire(t *testing.T, s factory.Snapshot) factory.Snapshot {
	t.Helper()
	raw, err := json.Marshal(api.FromSnapshot(s, ""))
	if err != nil {
		t.Fatal(err)
	}
	var q api.Queue
	if err := json.Unmarshal(raw, &q); err != nil {
		t.Fatal(err)
	}
	jobs := make([]api.Job, len(q.Jobs))
	for i, row := range q.Jobs {
		one, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(one, &jobs[i]); err != nil {
			t.Fatal(err)
		}
	}
	return api.ToSnapshot(q, jobs)
}

func sameFloor(t *testing.T, got, want factory.Snapshot) {
	t.Helper()
	diffs := snapDiff(reflect.ValueOf(want), reflect.ValueOf(got), "Snapshot")
	if len(diffs) == 0 {
		return
	}
	t.Fatalf("%d diffs\n%s", len(diffs), strings.Join(diffs, "\n"))
}

func snapDiff(a, b reflect.Value, path string) []string {
	if !a.IsValid() || !b.IsValid() {
		if a.IsValid() != b.IsValid() {
			return []string{path + ": validity"}
		}
		return nil
	}
	if a.Type() == reflect.TypeOf(time.Time{}) {
		if !a.Interface().(time.Time).Equal(b.Interface().(time.Time)) {
			return []string{path + ": " + b.Interface().(time.Time).String() + " want " + a.Interface().(time.Time).String()}
		}
		return nil
	}
	switch a.Kind() {
	case reflect.Pointer, reflect.Interface:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				return []string{path + ": nil-ness"}
			}
			return nil
		}
		return snapDiff(a.Elem(), b.Elem(), path)
	case reflect.Struct:
		var out []string
		for i := 0; i < a.NumField(); i++ {
			f := a.Type().Field(i)
			if f.IsExported() {
				out = append(out, snapDiff(a.Field(i), b.Field(i), path+"."+f.Name)...)
			}
		}
		return out
	case reflect.Slice, reflect.Array:
		if a.Len() != b.Len() {
			return []string{path + ": len"}
		}
		if a.Kind() == reflect.Slice && a.IsNil() != b.IsNil() {
			return []string{path + ": nil slice"}
		}
		var out []string
		for i := 0; i < a.Len(); i++ {
			out = append(out, snapDiff(a.Index(i), b.Index(i), path+"["+strconv.Itoa(i)+"]")...)
		}
		return out
	case reflect.Map:
		if a.Len() != b.Len() || a.IsNil() != b.IsNil() {
			return []string{path + ": map"}
		}
		var out []string
		for _, k := range a.MapKeys() {
			bv := b.MapIndex(k)
			if !bv.IsValid() {
				out = append(out, path+" missing key")
				continue
			}
			out = append(out, snapDiff(a.MapIndex(k), bv, path+" key")...)
		}
		return out
	}
	if !reflect.DeepEqual(a.Interface(), b.Interface()) {
		return []string{path + ": got " + stringify(b) + " want " + stringify(a)}
	}
	return nil
}

func stringify(v reflect.Value) string {
	b, err := json.Marshal(v.Interface())
	if err != nil {
		return fmtSprint(v)
	}
	s := string(b)
	if len(s) > 180 {
		s = s[:180]
	}
	return s
}

func fmtSprint(v reflect.Value) string { return v.String() }
