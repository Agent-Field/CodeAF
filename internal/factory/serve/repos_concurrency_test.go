package serve

import (
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// TWO OPERATORS TOGGLING DIFFERENT REPOSITORIES MUST BOTH LAND. The switch reads
// the saved list, edits it and saves it back, so without one shared server lock
// both requests read the same list and the later save drops the earlier edit.
// The first save is held at a barrier until the second request has either
// reached its own save (the lost-update shape) or had ample time to, which makes
// the outcome deterministic in both directions.
func TestConcurrentRepoSwitchesOfDifferentReposKeepBothEdits(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	seam := f.seam
	stored := seam.SetRepos
	var mu sync.Mutex
	calls := 0
	firstIn := make(chan struct{})
	secondIn := make(chan struct{})
	seam.SetRepos = func(repos []string) error {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			close(firstIn)
			select {
			case <-secondIn:
			case <-time.After(300 * time.Millisecond):
			}
		} else {
			close(secondIn)
		}
		return stored(repos)
	}
	h := New(seam, f.cfg)
	t.Cleanup(h.Close)

	codes := make(chan int, 2)
	toggle := func(repo string) {
		w := identityRequest(h, "PUT", "/v1/repos/"+repo, `{"on":true}`, "operator-secret", nil)
		codes <- w.Code
	}
	go toggle("acme/api")
	<-firstIn
	go toggle("acme/web")
	for i := 0; i < 2; i++ {
		if code := <-codes; code != 200 {
			t.Fatalf("toggle answered %d", code)
		}
	}
	repos, err := f.st.Repos()
	if err != nil || len(repos) != 2 || repos[0] != "acme/api" || repos[1] != "acme/web" {
		t.Fatalf("an edit was lost: %v %v", repos, err)
	}
}

// The whole-list door shares the lock, so a toggle cannot interleave with it.
func TestRepoSwitchWaitsForTheWholeListDoor(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	seam := f.seam
	stored := seam.SetRepos
	var mu sync.Mutex
	calls := 0
	firstIn := make(chan struct{})
	release := make(chan struct{})
	seam.SetRepos = func(repos []string) error {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			close(firstIn)
			<-release
		}
		return stored(repos)
	}
	h := New(seam, f.cfg)
	t.Cleanup(h.Close)

	whole := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		whole <- identityRequest(h, "PUT", "/v1/repos", `{"watched":["acme/api"]}`, "operator-secret", nil)
	}()
	<-firstIn
	one := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		one <- identityRequest(h, "PUT", "/v1/repos/acme/web", `{"on":true}`, "operator-secret", nil)
	}()
	select {
	case <-one:
		t.Fatal("the switch ran while the whole-list save was in flight")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if w := <-whole; w.Code != 200 {
		t.Fatalf("whole list = %d", w.Code)
	}
	if w := <-one; w.Code != 200 {
		t.Fatalf("switch = %d", w.Code)
	}
	repos, _ := f.st.Repos()
	if len(repos) != 2 {
		t.Fatalf("after both: %v", repos)
	}
}
