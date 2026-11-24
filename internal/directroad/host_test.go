package directroad

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/livedoor"
)

// sampleOfferSDP returns a valid data-channel offer SDP generated once from a
// real PeerConnection so host policy tests can submit well-formed offers.
func sampleOfferSDP(t *testing.T) string {
	t.Helper()
	pc, _, err := newPeerConnection(loopbackConfig())
	if err != nil {
		t.Fatalf("newPeerConnection: %v", err)
	}
	defer func() { _ = pc.Close() }()

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	return offer.SDP
}

func mustEncodeOffer(t *testing.T, id, sdp string) []byte {
	t.Helper()
	payload, err := livedoor.EncodeSignal(livedoor.Signal{
		T:   livedoor.SignalOffer,
		ID:  id,
		SDP: sdp,
	})
	if err != nil {
		t.Fatalf("EncodeSignal: %v", err)
	}
	return payload
}

// Contract: Host.Offered enforces synchronous admission policy:
//   - returns nil before SetAcceptor or on invalid/non-offer payloads;
//   - supersedes an existing pending offer from the same peer.Device (closing
//     the old session and accepting the new one);
//   - caps concurrent pending offers at livedoor.PendingOffers across distinct
//     devices;
//   - releases pending attempts that produce no stream within livedoor.OfferTTL.
func TestAnOfferFromTheSameDeviceSupersedesItsPendingOneAndTheCapsHold(t *testing.T) {
	sdp := sampleOfferSDP(t)
	noopSignaller := fnSignaller(func([]byte) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rig := newVNetRig(t)
	host := New(rig.hostConfig())
	defer host.Close()

	// 1. Before SetAcceptor is called, Offered must refuse (return nil).
	validOffer := mustEncodeOffer(t, "0000000000000001", sdp)
	if sess := host.Offered(ctx, livedoor.Peer{Device: "dev-1"}, validOffer, noopSignaller); sess != nil {
		sess.Close()
		t.Fatal("Offered succeeded before SetAcceptor was installed")
	}

	host.SetAcceptor(ctx, func(_ context.Context, stream io.ReadWriteCloser) {
		_ = stream.Close()
	})

	// 2. Invalid payloads, wrong signal kind, empty ID, and empty SDP all
	// return nil synchronously without taking a slot.
	if sess := host.Offered(ctx, livedoor.Peer{Device: "dev-1"}, []byte("{not-json"), noopSignaller); sess != nil {
		t.Fatal("Offered accepted undecodable payload")
	}
	endFrame, _ := livedoor.EncodeSignal(livedoor.Signal{T: livedoor.SignalEnd, ID: "0000000000000001"})
	if sess := host.Offered(ctx, livedoor.Peer{Device: "dev-1"}, endFrame, noopSignaller); sess != nil {
		t.Fatal("Offered accepted non-offer signal")
	}
	emptyID, _ := livedoor.EncodeSignal(livedoor.Signal{T: livedoor.SignalOffer, ID: "", SDP: sdp})
	if sess := host.Offered(ctx, livedoor.Peer{Device: "dev-1"}, emptyID, noopSignaller); sess != nil {
		t.Fatal("Offered accepted offer with empty ID")
	}
	emptySDP, _ := livedoor.EncodeSignal(livedoor.Signal{T: livedoor.SignalOffer, ID: "0000000000000001", SDP: ""})
	if sess := host.Offered(ctx, livedoor.Peer{Device: "dev-1"}, emptySDP, noopSignaller); sess != nil {
		t.Fatal("Offered accepted offer with empty SDP")
	}
	if got := host.pendingCount(); got != 0 {
		t.Fatalf("pendingCount after invalid offers = %d, want 0", got)
	}

	// 3. A second offer from the SAME peer.Device supersedes and closes the
	// first session rather than refusing as busy.
	firstSess := host.Offered(ctx, livedoor.Peer{Device: "dev-1"}, mustEncodeOffer(t, "1111111111111111", sdp), noopSignaller)
	if firstSess == nil {
		t.Fatal("first offer from dev-1 was refused")
	}
	secondSess := host.Offered(ctx, livedoor.Peer{Device: "dev-1"}, mustEncodeOffer(t, "2222222222222222", sdp), noopSignaller)
	if secondSess == nil {
		t.Fatal("superseding offer from dev-1 was refused")
	}
	if !firstSess.(*hostSession).closed.Load() {
		t.Fatal("first pending session from dev-1 was not closed when superseded")
	}
	if got := host.pendingCount(); got != 1 {
		t.Fatalf("pendingCount after superseding same device = %d, want 1", got)
	}

	// 4. Fill remaining slots up to livedoor.PendingOffers from distinct
	// devices; the (livedoor.PendingOffers + 1)th distinct device must get nil.
	sessions := []livedoor.DirectSession{secondSess}
	for i := 2; i <= livedoor.PendingOffers; i++ {
		dev := fmt.Sprintf("dev-%d", i)
		id := fmt.Sprintf("%016x", i)
		s := host.Offered(ctx, livedoor.Peer{Device: dev}, mustEncodeOffer(t, id, sdp), noopSignaller)
		if s == nil {
			t.Fatalf("offer %d (within PendingOffers=%d) was refused", i, livedoor.PendingOffers)
		}
		sessions = append(sessions, s)
	}
	if got := host.pendingCount(); got != livedoor.PendingOffers {
		t.Fatalf("pendingCount = %d, want %d", got, livedoor.PendingOffers)
	}

	overflowDev := fmt.Sprintf("dev-%d", livedoor.PendingOffers+1)
	if overflow := host.Offered(ctx, livedoor.Peer{Device: overflowDev}, mustEncodeOffer(t, "ffffffffffffffff", sdp), noopSignaller); overflow != nil {
		overflow.Close()
		t.Fatalf("offer %d beyond livedoor.PendingOffers=%d was accepted", livedoor.PendingOffers+1, livedoor.PendingOffers)
	}

	// Even when all livedoor.PendingOffers slots are full, a new offer from an
	// already-pending device still supersedes its own slot.
	replaced := host.Offered(ctx, livedoor.Peer{Device: "dev-1"}, mustEncodeOffer(t, "3333333333333333", sdp), noopSignaller)
	if replaced == nil {
		t.Fatal("superseding offer from dev-1 was refused when host was at PendingOffers cap")
	}
	sessions[0] = replaced

	for _, s := range sessions {
		s.Close()
	}
	if got := host.pendingCount(); got != 0 {
		t.Fatalf("pendingCount after closing all sessions = %d, want 0", got)
	}

	// 5. Shrink livedoor.OfferTTL and verify that a pending attempt with no
	// stream is automatically closed and releases its slot after OfferTTL.
	origTTL := livedoor.OfferTTL
	livedoor.OfferTTL = 150 * time.Millisecond
	t.Cleanup(func() {
		livedoor.OfferTTL = origTTL
	})

	ttlSess := host.Offered(ctx, livedoor.Peer{Device: "dev-ttl"}, mustEncodeOffer(t, "4444444444444444", sdp), noopSignaller)
	if ttlSess == nil {
		t.Fatal("TTL test offer was refused")
	}
	if got := host.pendingCount(); got != 1 {
		t.Fatalf("pendingCount = %d, want 1", got)
	}

	eventually(t, 2*time.Second, "pending offer to expire after OfferTTL", func() bool {
		return host.pendingCount() == 0
	})
}

// Contract: when the host answers an offer with a refuse signal, Offer returns
// *RefusedError carrying the reason promptly and leaks nothing.
func TestARefusalEndsOfferWithARefusedError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	// A host without SetAcceptor returns nil from Offered, causing the bridge
	// (acting as the router) to send a refuse signal with RefuseBusy.
	host := New(loopbackConfig())
	defer host.Close()

	bridge := newSignalBridge(ctx, host, livedoor.Peer{Device: "viewer-refused"})
	bridge.refuseOnNil = livedoor.RefuseBusy

	start := time.Now()
	stream, err := Offer(ctx, loopbackConfig(), bridge.viewerSignaller(), bridge.deliver())
	elapsed := time.Since(start)

	if err == nil {
		_ = stream.Close()
		t.Fatal("Offer succeeded when host refused")
	}
	if stream != nil {
		t.Fatalf("Offer returned non-nil stream on refusal: %v", stream)
	}

	var refused *RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("Offer error = %T (%v), want *RefusedError", err, err)
	}
	if refused.Reason != livedoor.RefuseBusy {
		t.Fatalf("RefusedError.Reason = %q, want %q", refused.Reason, livedoor.RefuseBusy)
	}
	wantText := "directroad: the host refused the offer: " + livedoor.RefuseBusy
	if refused.Error() != wantText {
		t.Fatalf("RefusedError.Error() = %q, want %q", refused.Error(), wantText)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Refused Offer took %v, expected prompt return", elapsed)
	}
}
