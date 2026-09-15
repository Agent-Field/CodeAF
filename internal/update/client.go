package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	internalenv "github.com/Agent-Field/codeaf/internal/env"
)

const (
	// CurlCommand is the independent installation road shown after a failed or
	// unavailable in-place update.
	CurlCommand = "curl -fsSL https://raw.githubusercontent.com/Agent-Field/codeaf/main/scripts/install.sh | bash"

	NoUpdateCheckEnv  = "CODEAF_NO_UPDATE_CHECK"
	GitHubAPIEnv      = "CODEAF_GITHUB_API"
	GitHubDownloadEnv = "CODEAF_GITHUB_DOWNLOAD"
)

const primaryRepository = "Agent-Field/codeaf"
const legacyRepository = "Agent-Field/aforge-v2" // legacy-name

// Client talks to the release API and release asset host.
type Client struct {
	HTTP         *http.Client
	APIBase      string
	DownloadBase string
	Token        string
	Revision     string
}

// NewClient builds the release client from the same mirror and token inputs as
// the shell installer. The timeout covers the whole request, including bodies.
func NewClient(revision string, timeout time.Duration) *Client {
	apiBase := strings.TrimRight(internalenv.Get(GitHubAPIEnv), "/")
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	downloadBase := strings.TrimRight(internalenv.Get(GitHubDownloadEnv), "/")
	if downloadBase == "" {
		downloadBase = "https://github.com"
	}
	token := internalenv.Value("GITHUB_TOKEN")
	if token == "" {
		token = internalenv.Value("GH_TOKEN")
	}
	return &Client{
		HTTP:         &http.Client{Timeout: timeout},
		APIBase:      apiBase,
		DownloadBase: downloadBase,
		Token:        token,
		Revision:     revision,
	}
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 3 * time.Second}
}

// requestClient keeps the caller's redirect decisions while enforcing the
// credential boundary the release API promises. Go considers another port on
// the same hostname safe for sensitive headers; the token here belongs to the
// exact API host, including its port, so that default is too broad.
func (c *Client) requestClient() *http.Client {
	base := c.httpClient()
	client := *base
	checkRedirect := base.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if checkRedirect != nil {
			if err := checkRedirect(request, via); err != nil {
				return err
			}
		} else if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if len(via) > 0 && !strings.EqualFold(request.URL.Host, via[0].URL.Host) {
			request.Header.Del("Authorization")
		}
		return nil
	}
	return &client
}

func (c *Client) userAgent() string {
	revision := strings.TrimSpace(c.Revision)
	if revision == "" {
		revision = "source"
	}
	return "codeaf/" + revision
}

type statusError struct {
	code int
	url  string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("%s answered HTTP %d", e.url, e.code)
}

func isStatus(err error, code int) bool {
	var status *statusError
	return errors.As(err, &status) && status.code == code
}

func (c *Client) get(ctx context.Context, rawURL, accept string, api bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", c.userAgent())
	// The token belongs only on API requests. Release downloads may redirect to
	// another host, and no credential follows them there.
	if api && strings.TrimSpace(c.Token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	}
	resp, err := c.requestClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, &statusError{code: resp.StatusCode, url: rawURL}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rawURL, err)
	}
	return body, nil
}

// Choice selects one release channel or one exact tag.
type Choice struct {
	Channel string
	Version string
}

// Release is one selected GitHub release and the repository that answered.
type Release struct {
	Tag        string
	Repository string
}

type apiRelease struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	CreatedAt   time.Time `json:"created_at"`
}

func (r apiRelease) stamp() time.Time {
	if !r.PublishedAt.IsZero() {
		return r.PublishedAt
	}
	return r.CreatedAt
}

// Select resolves a channel with the same timestamp law as the shell installer.
func (c *Client) Select(ctx context.Context, choice Choice) (Release, error) {
	if version := strings.TrimSpace(choice.Version); version != "" {
		if Kind(version) == "other" {
			return Release{}, fmt.Errorf("%q is not a codeaf release tag", version)
		}
		return Release{Tag: version, Repository: primaryRepository}, nil
	}
	channel := strings.TrimSpace(choice.Channel)
	if channel == "" {
		channel = "stable"
	}
	if channel != "stable" && channel != "rc" && channel != "dev" && channel != "staging" {
		return Release{}, fmt.Errorf("channel must be stable, rc, dev, or staging")
	}
	for index, repository := range []string{primaryRepository, legacyRepository} { // legacy-name
		release, err := c.selectRepository(ctx, repository, channel)
		if err == nil {
			return release, nil
		}
		if !isStatus(err, http.StatusNotFound) || index == 1 {
			return Release{}, err
		}
	}
	return Release{}, errors.New("no release repository answered")
}

func (c *Client) selectRepository(ctx context.Context, repository, channel string) (Release, error) {
	suffix := "releases/latest"
	if channel != "stable" {
		suffix = "releases?per_page=100"
	}
	rawURL := strings.TrimRight(c.APIBase, "/") + "/repos/" + repository + "/" + suffix
	body, err := c.get(ctx, rawURL, "application/vnd.github+json", true)
	if err != nil {
		return Release{}, err
	}
	if channel == "stable" {
		var row apiRelease
		if err := json.Unmarshal(body, &row); err != nil {
			return Release{}, fmt.Errorf("read the latest release: %w", err)
		}
		if Kind(row.TagName) != "stable" {
			return Release{}, fmt.Errorf("the latest release did not name a stable codeaf tag")
		}
		return Release{Tag: row.TagName, Repository: repository}, nil
	}
	var rows []apiRelease
	if err := json.Unmarshal(body, &rows); err != nil {
		return Release{}, fmt.Errorf("read the release list: %w", err)
	}
	var newest apiRelease
	for _, row := range rows {
		if Kind(row.TagName) != channel {
			continue
		}
		if newest.TagName == "" || row.stamp().After(newest.stamp()) {
			newest = row
		}
	}
	if newest.TagName == "" {
		return Release{}, fmt.Errorf("no %s build has been published yet", channel)
	}
	return Release{Tag: newest.TagName, Repository: repository}, nil
}

func (c *Client) assetURL(release Release, name string) string {
	base := strings.TrimRight(c.DownloadBase, "/")
	return base + "/" + release.Repository + "/releases/download/" + url.PathEscape(release.Tag) + "/" + url.PathEscape(name)
}
