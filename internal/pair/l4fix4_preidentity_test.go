package pair

import (
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
)

// L4-B: Chats written before an identity exists survive either pairing road
// byte for byte and belong to the newly joined account without replacement.
func TestL4Fix4PreIdentityChatsSurvivePairing(t *testing.T) {
	for _, link := range []bool{false, true} {
		t.Run(map[bool]string{false: "code", true: "link"}[link], func(t *testing.T) {
			var home string
			var account identity.Identity
			var join func()
			if link {
				r := newLinkRig(t)
				home = r.new
				account, _ = identity.Load(r.old)
				join = func() {
					ui, done := r.start(t, t.Context())
					in := waitInvite(t, ui)
					asking, err := r.approver.Look(t.Context(), in.Ref)
					if err != nil {
						t.Fatal(err)
					}
					if err := r.approver.Approve(t.Context(), asking); err != nil {
						t.Fatal(err)
					}
					if result := waitResult(t, done); result.err != nil {
						t.Fatal(result.err)
					}
				}
			} else {
				r := newChatRig(t)
				home, account = r.homeB, r.a
				join = func() {
					ui := newScreen()
					offer := offerAs(t, r, ui)
					if _, err := r.join(t.Context(), home, ui.nextCode(t).Shown(), newScreen()); err != nil {
						t.Fatal(err)
					}
					if end := offer.wait(t); end.err != nil {
						t.Fatal(end.err)
					}
				}
			}
			journal := preflightChat(t, home)
			before, err := os.ReadFile(journal)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := identity.Load(home); err != identity.ErrNone {
				t.Fatalf("fixture already has an identity: %v", err)
			}
			join()
			if held, err := identity.Load(home); err != nil || held.ID() != account.ID() || identity.Solo(home) {
				t.Fatalf("did not join the account: %v", err)
			}
			if after, err := os.ReadFile(journal); err != nil || string(after) != string(before) {
				t.Fatalf("pairing disturbed the original chat: %v", err)
			}
			if count, err := identity.ChatCount(home); err != nil || count != 1 {
				t.Fatalf("ChatCount = %d, %v", count, err)
			}
		})
	}
}
