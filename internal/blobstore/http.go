package blobstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// HTTP is a Store that lives behind the store wire (contract §3). Every
// request is signed, so the far side knows whose namespace it is.
type HTTP struct {
	base string
	sign wireauth.Sign
	hc   *http.Client
}

// NewHTTP returns a client for the store wire at base (no trailing path).
// A nil hc means http.DefaultClient.
func NewHTTP(base string, sign wireauth.Sign, hc *http.Client) *HTTP {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &HTTP{base: strings.TrimRight(base, "/"), sign: sign, hc: hc}
}

// PutFrame implements Store.
func (c *HTTP) PutFrame(ctx context.Context, frame []byte) (FrameID, error) {
	var out putAnswer
	if err := c.doJSON(ctx, http.MethodPost, pathFrames, frame, &out); err != nil {
		return "", err
	}
	return out.Frame, nil
}

// Get implements Store. The id is checked here as well as on the server,
// because it becomes part of a URL and a hostile one must not rewrite the path.
func (c *HTTP) Get(ctx context.Context, rid string) ([]byte, error) {
	if err := checkGet(rid); err != nil {
		return nil, err
	}
	return c.do(ctx, http.MethodGet, pathObjects+rid, nil)
}

// Has implements Store.
func (c *HTTP) Has(ctx context.Context, rids []string) ([]bool, error) {
	if rids == nil {
		rids = []string{}
	}
	var out hasAnswer
	if err := c.doJSON(ctx, http.MethodPost, pathHas, mustJSON(hasRequest{Rids: rids}), &out); err != nil {
		return nil, err
	}
	return out.Have, nil
}

// Stats asks the server for its own count of this identity's requests.
func (c *HTTP) Stats(ctx context.Context) (Counts, error) {
	var out statsBody
	err := c.doJSON(ctx, http.MethodGet, pathStats, nil, &out)
	return Counts(out), err
}

// Counts is the server's tally for one identity since it started.
type Counts struct {
	Puts, Gets, Has, BytesIn, BytesOut int64
}

func (c *HTTP) doJSON(ctx context.Context, method, path string, body []byte, into any) error {
	raw, err := c.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("%w: unreadable answer from %s", ErrUnreachable, path)
	}
	return nil
}

// do sends one signed request and answers the body of a 200, or the error the
// answer names. It never retries: a skew refusal must reach the person once.
func (c *HTTP) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	c.sign(req, body)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, MaxFrame+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if resp.StatusCode == http.StatusOK {
		return answer, nil
	}
	return nil, answerError(resp.StatusCode, answer)
}

// answerError turns a non-200 answer into an error: the code the server named,
// else a 5xx as "unreachable" (the server is not doing its job now, so
// callers degrade), else a plain error carrying the status.
func answerError(status int, answer []byte) error {
	var e errBody
	_ = json.Unmarshal(answer, &e)
	if named := errOf(e.Err); named != nil {
		return fmt.Errorf("%w (%d)", named, status)
	}
	if status >= 500 {
		return fmt.Errorf("%w: server answered %d", ErrUnreachable, status)
	}
	return fmt.Errorf("blobstore: server refused with %d %q", status, e.Err)
}
