// Package client is a [factory.Seam] over the factory's /v1 API. The desk
// keeps calling the seam; this package is what it is given when the floor
// is a server.
//
// Doors the wire does not carry stay nil ([api.TUIOnly]), so the desk draws
// no key for them. A refused call's error is the server's own sentence.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// Client talks to one factory. It keeps no copy of the floor: every read
// is a request, and [Subscription] only says that a read is due.
type Client struct {
	base   string
	token  string
	http   *http.Client
	seenMu sync.Mutex
	seen   map[int]api.Job
}

// Open returns a client for the factory at base. base is the origin, such
// as http://127.0.0.1:7420. token is the bearer access token; leave it
// empty for a loopback factory, which does not ask.
func Open(base, token string) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// The address is the factory the person named. An ambient proxy would
	// carry the access token to whatever HTTP_PROXY points at.
	transport.Proxy = nil
	return &Client{
		base:  strings.TrimRight(strings.TrimSpace(base), "/"),
		token: token,
		// Timeout stays zero because Subscribe holds one request open for
		// the life of the stream. Load has no context of its own.
		http: &http.Client{Transport: transport, CheckRedirect: refuseRedirect},
	}
}

func refuseRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

// Seam is the doors the wire carries. The same client answers every call.
func (c *Client) Seam() factory.Seam {
	return factory.Seam{
		Load:          c.load,
		Launch:        c.launch,
		Stop:          c.stop,
		Hold:          c.hold,
		Dismiss:       c.dismiss,
		Answer:        c.answer,
		Steer:         c.steer,
		SignOff:       c.signOff,
		SendBack:      c.sendBack,
		Reverify:      c.reverify,
		New:           c.make,
		Repos:         c.repos,
		SetRepos:      c.setRepos,
		GitHub:        c.github,
		ConnectGitHub: c.connect,
		Say:           c.say,
		Mark:          c.mark,
	}
}

// load is GET /v1/queue plus GET /v1/jobs/{id} for each row, rebuilt through
// the codec. The queue already carries full jobs today; the second read is
// what a later summary queue still has to satisfy, and a missing job fails
// the whole read so the desk never folds a torn floor.
func (c *Client) load() (factory.Snapshot, error) {
	var q api.Queue
	if err := c.call(context.Background(), http.MethodGet, "/v1/queue", nil, &q); err != nil {
		return factory.Snapshot{}, err
	}
	jobs := make([]api.Job, len(q.Jobs))
	for i, row := range q.Jobs {
		job, err := c.oneJob(row.ID)
		if err != nil {
			return factory.Snapshot{}, err
		}
		jobs[i] = job
	}
	c.seenMu.Lock()
	c.seen = make(map[int]api.Job, len(jobs))
	for _, job := range jobs {
		c.seen[job.ID] = job
	}
	c.seenMu.Unlock()
	return api.ToSnapshot(q, jobs), nil
}

func (c *Client) oneJob(id int) (api.Job, error) {
	var body struct {
		api.Job
	}
	err := c.call(context.Background(), http.MethodGet, "/v1/jobs/"+strconv.Itoa(id), nil, &body)
	return body.Job, err
}

func (c *Client) repos(ctx context.Context) ([]string, []factory.RepoInfo, error) {
	var body struct {
		Watched   []string   `json:"watched"`
		Available []repoWire `json:"available"`
	}
	if err := c.call(ctx, http.MethodGet, "/v1/repos", nil, &body); err != nil {
		return nil, nil, err
	}
	available := make([]factory.RepoInfo, len(body.Available))
	for i, r := range body.Available {
		available[i] = factory.RepoInfo{
			Full: r.Full, Name: r.Name, Owner: r.Owner, Private: r.Private,
			Pushed: r.Pushed, Open: r.Open, Dir: r.Checkout,
		}
	}
	return body.Watched, available, nil
}

// repoWire is the repository list as /v1/repos says it. Checkout is the
// directory the seam calls Dir.
type repoWire struct {
	Full     string    `json:"full"`
	Name     string    `json:"name"`
	Owner    string    `json:"owner"`
	Private  bool      `json:"private"`
	Pushed   time.Time `json:"pushed"`
	Open     int       `json:"open"`
	Checkout string    `json:"checkout,omitempty"`
}

func (c *Client) github(ctx context.Context) (factory.GitHubLink, error) {
	var body struct {
		Login string `json:"login"`
		Via   string `json:"via"`
	}
	err := c.call(ctx, http.MethodGet, "/v1/github", nil, &body)
	return factory.GitHubLink{Login: body.Login, Via: body.Via}, err
}

func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	c.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return sentence(raw)
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) authorize(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// sentence is the server's {error} text. The desk prints the error as the
// person reads it, so the status code stays off the sentence.
func sentence(raw []byte) error {
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(raw), &body) == nil && body.Error != "" {
		return errors.New(body.Error)
	}
	return errors.New("the factory refused")
}

// seenJob is the version the desk last read, not a fresh head fetched while
// approving. Re-reading here would silently approve work the person never saw.
func (c *Client) seenJob(id int) api.Job {
	c.seenMu.Lock()
	defer c.seenMu.Unlock()
	return c.seen[id]
}
