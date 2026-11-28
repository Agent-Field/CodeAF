package clientrun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func clientPath(id string) string { return "/v1/clients/" + url.PathEscape(id) }

type enrolledClientRow struct {
	Device string                 `json:"device"`
	Enroll clientproto.Enrollment `json:"enroll"`
}

func (r *Runner) fetchEnrollments(ctx context.Context) ([]enrolledClientRow, error) {
	var out struct {
		Clients []enrolledClientRow `json:"clients"`
	}
	err := r.request(ctx, "GET", "/v1/clients", nil, func(reader io.Reader) error {
		return json.NewDecoder(reader).Decode(&out)
	})
	return out.Clients, err
}

func (r *Runner) request(ctx context.Context, method, path string, in any, read func(io.Reader) error) error {
	_, err := r.requestHeader(ctx, method, path, in, read)
	return err
}

func (r *Runner) requestHeader(ctx context.Context, method, path string, in any, read func(io.Reader) error) (http.Header, error) {
	var body []byte
	var err error
	if in != nil {
		body, err = json.Marshal(in)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(r.cfg.Relay, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	reqsign.Sign(req, body, r.cfg.Signer, r.cfg.Now())
	resp, err := r.cfg.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return resp.Header, fmt.Errorf("client relay request: HTTP %d", resp.StatusCode)
	}
	if read == nil {
		return resp.Header, nil
	}
	// Original request bodies are returned as base64 beside the ciphertext.
	// Noncanonical certificate JSON can fill a server's header allowance too.
	// The queue is streamed, so the total bound need not be held in memory.
	const maxQueueReply = clientproto.MaxClientQueue * (http.DefaultMaxHeaderBytes + 3*clientproto.MaxFabricBody)
	return resp.Header, read(io.LimitReader(resp.Body, maxQueueReply))
}
