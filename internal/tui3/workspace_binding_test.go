package tui3

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

type bindingTaker struct {
	seen  *directory.LeaseExpectation
	roots map[string]string
	calls int
}

func (f *bindingTaker) Take(ctx context.Context, id string) (Taken, error) {
	return f.TakeSeen(ctx, id, nil)
}
func (f *bindingTaker) TakeSeen(_ context.Context, _ string, seen *directory.LeaseExpectation) (Taken, error) {
	f.calls++
	f.seen = seen
	return Taken{}, &session.WorkspaceBindingsRequired{Roots: []session.WorkspaceRootBinding{{URI: "root-app", Label: "1/app"}, {URI: "root-library", Label: "1/library"}}}
}
func (f *bindingTaker) TakeSeenWithWorkspaceRoots(_ context.Context, _ string, seen *directory.LeaseExpectation, roots map[string]string) (Taken, error) {
	f.calls++
	if f.seen == nil || seen == nil || *f.seen != *seen {
		return Taken{}, directory.ErrConditionalTake
	}
	f.roots = roots
	return Taken{}, directory.ErrLeaseHeld
}

func TestTakeRootPickerCollectsExplicitFoldersThroughKeys(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "select", true: "cancel"}[cancel], func(t *testing.T) {
			fake := &bindingTaker{}
			rows := chatlist.Static{{Cell: "mapping-chat", Title: "Mapped task", Device: "other", DeviceID: "device-a", Fence: 17, Status: chatlist.Off}}
			a := continueHome(t, fake, rows, 200)
			base := t.TempDir()
			app, library := filepath.Join(base, "local-app"), filepath.Join(base, "local-library")
			for _, path := range []string{app, library} {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			a.home.world = session.World{Projects: []session.Project{{Path: app}, {Path: library}}}
			standOn(t, a, "mapping-chat")
			confirm(t, a)
			if got := plain(a.View().Content); !strings.Contains(got, "local folder for 1/app") {
				t.Fatalf("mapping choice is absent: %s", got)
			}
			if cancel {
				drive(t, a, key("esc"))
				if fake.calls != 1 || len(fake.roots) != 0 {
					t.Fatal("cancelling retried takeover or retained bindings")
				}
				return
			}
			choose := func(name string) {
				msgs := []tea.Msg{tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}}
				for _, char := range name {
					msgs = append(msgs, key(string(char)))
				}
				msgs = append(msgs, key("enter"))
				drive(t, a, msgs...)
			}
			choose(app + string(filepath.Separator))
			if got := plain(a.View().Content); !strings.Contains(got, "local folder for 1/library") {
				t.Fatalf("second root choice is absent: %s", got)
			}
			choose(library + string(filepath.Separator))
			if fake.calls != 2 || fake.roots["root-app"] != app || fake.roots["root-library"] != library {
				t.Fatalf("explicit choices lost: calls=%d roots=%v", fake.calls, fake.roots)
			}
			if fake.seen.Device != "device-a" || fake.seen.Fence != 17 {
				t.Fatalf("holder expectation changed: %+v", fake.seen)
			}
		})
	}
}
