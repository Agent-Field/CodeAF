package pair

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/identity/identitytest"
)

func TestCodeAndLinkPairingAcceptAnOpenedChatAndKeepASentChat(t *testing.T) {
	for _, road := range []string{"code", "link"} {
		t.Run(road, func(t *testing.T) {
			for _, sent := range []bool{false, true} {
				t.Run(map[bool]string{false: "only opened", true: "one sent turn"}[sent], func(t *testing.T) {
					home := t.TempDir()
					place := identitytest.OpenAndQuit(t, home, true)
					before, err := identity.Load(home)
					if err != nil {
						t.Fatal(err)
					}
					if sent {
						f, err := os.OpenFile(place.Transcript(), os.O_APPEND|os.O_WRONLY, 0)
						if err != nil {
							t.Fatal(err)
						}
						_, writeErr := f.WriteString("{\"type\":\"message\",\"role\":\"user\",\"content\":\"hello\"}\n")
						if err := errors.Join(writeErr, f.Close()); err != nil {
							t.Fatal(err)
						}
					}
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					var want identity.Identity
					if road == "code" {
						r := newChatRig(t)
						want = r.a
						ui := newScreen()
						offer := offerAs(t, r, ui)
						_, err = r.join(ctx, home, ui.nextCode(t).Shown(), newScreen())
						if !sent && err == nil {
							if end := offer.wait(t); end.err != nil {
								t.Fatal(end.err)
							}
						}
					} else {
						r := newLinkRig(t)
						r.new = home
						want, _ = identity.Load(r.old)
						ui := &invites{got: make(chan Invite, 1)}
						done := make(chan result, 1)
						guard.Go("pair/opened-chat", func() {
							joined, joinErr := JoinByLink(ctx, r.approver.Requests, r.joining(false), ui)
							done <- result{joined, joinErr}
						})
						if !sent {
							select {
							case in := <-ui.got:
								asking, lookErr := r.approver.Look(ctx, in.Ref)
								if lookErr != nil {
									t.Fatal(lookErr)
								}
								if approveErr := r.approver.Approve(ctx, asking); approveErr != nil {
									t.Fatal(approveErr)
								}
							case res := <-done:
								t.Fatalf("an opened chat refused link pairing: %v", res.err)
							case <-ctx.Done():
								t.Fatal(ctx.Err())
							}
						}
						err = waitResult(t, done).err
					}
					after, loadErr := identity.Load(home)
					if loadErr != nil {
						t.Fatal(loadErr)
					}
					if sent {
						if !errors.Is(err, ErrDifferentChats) || !strings.HasPrefix(err.Error(), "this computer already has 1 chat of its own, so nothing was changed.") || after.ID() != before.ID() {
							t.Fatalf("sent chat was not kept with its counted refusal: %v", err)
						}
					} else if err != nil || after.ID() != want.ID() {
						t.Fatalf("an opened chat did not pair: %v", err)
					}
				})
			}
		})
	}
}
