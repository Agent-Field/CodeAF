// Package clusterctl is the authenticated leadership door. Consensus hosts
// supply committed reads and completed transfers; this wire owns no voter list.
package clusterctl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/wireauth"
	"io"
	"net/http"
	"strings"
)

const Path = "/v2/cluster/control"

// NamesHeader opts into optional membership labels without changing older status replies.
const NamesHeader = "Codeaf-Cluster-Names"

var ErrUnavailable = errors.New(cs.LeadershipUnavailable)

// ErrUnsupported distinguishes an absent leadership capability from a lost
// quorum. Clients can refuse before saving an intent the relay cannot finish.
var ErrUnsupported = errors.New("this sync service does not offer cluster leadership yet; your saved chats are still here")

type CommittedRefusal struct {
	Code     cs.Code `json:"code"`
	Sequence uint64  `json:"sequence"`
}

func (e CommittedRefusal) Error() string {
	return "the leadership request could not complete; refresh cluster status and try again"
}

type Status struct {
	Members               []cs.MemberLabel `json:"members,omitempty"`
	Ready                 []uint64         `json:"ready,omitempty"`
	LeadershipUnavailable bool             `json:"leadership_unavailable,omitempty"`
	Cluster               string           `json:"cluster"`
	Self                  uint64           `json:"self"`
	Leader                uint64           `json:"leader"`
	Membership            cs.Membership    `json:"membership"`
	Configuration         cs.Configuration `json:"configuration"`
}

// Backend must authorize against committed state after a fresh quorum read.
// Lead returns only after the target is elected, commits its term and passes a
// new read barrier. Admission, a pending transfer and cached status are errors.
// A durably refused or superseded intent returns CommittedRefusal with its
// original sequence, allowing the client to acknowledge it before a new request.
type Backend interface {
	Status(context.Context, string) (Status, error)
	Lead(context.Context, cs.Command) (cs.LeadershipReceipt, error)
}

func Handler(auth wireauth.Authenticate, open func(string) (Backend, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != Path || r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, cs.MaxCommandBytes))
		if err != nil {
			http.Error(w, "request is too large", http.StatusRequestEntityTooLarge)
			return
		}
		identity, device, err := auth(r, body)
		if err != nil {
			http.Error(w, "this computer is not allowed to coordinate this cluster", http.StatusForbidden)
			return
		}
		backend, err := open(identity)
		if err != nil {
			http.Error(w, cs.LeadershipUnavailable, http.StatusServiceUnavailable)
			return
		}
		var result any
		if r.Method == http.MethodGet {
			if len(body) != 0 {
				http.Error(w, "unexpected request body", 400)
				return
			}
			result, err = backend.Status(r.Context(), device)
		} else {
			var c cs.Command
			c, err = cs.DecodeCommand(body)
			if err != nil || cs.ClientCommand(c) != nil || c.Actor != device || c.Change.RequestLeadership == nil {
				http.Error(w, "refresh cluster status before requesting leadership", 400)
				return
			}
			result, err = backend.Lead(r.Context(), c)
		}
		if err != nil {
			if errors.Is(err, ErrUnsupported) {
				http.Error(w, ErrUnsupported.Error(), http.StatusNotFound)
				return
			}
			var refused CommittedRefusal
			if errors.As(err, &refused) {
				b, _ := cs.Encode(refused)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write(b)
				return
			}
			http.Error(w, cs.LeadershipUnavailable, http.StatusServiceUnavailable)
			return
		}
		if status, ok := result.(Status); ok && r.Header.Get(NamesHeader) != "1" {
			status.Members = nil
			result = status
		}
		b, err := cs.Encode(result)
		if err != nil {
			http.Error(w, cs.LeadershipUnavailable, 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	})
}

type Client struct {
	Base string
	Sign wireauth.Sign
	HTTP *http.Client
}

func (c Client) request(ctx context.Context, method string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Base, "/")+Path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if c.Sign == nil {
		return wireauth.ErrUnauthorized
	}
	req.Header.Set(NamesHeader, "1")
	c.Sign(req, body)
	hc := http.Client{Timeout: ForwardTimeout}
	if c.HTTP != nil {
		hc = *c.HTTP
	}
	if hc.Timeout <= 0 || hc.Timeout > ForwardTimeout {
		hc.Timeout = ForwardTimeout
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, cs.MaxEvaluationBytes+1))
	if err != nil {
		return err
	}
	if len(b) > cs.MaxEvaluationBytes {
		return ErrUnavailable
	}
	if res.StatusCode == http.StatusConflict {
		var refused CommittedRefusal
		if cs.Decode(b, &refused) == nil {
			return refused
		}
	}
	if res.StatusCode != 200 {
		if res.StatusCode == http.StatusUnauthorized {
			var refusal struct {
				Err string `json:"err"`
			}
			if json.Unmarshal(b, &refusal) == nil && refusal.Err == "revoked" {
				return directory.ErrRevoked
			}
		}
		if res.StatusCode == 404 {
			return ErrUnsupported
		}
		return ErrUnavailable
	}
	return cs.Decode(b, out)
}
func (c Client) Status(ctx context.Context) (s Status, err error) {
	err = c.request(ctx, http.MethodGet, nil, &s)
	return
}
func (c Client) Lead(ctx context.Context, command cs.Command) (r cs.LeadershipReceipt, err error) {
	if command.Change.RequestLeadership == nil || cs.ClientCommand(command) != nil {
		return r, errors.New("a leadership request is required")
	}
	b, err := cs.EncodeCommand(command)
	if err != nil {
		return r, err
	}
	err = c.request(ctx, http.MethodPost, b, &r)
	if err == nil && (r.Node != command.Change.RequestLeadership.Node || r.Term == 0 || r.Barrier == 0 || len(r.Voters) != 3 || r.Revision <= command.Change.RequestLeadership.Revision) {
		err = ErrUnavailable
	}
	return
}
