package relayserve

import (
	"encoding/hex"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/clientproto"

	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// Queue limits bound both the number of calls and each sealed payload.
const (
	MaxQueueBytes  = clientproto.MaxQueueBytes
	MaxClientQueue = clientproto.MaxClientQueue
)

type queuedCall struct {
	Number int64  `json:"number"`
	At     int64  `json:"at"`
	Method string `json:"method"`
	Target string `json:"target"`
	Sealed []byte `json:"sealed"`
}

type queueEntry struct {
	queuedCall
	ClaimedBy   string `json:"claimed_by,omitempty"`
	Signature   string `json:"signature"`
	Cert        string `json:"cert"`
	Identity    string `json:"identity"`
	RequestTime string `json:"request_time"`
	RequestURI  string `json:"request_uri"`
	Body        []byte `json:"body"`
}

func appendQueue(c fabricCall) (any, error) {
	if err := c.target(true); err != nil {
		return nil, err
	}
	var in queuedCall
	if err := decodeFabric(c.body, &in); err != nil {
		return nil, err
	}
	if err := c.validateEntry(in); err != nil {
		return nil, err
	}
	s, err := c.load()
	if err != nil {
		return nil, err
	}
	if in.Number <= 0 || in.Number != s.LastNumber+1 {
		return nil, badFabric(409, "sequence")
	}
	if len(s.Queue) >= MaxClientQueue {
		return nil, badFabric(429, "full")
	}
	h := c.r.Header
	s.Queue = append(s.Queue, queueEntry{queuedCall: in, Signature: h.Get(reqsign.HeaderSig), Cert: h.Get(reqsign.HeaderCert), Identity: h.Get(reqsign.HeaderIdentity), RequestTime: h.Get(reqsign.HeaderTime), RequestURI: c.r.URL.RequestURI(), Body: c.body})
	s.LastNumber = in.Number
	return nil, c.save(s)
}

func (c fabricCall) validateEntry(in queuedCall) error {
	if len(in.Sealed) > MaxQueueBytes {
		return badFabric(413, "too_large")
	}
	if len(in.Sealed) == 0 || !fabricName.MatchString(in.Method) {
		return badFabric(400, "bad_request")
	}
	if !slices.Contains(c.listing.Devices[c.dev].Methods, in.Method) {
		return badFabric(403, "method_denied")
	}
	if in.Target != "any" {
		d, ok := c.listing.Devices[in.Target]
		if !fabricDevice.MatchString(in.Target) || !ok || d.Kind == "client" || d.Revoked {
			return badFabric(400, "bad_request")
		}
	}
	now := c.f.ns.now().UnixMilli()
	if in.At <= now-FabricMaxAge.Milliseconds() || in.At > now+reqsign.Skew.Milliseconds() {
		return badFabric(400, "stale")
	}
	return nil
}

func listQueue(c fabricCall) (any, error) {
	if err := c.target(false); err != nil {
		return nil, err
	}
	if len(c.body) != 0 {
		return nil, badFabric(400, "bad_request")
	}
	s, err := c.load()
	if err != nil {
		return nil, err
	}
	entries := []queueEntry{}
	for _, e := range s.Queue {
		owner, _, _ := strings.Cut(e.ClaimedBy, ":")
		if (e.Target == "any" || e.Target == c.dev) && (owner == "" || owner == c.dev) {
			// Ownership tokens stay private to the original claiming process.
			e.ClaimedBy = owner
			entries = append(entries, e)
		}
	}
	return struct {
		Entries []queueEntry `json:"entries"`
	}{entries}, nil
}

func claimQueue(c fabricCall) (any, error) { return changeQueue(c, false) }
func ackQueue(c fabricCall) (any, error)   { return changeQueue(c, true) }

func changeQueue(c fabricCall, ack bool) (any, error) {
	if err := c.target(false); err != nil {
		return nil, err
	}
	token := ""
	if len(c.body) != 0 {
		var claim struct {
			Token string `json:"token"`
		}
		if ack {
			return nil, badFabric(400, "bad_request")
		}
		if err := decodeFabric(c.body, &claim); err != nil {
			return nil, err
		}
		decoded, err := hex.DecodeString(claim.Token)
		if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != claim.Token {
			return nil, badFabric(400, "bad_request")
		}
		token = claim.Token
	}
	text := c.r.PathValue("number")
	number, err := strconv.ParseInt(text, 10, 64)
	if err != nil || number <= 0 || strconv.FormatInt(number, 10) != text {
		return nil, badFabric(400, "bad_request")
	}
	s, err := c.load()
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(s.Queue, func(e queueEntry) bool { return e.Number == number })
	if index < 0 {
		return nil, badFabric(404, "not_found")
	}
	e := &s.Queue[index]
	if e.Target != "any" && e.Target != c.dev {
		return nil, wireauth.ErrUnauthorized
	}
	owner, _, _ := strings.Cut(e.ClaimedBy, ":")
	claim := c.dev
	if token != "" {
		claim += ":" + token
	}
	if (!ack && e.ClaimedBy != "" && (token == "" || e.ClaimedBy != claim)) || (ack && owner != c.dev) {
		return nil, badFabric(409, "claimed")
	}
	if ack {
		s.Queue = slices.Delete(s.Queue, index, index+1)
	} else {
		e.ClaimedBy = claim
	}
	return nil, c.save(s)
}

func (f *fabric) mountQueue(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/clients/{device}/queue", f.serve(appendQueue))
	mux.HandleFunc("GET /v1/clients/{device}/queue", f.serve(listQueue))
	mux.HandleFunc("POST /v1/clients/{device}/queue/{number}/claim", f.serve(claimQueue))
	mux.HandleFunc("DELETE /v1/clients/{device}/queue/{number}", f.serve(ackQueue))
}
