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

	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func clientPath(id string) string { return "/v1/clients/" + url.PathEscape(id) }

func (r *Runner) request(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	var err error
	if in != nil {
		body, err = json.Marshal(in)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(r.cfg.Relay, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	reqsign.Sign(req, body, r.cfg.Signer, r.cfg.Now())
	resp, err := r.cfg.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return fmt.Errorf("client relay request: HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}
