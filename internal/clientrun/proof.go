package clientrun

import (
	"bytes"
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
	now := r.cfg.Now().UnixMilli()
	if e.At <= now-clientproto.MaxAge.Milliseconds() || e.At > now+reqsign.Skew.Milliseconds() {
		return errors.New("queued call expired or is from the future")
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
	// Offline proofs are checked at the entry's signed creation time, not at
	// pickup: the HTTP signature's five-minute window must not defeat the queue.
	id, device, err := reqsign.Verify(req, e.Body, time.UnixMilli(e.At))
	if err != nil {
		return err
	}
	if id != reqsign.IDOf(r.cfg.Signer.IdentityKey()) || device != c.ID {
		return errors.New("queued call signed by another client or identity")
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
