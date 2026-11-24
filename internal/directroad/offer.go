package directroad

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/pion/webrtc/v4"
)

// RefusedError is returned by Offer when the host answers the offer with a
// refuse signal.
type RefusedError struct {
	Reason string
}

// Error formats the refusal reason with the fixed package prefix.
func (e *RefusedError) Error() string {
	return "directroad: the host refused the offer: " + e.Reason
}

// Offer runs the viewer side of a direct-road attempt. It creates an offer,
// exchanges signalling over s and deliver until the pre-negotiated data
// channel opens and is detached, and returns the resulting byte stream.
//
// ctx bounds the connection attempt only; cancelling ctx after Offer succeeds
// does not close the returned stream.
func Offer(ctx context.Context, cfg Config, s livedoor.Signaller, deliver <-chan []byte) (io.ReadWriteCloser, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	id, err := newAttemptID()
	if err != nil {
		return nil, err
	}

	sess, err := newPCSession(id, cfg, s)
	if err != nil {
		return nil, err
	}

	offerDesc, err := sess.pc.CreateOffer(nil)
	if err != nil {
		sess.abort()
		return nil, err
	}
	if err := sess.pc.SetLocalDescription(offerDesc); err != nil {
		sess.abort()
		return nil, err
	}
	if err := sendOffer(s, id, offerDesc.SDP); err != nil {
		sess.abort()
		return nil, err
	}

	failAttempt := func(cause error) (io.ReadWriteCloser, error) {
		_ = sendEnd(s, id)
		sess.abort()
		return nil, cause
	}

	answerApplied := false
	for {
		select {
		case stream := <-sess.openCh:
			return stream, nil
		case err := <-sess.errCh:
			return failAttempt(err)
		case <-ctx.Done():
			return failAttempt(fmt.Errorf("directroad: offer timed out: %w", ctx.Err()))
		case payload, ok := <-deliver:
			if !ok {
				return failAttempt(errors.New("directroad: signal delivery channel closed"))
			}
			sig, err := livedoor.DecodeSignal(payload)
			if err != nil {
				// Payloads that do not decode are ignored here because the
				// underlying relay link polices wire framing.
				continue
			}

			switch sig.T {
			case livedoor.SignalRefuse:
				if sig.ID != "" && sig.ID != id {
					continue
				}
				sess.abort()
				return nil, &RefusedError{Reason: sig.Reason}

			case livedoor.SignalEnd:
				if sig.ID != id {
					continue
				}
				sess.abort()
				return nil, errors.New("directroad: host ended the attempt")

			case livedoor.SignalAnswer:
				if sig.ID != id {
					continue
				}
				if sig.SDP == "" {
					return failAttempt(errors.New("directroad: empty answer SDP"))
				}
				if answerApplied {
					continue
				}
				answerApplied = true
				err := sess.setRemoteDescription(webrtc.SessionDescription{
					Type: webrtc.SDPTypeAnswer,
					SDP:  sig.SDP,
				})
				if err != nil {
					return failAttempt(err)
				}

			case livedoor.SignalCandidate:
				if sig.ID != id {
					continue
				}
				if err := sess.addCandidate(sig); err != nil {
					return failAttempt(err)
				}
			}
		}
	}
}
