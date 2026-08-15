package search

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Jina: the zero-key FETCH default. r.jina.ai/<url> returns the page at <url>
// rendered to markdown-ish text — headings, links and prose, none of the
// chrome — which is exactly the shape an agent can read and roughly a tenth
// the tokens of the HTML.
//
// It answers unauthenticated at about 20 requests a minute, which is why
// [Options.JinaKey] does NOT gate availability: the key raises the ceiling for
// someone who has one, and its absence costs nothing but rate. That is what
// makes this the fetch counterpart to DuckDuckGo — the rung that is always
// there — with exa-fetch taking over whenever an exa key is present.

var jinaBaseURL = "https://r.jina.ai/"

func init() { RegisterFetch(jina{}) }

type jina struct{ opts Options }

func (jina) Name() string { return "jina" }

// Available is unconditionally true: no key required. See the note above on
// why JinaKey does not appear here.
func (jina) Available(Options) bool { return true }

func (jina) Bind(opts Options) Fetcher { return jina{opts: opts} }

func (j jina) Fetch(ctx context.Context, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("jina: empty url")
	}
	// Checked here rather than left to the service: r.jina.ai concatenates
	// whatever it is given, so a relative path becomes a request for a page
	// on jina itself, and the agent gets a confusing 404 instead of "that is
	// not a URL".
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("jina: %q is not an http(s) url", target)
	}

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	// The target is appended verbatim, not escaped: r.jina.ai's route is the
	// literal URL, and percent-encoding it yields a request for a page whose
	// name contains %3A%2F%2F.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jinaBaseURL+target, nil)
	if err != nil {
		return "", fmt.Errorf("jina: build request: %w", err)
	}
	if key := strings.TrimSpace(j.opts.JinaKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := j.opts.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("jina: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", fmt.Errorf("jina: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("jina: %s%s", resp.Status, detail(body))
	}

	// Returned whole. Capping is the RENDERER's job ([RenderFetch]) so that a
	// caller doing something other than prompting a model — a summariser, a
	// grep — is not handed a page someone already cut for a different budget.
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "", fmt.Errorf("jina: empty body for %s", target)
	}
	return text, nil
}
