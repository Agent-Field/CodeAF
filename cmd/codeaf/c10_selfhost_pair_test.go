package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// C1 and C10 keep the real terminal invitation, browser page and approval on
// the selected self-hosted service, without printing the hosted page address.
func TestC10SelfHostedTerminalPairingUsesItsOwnPage(t *testing.T) {
	rig := newPairRig(t)
	t.Setenv("CODEAF_SYNC_URL", rig.url)
	screen := &pairScreenText{}
	asker := rig.door(rig.homeB, strings.NewReader(""), screen)
	asker.mailbox = pairMailbox
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	done := make(chan error, 1)
	go func() { done <- asker.run(ctx, nil) }()
	link := screen.waitFor(t, `http://127\.0\.0\.1:\d+/p/\S+#\S+`)[0]
	if strings.Contains(screen.String(), pair.LinkOrigin) {
		t.Fatalf("custom invitation names the hosted page: %s", screen.String())
	}
	res, err := http.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusOK || !strings.Contains(string(page), " --via ") {
		t.Fatalf("custom invitation has no working page: %s %s %v", res.Status, page, err)
	}
	out := &pairScreenText{}
	approver := rig.door(rig.homeA, strings.NewReader("a\n"), out)
	approveAt := pairMailbox
	approver.mailbox = func(url string) (pair.Mailbox, error) {
		if url != rig.url {
			t.Errorf("approval selected %q instead of %q", url, rig.url)
		}
		return approveAt(url)
	}
	if err := approver.run(ctx, []string{"approve", link}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	a, errA := identity.Load(rig.homeA)
	b, errB := identity.Load(rig.homeB)
	if errA != nil || errB != nil || a.ID() != b.ID() {
		t.Fatalf("custom page did not pair the homes: %v %v", errA, errB)
	}
}

// C10 refuses a pasted link for a different service before looking up its code
// on the configured service; the printed terminal command remains available.
func TestC10ChatDoesNotSendCustomPairingCodeToAnotherService(t *testing.T) {
	door := newApprovalsDoor(t.TempDir())
	door.mailbox = func(string) (pair.Mailbox, error) { return pair.Mailbox{URL: "https://configured.example"}, nil }
	door.approver = func(pair.Mailbox) (pair.Approver, error) {
		t.Fatal("custom pairing code reached another service")
		return pair.Approver{}, nil
	}
	_, err := door.Pending(t.Context(), "https://chosen.example/p/k7m2q9xd#AAAAAAAAAAAAAAAAAAAAAA")
	if err == nil || !strings.Contains(err.Error(), "that link is waiting on chosen.example, but this computer syncs through configured.example — on the new device run: codeaf pair --via https://configured.example") {
		t.Fatalf("different service link was not explained: %v", err)
	}
}
