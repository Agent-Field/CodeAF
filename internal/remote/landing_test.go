package remote

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

var _ folderLander = (*session.Agent)(nil)

type landingTestAgent struct {
	*fakeAgent
	waiting []session.StandingChange
	result  session.FolderLanding
	fail    error
}

func (a *landingTestAgent) UnlandedChanges() []session.StandingChange { return a.waiting }
func (a *landingTestAgent) LandingFor(folder string) (session.FolderLanding, bool) {
	return a.result, len(a.waiting) > 0 && folder == a.result.Folder
}
func (a *landingTestAgent) Land(string) (session.FolderLanding, error) {
	if a.fail == nil {
		a.waiting = nil
	}
	return a.result, a.fail
}

func TestLandingCrossesTheHostAndWaitingReadsStayOffTheWire(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		t.Run(map[bool]string{false: "lands", true: "refuses"}[refuse], func(t *testing.T) {
			far := &landingTestAgent{fakeAgent: &fakeAgent{model: "m"}, waiting: []session.StandingChange{{Folder: "/srv/repo", Name: "repo", Files: 1}}, result: session.FolderLanding{Folder: "/srv/repo", Name: "repo", Files: []string{"README.md"}, Merged: "merged"}}
			if refuse {
				far.fail = errors.New("landing refused")
			}
			loop := foldersLoop(t, far)
			door, ok := any(loop.Client.Agent()).(interface {
				UnlandedChanges() []session.StandingChange
				LandingFor(string) (session.FolderLanding, bool)
				Land(string) (session.FolderLanding, error)
			})
			if !ok {
				t.Fatal("the local-host adapter has no landing door")
			}
			if got := door.UnlandedChanges(); !reflect.DeepEqual(got, far.waiting) {
				t.Fatalf("welcome waiting = %+v", got)
			}
			if got, ok := door.LandingFor("/srv/repo"); !ok || !reflect.DeepEqual(got, far.result) {
				t.Fatalf("preview = %+v, %t", got, ok)
			}
			if _, ok := door.LandingFor("/srv/other"); ok {
				t.Fatal("preview invented a folder")
			}
			got, err := door.Land("")
			if refuse {
				if err == nil || err.Error() != far.fail.Error() {
					t.Fatalf("refusal = %v", err)
				}
			} else if err != nil || !reflect.DeepEqual(got, far.result) {
				t.Fatalf("landing = %+v, %v", got, err)
			}
			if err := loop.Close(); err != nil {
				t.Fatal(err)
			}
			// The last stated waiting set remains readable after disconnection, so
			// repainting cannot depend on an RPC or silently erase retained work.
			if got := door.UnlandedChanges(); !reflect.DeepEqual(got, far.waiting) {
				t.Fatalf("cached waiting = %+v, want %+v", got, far.waiting)
			}
		})
	}
}
