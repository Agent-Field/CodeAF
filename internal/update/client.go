package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	internalenv "github.com/Agent-Field/codeaf/internal/env"
)

var installNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

const (
	// CurlCommand is the independent installation road shown at launch beside
	// /update and after a failed or unavailable in-place update. IT IS THE
	// SUPPORTED LINE, NOT THE SCRIPT UNDER IT: agentfield.ai/get/codeaf serves
	// scripts/install.sh from main, and the address is the one a person can
	// remember and the one every other place codeaf documents spells, so a
	// launch line and the README can never hand out two different roads.
	CurlCommand = "curl -fsSL https://agentfield.ai/get/codeaf | bash"

	NoUpdateCheckEnv  = "CODEAF_NO_UPDATE_CHECK"
	GitHubAPIEnv      = "CODEAF_GITHUB_API"
	GitHubDownloadEnv = "CODEAF_GITHUB_DOWNLOAD"
)

// CurlLine returns the independent installation road for the running file and
// release channel. The product remains codeaf; only the destination file name
// changes when a differently named executable asks for its own road back.
func CurlLine(executable, channel string) string {
	channel = strings.TrimSpace(channel)
	switch channel {
	case "dev", "staging", "rc", "stable":
	default:
		channel = "stable"
	}
	name := filepath.Base(strings.TrimSpace(executable))
	if len(name) >= 4 && strings.EqualFold(name[len(name)-4:], ".exe") {
		name = name[:len(name)-4]
	}
	if !installNamePattern.MatchString(name) {
		name = "codeaf"
	}
	if name == "devaf" {
		return "curl -fsSL https://agentfield.ai/get/devaf | bash"
	}
	if name == "stageaf" {
		return "curl -fsSL https://agentfield.ai/get/stageaf | bash"
	}
	address := "https://agentfield.ai/get/codeaf"
	if channel != "stable" {
		address += "/" + channel
	}
	line := "curl -fsSL " + address + " | "
	if name != "codeaf" {
		line += "CODEAF_INSTALL_NAME=" + name + " "
	}
	return line + "bash"
}

const (
	// CheckTimeout is the whole-exchange budget for a launch check and for
	// `codeaf update --check`. THE CHECK CLOCK NEVER COVERS AN INSTALL: a
	// release asset is allowed to make steady progress for much longer.
	CheckTimeout = 3 * time.Second

	networkSetupTimeout = 5 * time.Second
	apiRequestTimeout   = 10 * time.Second
	downloadStallWindow = 30 * time.Second
	downloadCeiling     = 15 * time.Minute
)

const primaryRepository = "Agent-Field/codeaf"
const legacyRepository = "Agent-Field/aforge-v2" // legacy-name

// Client talks to the release API and release asset host.
type Client struct {
	HTTP            *http.Client
	APIBase         string
	DownloadBase    string
	Token           string
	Revision        string
	CheckWindow     time.Duration
	APIWindow       time.Duration
	StallWindow     time.Duration
	DownloadCeiling time.Duration
}

// NewClient builds the release client from the same mirror and token inputs as
// the shell installer. The timeout belongs only to the small release check;
// every install request carries the clock for the particular thing it reads.
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
		HTTP:            defaultHTTP(),
		APIBase:         apiBase,
		DownloadBase:    downloadBase,
		Token:           token,
		Revision:        revision,
		CheckWindow:     timeout,
		APIWindow:       apiRequestTimeout,
		StallWindow:     downloadStallWindow,
		DownloadCeiling: downloadCeiling,
	}
}

// defaultHTTP bounds only the parts of a request that have made no progress.
// In particular, Client.Timeout stays zero: that field includes every body
// byte and was the three-second clock that cut a healthy 57 MB download off.
func defaultHTTP() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: networkSetupTimeout}).DialContext
	transport.TLSHandshakeTimeout = networkSetupTimeout
	transport.ResponseHeaderTimeout = apiRequestTimeout
	return &http.Client{Transport: transport}
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return defaultHTTP()
}

// requestClient keeps the caller's redirect decisions while enforcing the
// credential boundary the release API promises. Go considers another port on
// the same hostname safe for sensitive headers; the token here belongs to the
// exact API host, including its port, so that default is too broad.
func (c *Client) requestClient() *http.Client {
	base := c.httpClient()
	client := *base
	// EVERY CALL GETS ITS DEADLINE FROM ITS CONTEXT. An injected or reused
	// client must not smuggle a whole-body Client.Timeout back into downloads.
	client.Timeout = 0
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
	code     int
	resource string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("%s answered HTTP %d", e.resource, e.code)
}

func isStatus(err error, code int) bool {
	var status *statusError
	return errors.As(err, &status) && status.code == code
}

func (c *Client) checkWindow() time.Duration {
	if c != nil && c.CheckWindow > 0 {
		return c.CheckWindow
	}
	return CheckTimeout
}

func (c *Client) apiWindow() time.Duration {
	if c != nil && c.APIWindow > 0 {
		return c.APIWindow
	}
	return apiRequestTimeout
}

func (c *Client) stallWindow() time.Duration {
	if c != nil && c.StallWindow > 0 {
		return c.StallWindow
	}
	return downloadStallWindow
}

func (c *Client) downloadCeiling() time.Duration {
	if c != nil && c.DownloadCeiling > 0 {
		return c.DownloadCeiling
	}
	return downloadCeiling
}

// Check selects one release inside the short whole-exchange window shared by
// the launch notice and the terminal's explicit check. Installation calls
// Select directly, where each metadata request receives its own API window.
func (c *Client) Check(ctx context.Context, choice Choice) (Release, error) {
	window := c.checkWindow()
	check, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	release, err := c.Select(check, choice)
	if err != nil && ctx.Err() == nil && errors.Is(check.Err(), context.DeadlineExceeded) {
		return Release{}, fmt.Errorf("release API did not answer within %s", spellDuration(window))
	}
	return release, err
}

func (c *Client) get(ctx context.Context, rawURL, accept string, api bool, resource string) ([]byte, error) {
	requestContext, cancel := context.WithTimeout(ctx, c.apiWindow())
	defer cancel()
	req, err := http.NewRequestWithContext(requestContext, http.MethodGet, rawURL, nil)
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
		return nil, metadataRequestFailure(ctx, requestContext, err, resource, c.apiWindow())
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, &statusError{code: resp.StatusCode, resource: resource}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, metadataRequestFailure(ctx, requestContext, err, resource, c.apiWindow())
	}
	return body, nil
}

func metadataRequestFailure(parent, request context.Context, err error, resource string, window time.Duration) error {
	if parent.Err() != nil {
		if errors.Is(parent.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%s did not answer before this update stopped", resource)
		}
		return fmt.Errorf("the %s request was stopped", resource)
	}
	if errors.Is(request.Err(), context.DeadlineExceeded) || networkTimedOut(err) {
		return fmt.Errorf("%s did not answer within %s", resource, spellDuration(window))
	}
	return fmt.Errorf("%s could not be reached", resource)
}

func networkTimedOut(err error) bool {
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}

func spellDuration(duration time.Duration) string {
	switch {
	case duration%time.Minute == 0:
		minutes := duration / time.Minute
		unit := "minutes"
		if minutes == 1 {
			unit = "minute"
		}
		return fmt.Sprintf("%d %s", minutes, unit)
	case duration%time.Second == 0:
		return fmt.Sprintf("%d s", duration/time.Second)
	case duration%time.Millisecond == 0:
		return fmt.Sprintf("%d ms", duration/time.Millisecond)
	default:
		return duration.String()
	}
}

// Choice selects one release channel or one exact tag.
type Choice struct {
	Channel string
	Version string
	Running string
}

// Release is one selected GitHub release and the repository that answered.
type Release struct {
	Tag                string
	Repository         string
	PublishedAt        time.Time
	RunningPublishedAt time.Time
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
		release, err := c.selectRepository(ctx, repository, channel, strings.TrimSpace(choice.Running))
		if err == nil {
			return release, nil
		}
		if !isStatus(err, http.StatusNotFound) || index == 1 {
			return Release{}, err
		}
	}
	return Release{}, errors.New("no release repository answered")
}

func (c *Client) selectRepository(ctx context.Context, repository, channel, running string) (Release, error) {
	suffix := "releases/latest"
	if channel != "stable" {
		suffix = "releases?per_page=100"
	}
	rawURL := strings.TrimRight(c.APIBase, "/") + "/repos/" + repository + "/" + suffix
	body, err := c.get(ctx, rawURL, "application/vnd.github+json", true, "release API")
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
		return Release{Tag: row.TagName, Repository: repository, PublishedAt: row.stamp()}, nil
	}
	var rows []apiRelease
	if err := json.Unmarshal(body, &rows); err != nil {
		return Release{}, fmt.Errorf("read the release list: %w", err)
	}
	var newest apiRelease
	var runningPublished time.Time
	for _, row := range rows {
		if Kind(row.TagName) != channel {
			continue
		}
		if row.TagName == running {
			runningPublished = row.stamp()
		}
		if newest.TagName == "" || row.stamp().After(newest.stamp()) {
			newest = row
		}
	}
	if newest.TagName == "" {
		return Release{}, fmt.Errorf("no %s build has been published yet", channel)
	}
	return Release{
		Tag: newest.TagName, Repository: repository, PublishedAt: newest.stamp(),
		RunningPublishedAt: runningPublished,
	}, nil
}

func (c *Client) assetURL(release Release, name string) string {
	base := strings.TrimRight(c.DownloadBase, "/")
	return base + "/" + release.Repository + "/releases/download/" + url.PathEscape(release.Tag) + "/" + url.PathEscape(name)
}
