package directroad

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"

	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/pion/webrtc/v4"
)

// attemptIDBytes is the byte length of the random attempt identifier (8 bytes
// encode to 16 lowercase hexadecimal digits).
const attemptIDBytes = 8

// newAttemptID returns 16 lowercase hexadecimal digits from crypto/rand so
// every direct-road attempt carries a fresh identifier on the relay link.
func newAttemptID() (string, error) {
	var raw [attemptIDBytes]byte
	if _, err := io.ReadFull(rand.Reader, raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// sendSignal encodes sig with livedoor.EncodeSignal (enforcing
// livedoor.SignalMax) and hands it to the relay Signaller.
func sendSignal(s livedoor.Signaller, sig livedoor.Signal) error {
	if s == nil {
		return errors.New("directroad: nil signaller")
	}
	sig.V = livedoor.SignalVersion
	payload, err := livedoor.EncodeSignal(sig)
	if err != nil {
		return err
	}
	return s.Send(payload)
}

// sendOffer sends a viewer offer SDP for attempt id.
func sendOffer(s livedoor.Signaller, id, sdp string) error {
	return sendSignal(s, livedoor.Signal{
		T:   livedoor.SignalOffer,
		ID:  id,
		SDP: sdp,
	})
}

// sendAnswer sends a host answer SDP for attempt id.
func sendAnswer(s livedoor.Signaller, id, sdp string) error {
	return sendSignal(s, livedoor.Signal{
		T:   livedoor.SignalAnswer,
		ID:  id,
		SDP: sdp,
	})
}

// sendCandidate sends one trickled local ICE candidate, or an end-of-candidates
// marker when cand is nil.
func sendCandidate(s livedoor.Signaller, id string, cand *webrtc.ICECandidate) error {
	if cand == nil {
		return sendSignal(s, livedoor.Signal{
			T:   livedoor.SignalCandidate,
			ID:  id,
			End: true,
		})
	}
	init := cand.ToJSON()
	mid := ""
	if init.SDPMid != nil {
		mid = *init.SDPMid
	}
	idx := 0
	if init.SDPMLineIndex != nil {
		idx = int(*init.SDPMLineIndex)
	}
	return sendSignal(s, livedoor.Signal{
		T:   livedoor.SignalCandidate,
		ID:  id,
		C:   init.Candidate,
		Mid: mid,
		Idx: idx,
	})
}

// sendEnd best-effort notifies the peer that attempt id has been abandoned.
func sendEnd(s livedoor.Signaller, id string) error {
	return sendSignal(s, livedoor.Signal{
		T:  livedoor.SignalEnd,
		ID: id,
	})
}

// candidateInitFromSignal converts a received cand signal into pion's
// ICECandidateInit, returning false for an end-of-candidates marker.
func candidateInitFromSignal(sig livedoor.Signal) (webrtc.ICECandidateInit, bool) {
	if sig.End || sig.C == "" {
		return webrtc.ICECandidateInit{}, false
	}
	mid := sig.Mid
	var idx uint16
	if sig.Idx >= 0 && sig.Idx <= 0xffff {
		idx = uint16(sig.Idx)
	}
	return webrtc.ICECandidateInit{
		Candidate:     sig.C,
		SDPMid:        &mid,
		SDPMLineIndex: &idx,
	}, true
}
