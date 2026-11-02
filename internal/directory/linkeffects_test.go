package directory_test

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
)

func TestR16R17PairingReservationExcludesOtherNamespacesAndLegacyDecisions(t *testing.T) {
	clock := directorytest.NewFakeClock()
	links := directory.NewLinks(clock.Now, directory.DefaultLinkLimits)
	b64 := base64.RawURLEncoding.EncodeToString
	pub := make([]byte, 32)
	pub[0] = 1
	opened, e := links.Open("fixture", directory.NewRequest{Pubkey: b64(pub), X25519: b64(pub), NameSealed: b64(make([]byte, 40))})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = links.Bind(opened.Code, "one"); e != nil {
		t.Fatal(e)
	}
	if _, e = links.Bind(opened.Code, "two"); !errors.Is(e, directory.ErrAlreadyDecided) {
		t.Fatal(e)
	}
	d := directory.Decision{State: directory.RequestApproved, Device: opened.Device, Grant: "c2VhbGVk"}
	if e = links.Decide(opened.Code, d, func(directory.Request) error { t.Fatal("legacy writer ran"); return nil }); !errors.Is(e, directory.ErrAlreadyDecided) {
		t.Fatal(e)
	}
	if e = links.Deliver(opened.Code, "two", "decision", d); !errors.Is(e, directory.ErrAlreadyDecided) {
		t.Fatal(e)
	}
	r, e := links.Read(context.Background(), "fixture", opened.Code, 0)
	if e != nil || r.State != directory.RequestPending {
		t.Fatal(r, e)
	}
	for range 2 {
		if e = links.Deliver(opened.Code, "one", "decision", d); e != nil {
			t.Fatal(e)
		}
	}
	d.Grant = "b3RoZXI"
	if e = links.Deliver(opened.Code, "one", "decision", d); !errors.Is(e, directory.ErrAlreadyDecided) {
		t.Fatal(e)
	}
}
