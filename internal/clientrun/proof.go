package clientrun

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// Queued preserves the exact relay proof. Execution always re-decodes Body
// and compares it to the listing, so the relay cannot change signed metadata.
type Queued struct {
	Number      int64  `json:"number"`
	At          int64  `json:"at"`
	Method      string `json:"method"`
	Target      string `json:"target"`
	Sealed      []byte `json:"sealed"`
	Signature   string `json:"signature"`
	Cert        string `json:"cert"`
	Identity    string `json:"identity"`
	RequestTime string `json:"request_time"`
	RequestURI  string `json:"request_uri"`
	Body        []byte `json:"body"`
}

func (r *Runner) verify(c Client, e Queued) error {
	if e.Number <= 0 || len(e.Body) > clientproto.MaxFabricBody || len(e.Sealed) == 0 || len(e.Sealed) > clientproto.MaxQueueBytes {
		return errors.New("invalid queued call")
	}
	requestTime, err := strconv.ParseInt(e.RequestTime, 10, 64)
	if err != nil {
		return errors.New("invalid queued call request time")
	}
	// AUTHENTICITY IS A PURE FUNCTION OF THE SIGNED MATERIAL. The relay already
	// checked RequestTime against its clock at upload and enforced that At lies
	// within (RequestTime-MaxAge, RequestTime+Skew], so the computer evaluates
	// that same window against the signed RequestTime rather than its own clock.
	if e.At <= requestTime-clientproto.MaxAge.Milliseconds() || e.At > requestTime+reqsign.Skew.Milliseconds() {
		return errors.New("queued call timestamp is outside its signed request window")
	}
	req, err := http.NewRequest("POST", strings.TrimRight(r.cfg.Relay, "/")+clientPath(c.ID)+"/queue", nil)
	if err != nil {
		return err
	}
	if req.URL.RequestURI() != e.RequestURI {
		return errors.New("queued call proof has the wrong route")
	}
	req.Header.Set(reqsign.HeaderSig, e.Signature)
	req.Header.Set(reqsign.HeaderCert, e.Cert)
	req.Header.Set(reqsign.HeaderIdentity, e.Identity)
	req.Header.Set(reqsign.HeaderTime, e.RequestTime)
	// Verify the HTTP signature at the entry's own signed RequestTime so an
	// offline-composed call uploaded later or a computer with a slow clock is
	// never dropped as unauthentic.
	id, device, err := reqsign.Verify(req, e.Body, time.UnixMilli(requestTime))
	if err != nil {
		return err
	}
	r.mu.Lock()
	idPub := r.identityKeyLocked()
	r.mu.Unlock()
	if id != reqsign.IDOf(idPub) || device != c.ID {
		return errors.New("queued call signed by another client or identity")
	}
	if len(c.SignKey) == ed25519.PublicKeySize && c.ID != clientproto.DeviceIDOf(c.SignKey) {
		return errors.New("queued call client signing key does not match device id")
	}
	var signed struct {
		Number int64  `json:"number"`
		At     int64  `json:"at"`
		Method string `json:"method"`
		Target string `json:"target"`
		Sealed []byte `json:"sealed"`
	}
	if err := strictJSON(e.Body, &signed); err != nil {
		return err
	}
	if signed.Number != e.Number || signed.At != e.At || signed.Method != e.Method || signed.Target != e.Target || !bytes.Equal(signed.Sealed, e.Sealed) {
		return errors.New("queued metadata differs from its signed body")
	}
	return nil
}

func strictJSON(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing queued data")
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return errors.New("trailing queued data")
	}
	return nil
}

func queuePath(id string, number int64) string {
	return clientPath(id) + "/queue/" + strconv.FormatInt(number, 10)
}
