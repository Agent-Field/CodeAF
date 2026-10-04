package blobstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// HTTP is a Store that lives behind the store wire (contract §3). Every
// request is signed, so the far side knows whose namespace it is.
type HTTP struct {
	base string
	sign wireauth.Sign
	hc   *http.Client

	// deadline sizes each request's time limit from its body; a test swaps it
	// to make a modest frame time out.
	deadline func(bodyBytes int) time.Duration
}

// NewHTTP returns a client for the store wire at base, which may carry a path prefix.
// A nil hc means http.DefaultClient.
func NewHTTP(base string, sign wireauth.Sign, hc *http.Client) *HTTP {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &HTTP{base: base, sign: sign, hc: hc, deadline: Deadline}
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

// GetMany implements Store: one request, answered by a frame holding a prefix of
// the objects asked for. The frame is decoded with the same checks as any
// other, and its objects must be that prefix, in order, or the answer is
// refused, so a relay cannot hand a taker objects it did not ask for.
func (c *HTTP) GetMany(ctx context.Context, rids []string) ([]Object, error) {
	if len(rids) == 0 {
		return nil, nil
	}
	if err := checkMany(rids); err != nil {
		return nil, err
	}
	frame, err := c.do(ctx, http.MethodPost, pathMany, mustJSON(getManyRequest{Rids: rids}))
	if err != nil {
		return nil, err
	}
	_, objects, err := Decode(frame)
	if err != nil || !isPrefix(objects, rids) {
		return nil, fmt.Errorf("%w: the answer to a GetMany is not the objects asked for", ErrUnreachable)
	}
	return objects, nil
}

// isPrefix says whether objects are exactly the first len(objects) of rids.
func isPrefix(objects []Object, rids []string) bool {
	if len(objects) > len(rids) {
		return false
	}
	for i, o := range objects {
		if o.RID != rids[i] {
			return false
		}
	}
	return true
}

// GetFrame implements Store. The id is checked here as well as on the server,
// because it becomes part of a URL and a hostile one must not rewrite the
// path. The answer is verified against the id — it is the hash of the bytes —
// so a wrong answer degrades like a missing one instead of failing a take at
// import.
func (c *HTTP) GetFrame(ctx context.Context, frame string) ([]byte, error) {
	if err := checkGet(frame); err != nil {
		return nil, err
	}
	b, err := c.do(ctx, http.MethodGet, pathFrames+"/"+frame, nil)
	if err != nil {
		return nil, err
	}
	if IDOf(b) != frame {
		return nil, fmt.Errorf("%w: frame %s answered bytes that are not it", ErrDamaged, frame)
	}
	return b, nil
}

// Locate implements Store: one request answering where each held rid lies.
func (c *HTTP) Locate(ctx context.Context, rids []string) (map[string]Location, error) {
	if rids == nil {
		rids = []string{}
	}
	var out locateAnswer
	if err := c.doJSON(ctx, http.MethodPost, pathLocate, mustJSON(locateRequest{Rids: rids}), &out); err != nil {
		return nil, err
	}
	return out.At, nil
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
	ctx, limit := startBudget(ctx, c.deadline, len(body))
	defer limit.done()
	req, err := http.NewRequestWithContext(ctx, method, wireauth.Endpoint(c.base, path), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	c.sign(req, body)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	limit.answering(resp.ContentLength)
	answer, err := io.ReadAll(io.LimitReader(resp.Body, MaxFrame+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if resp.StatusCode == http.StatusOK {
		return answer, nil
	}
	return nil, answerError(resp.StatusCode, resp.Header, answer)
}

// answerError turns a non-200 answer into an error: the code the server named,
// else a 5xx as "unreachable" (the server is not doing its job now, so
// callers degrade), else a plain error carrying the status. A refusal keeps the
// Retry-After the relay sent with it.
func answerError(status int, h http.Header, answer []byte) error {
	var e errBody
	_ = json.Unmarshal(answer, &e)
	if named := errOf(e.Err); named != nil {
		return Capped(wireauth.Wait(fmt.Errorf("%w (%d)", named, status), h), e.LimitBytes)
	}
	if status >= 500 {
		return fmt.Errorf("%w: server answered %d", ErrUnreachable, status)
	}
	return fmt.Errorf("blobstore: server refused with %d %q", status, e.Err)
}
