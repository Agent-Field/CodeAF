package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	maxMediaResponseBytes = 128 << 20
	maxVideoResponseBytes = 512 << 20

	defaultVideoPollInitial = 5 * time.Second
	defaultVideoPollMaximum = 20 * time.Second
	defaultVideoTimeout     = 10 * time.Minute
)

var ErrVideoTimeout = errors.New("video generation timed out")

// ImageRequest is OpenRouter's non-streaming image generation request.
//
// InputReferences carries the SAME envelope the video endpoint takes and not a
// bare URL, because that is what /api/v1/images validates against: a plain
// string comes back as `{"expected":"object","path":["input_references",0]}`
// with the whole render refused. One reference type for both endpoints, so the
// shape cannot drift out of step in one of them again.
type ImageRequest struct {
	Model           string           `json:"model"`
	Prompt          string           `json:"prompt"`
	N               int              `json:"n,omitempty"`
	Size            string           `json:"size,omitempty"`
	AspectRatio     string           `json:"aspect_ratio,omitempty"`
	OutputFormat    string           `json:"output_format"`
	InputReferences []ImageReference `json:"input_references,omitempty"`
}

type GeneratedImage struct {
	Base64    string `json:"b64_json"`
	MediaType string `json:"media_type"`
}

type ImageResponse struct {
	Data  []GeneratedImage `json:"data"`
	Usage *ai.Usage        `json:"usage,omitempty"`
}

type SpeechRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice,omitempty"`
	ResponseFormat string `json:"response_format"`
}

type SpeechResponse struct {
	Audio []byte
	Usage *ai.Usage
}

// ImageReference is the OpenRouter image-ref envelope, and it is ONE envelope
// for every endpoint that takes a picture as input: the image endpoint's
// input_references, and the video endpoint's first / last frames and style
// references. FrameType is a video-only slot and is omitted everywhere else.
type ImageReference struct {
	Type      string            `json:"type"`
	ImageURL  ImageReferenceURL `json:"image_url"`
	FrameType string            `json:"frame_type,omitempty"`
}

type ImageReferenceURL struct {
	URL string `json:"url"`
}

type VideoRequest struct {
	Model           string           `json:"model"`
	Prompt          string           `json:"prompt"`
	Duration        int              `json:"duration,omitempty"`
	Resolution      string           `json:"resolution,omitempty"`
	AspectRatio     string           `json:"aspect_ratio,omitempty"`
	FrameImages     []ImageReference `json:"frame_images,omitempty"`
	InputReferences []ImageReference `json:"input_references,omitempty"`
	GenerateAudio   *bool            `json:"generate_audio,omitempty"`
	Seed            *int             `json:"seed,omitempty"`
}

// NewImageReference is the one place the envelope is built, so no caller has to
// remember that the type word is "image_url" and that the URL lives one level
// down. Every reference on every endpoint goes through here.
func NewImageReference(url string) ImageReference {
	return ImageReference{Type: "image_url", ImageURL: ImageReferenceURL{URL: url}}
}

type VideoResponse struct {
	Video []byte
	Usage *ai.Usage
}

type videoJob struct {
	ID           string          `json:"id"`
	PollingURL   string          `json:"polling_url"`
	Status       string          `json:"status"`
	UnsignedURLs []string        `json:"unsigned_urls"`
	Usage        *ai.Usage       `json:"usage,omitempty"`
	Error        json.RawMessage `json:"error,omitempty"`
}

// MediaClient owns the non-chat OpenRouter endpoints while sharing the
// adapter's bearer key, attribution headers, timeout, and in-memory test seam.
type MediaClient struct {
	config Config
	http   *http.Client

	// Video generation is synchronous to callers but asynchronous on the wire.
	// These seams keep the production backoff honest while tests advance a fake
	// clock and never perform a real sleep.
	videoNow         func() time.Time
	videoWait        func(context.Context, time.Duration) error
	videoPollInitial time.Duration
	videoPollMaximum time.Duration
	videoTimeout     time.Duration
}

func NewMediaClient(config Config) (*MediaClient, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, fmt.Errorf("provider API key is required")
	}
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, fmt.Errorf("provider base URL is required")
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Transport: SharedTransport(), Timeout: config.Timeout}
	}
	return &MediaClient{
		config: config, http: client, videoNow: time.Now, videoWait: waitContext,
		videoPollInitial: defaultVideoPollInitial, videoPollMaximum: defaultVideoPollMaximum,
		videoTimeout: defaultVideoTimeout,
	}, nil
}

func (c *MediaClient) GenerateImage(ctx context.Context, request ImageRequest) (*ImageResponse, error) {
	if strings.TrimSpace(request.OutputFormat) == "" {
		request.OutputFormat = "png"
	}
	var response ImageResponse
	headers, err := c.postJSON(ctx, "/images", request, &response)
	if err != nil {
		return nil, err
	}
	if response.Usage == nil {
		response.Usage = usageFromHeaders(headers)
	}
	return &response, nil
}

func (c *MediaClient) Speak(ctx context.Context, request SpeechRequest) (*SpeechResponse, error) {
	if strings.TrimSpace(request.ResponseFormat) == "" {
		request.ResponseFormat = "mp3"
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal speech request: %w", err)
	}
	response, err := c.do(ctx, "/audio/speech", body)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxMediaResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read speech response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, apiError(response.StatusCode, payload)
	}
	return &SpeechResponse{Audio: payload, Usage: usageFromHeaders(response.Header)}, nil
}

// GenerateVideo submits one asynchronous OpenRouter job, waits through its
// pending/in-progress states, and downloads the first completed artifact. The
// method is deliberately synchronous: a leaf tool does not return a path until
// that path names a complete local video.
func (c *MediaClient) GenerateVideo(ctx context.Context, request VideoRequest) (*VideoResponse, error) {
	videoCtx, cancel := context.WithTimeout(ctx, c.videoTimeout)
	defer cancel()
	response, err := c.generateVideo(videoCtx, request)
	if errors.Is(videoCtx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("%w after %s", ErrVideoTimeout, c.videoTimeout)
	}
	return response, err
}

func (c *MediaClient) generateVideo(ctx context.Context, request VideoRequest) (*VideoResponse, error) {
	var job videoJob
	if _, err := c.postJSON(ctx, "/videos", request, &job); err != nil {
		return nil, err
	}
	if strings.TrimSpace(job.ID) == "" {
		return nil, fmt.Errorf("video submission returned no job id")
	}
	if strings.EqualFold(strings.TrimSpace(job.Status), "completed") {
		return c.downloadVideo(ctx, job)
	}

	pollingURL := strings.TrimSpace(job.PollingURL)
	if pollingURL == "" {
		pollingURL = c.mediaEndpoint("/videos/" + url.PathEscape(job.ID))
	} else {
		var err error
		pollingURL, err = c.resolveEndpoint(pollingURL)
		if err != nil {
			return nil, fmt.Errorf("resolve video polling URL: %w", err)
		}
	}

	started := c.videoNow()
	deadline := started.Add(c.videoTimeout)
	delay := c.videoPollInitial
	for {
		now := c.videoNow()
		if !now.Before(deadline) {
			return nil, fmt.Errorf("%w after %s", ErrVideoTimeout, c.videoTimeout)
		}
		if remaining := deadline.Sub(now); delay > remaining {
			delay = remaining
		}
		if err := c.videoWait(ctx, delay); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, fmt.Errorf("%w after %s", ErrVideoTimeout, c.videoTimeout)
			}
			return nil, fmt.Errorf("wait for video job: %w", err)
		}
		if !c.videoNow().Before(deadline) {
			return nil, fmt.Errorf("%w after %s", ErrVideoTimeout, c.videoTimeout)
		}

		if err := c.getJSON(ctx, pollingURL, &job); err != nil {
			return nil, err
		}
		switch strings.ToLower(strings.TrimSpace(job.Status)) {
		case "pending", "in_progress":
			if delay < c.videoPollMaximum {
				delay *= 2
				if delay > c.videoPollMaximum {
					delay = c.videoPollMaximum
				}
			}
		case "completed":
			return c.downloadVideo(ctx, job)
		case "failed", "cancelled", "expired":
			return nil, fmt.Errorf("video job %s: %s", job.Status, videoErrorDetail(job.Error))
		default:
			return nil, fmt.Errorf("video job returned unknown status %q", job.Status)
		}
	}
}

func (c *MediaClient) downloadVideo(ctx context.Context, job videoJob) (*VideoResponse, error) {
	if len(job.UnsignedURLs) == 0 || strings.TrimSpace(job.UnsignedURLs[0]) == "" {
		return nil, fmt.Errorf("completed video job returned no download URL")
	}
	endpoint, err := c.resolveEndpoint(job.UnsignedURLs[0])
	if err != nil {
		return nil, fmt.Errorf("resolve video download URL: %w", err)
	}
	// THE CREDENTIAL IS SCOPED TO THE HOST, not withheld from every host.
	//
	// The field is called unsigned_urls, and the first reading of that was "this
	// is object storage, never forward the bearer". It is half right: OpenRouter
	// answers a completed job with a SAME-ORIGIN content URL —
	// <base>/videos/<id>/content?index=0 — and that endpoint requires the bearer
	// like every other endpoint on the host. Withholding it there returned
	// 401 `No cookie auth credentials found`, which flowed into apiError and
	// landed a successful, already-paid render on the model's belt as a failure.
	//
	// So the rule is the narrow one the original comment was reaching for: send
	// the key iff the download host is the host we were configured to talk to,
	// and withhold it anywhere else. Genuinely off-site storage still never sees
	// it, and the router's own content endpoint works.
	response, err := c.doEndpoint(ctx, http.MethodGet, endpoint, nil, c.sameHostAsBase(endpoint))
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxVideoResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read video response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, apiError(response.StatusCode, payload)
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("video download was empty")
	}
	return &VideoResponse{Video: payload, Usage: job.Usage}, nil
}

func (c *MediaClient) postJSON(ctx context.Context, path string, request any, target any) (http.Header, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal media request: %w", err)
	}
	response, err := c.do(ctx, path, body)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxMediaResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read media response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, apiError(response.StatusCode, payload)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return nil, fmt.Errorf("decode media response: %w", err)
	}
	return response.Header.Clone(), nil
}

func (c *MediaClient) getJSON(ctx context.Context, endpoint string, target any) error {
	response, err := c.doEndpoint(ctx, http.MethodGet, endpoint, nil, true)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxMediaResponseBytes))
	if err != nil {
		return fmt.Errorf("read media response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return apiError(response.StatusCode, payload)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("decode media response: %w", err)
	}
	return nil
}

func (c *MediaClient) do(ctx context.Context, path string, body []byte) (*http.Response, error) {
	return c.doEndpoint(ctx, http.MethodPost, c.mediaEndpoint(path), body, true)
}

func (c *MediaClient) doEndpoint(ctx context.Context, method, endpoint string, body []byte, authenticated bool) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create media request: %w", err)
	}
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	if authenticated {
		request.Header.Set("Authorization", "Bearer "+c.config.APIKey)
		// Unconditional rather than gated on the router hint: every endpoint this
		// client speaks to is a router media endpoint, and an attribution header
		// is inert anywhere it is not read.
		ApplyAttribution(request.Header)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("execute media request: %w", err)
	}
	return response, nil
}

func (c *MediaClient) mediaEndpoint(path string) string {
	return strings.TrimSuffix(strings.TrimSpace(c.config.BaseURL), "/") + path
}

// sameHostAsBase reports whether an absolute URL points at the very host this
// client was configured with. It is the whole test behind [downloadVideo]'s
// credential decision, and it compares HOSTS — scheme and port included via
// url.Host — rather than prefixes: a prefix check would hand the key to
// `openrouter.ai.evil.example` for a base of `openrouter.ai`.
//
// A URL that will not parse answers false. An unreadable download URL is not a
// reason to guess in the direction that leaks a credential.
func (c *MediaClient) sameHostAsBase(endpoint string) bool {
	target, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return false
	}
	base, err := url.Parse(strings.TrimSpace(c.config.BaseURL))
	if err != nil {
		return false
	}
	return target.Host != "" && strings.EqualFold(target.Host, base.Host)
}

func (c *MediaClient) resolveEndpoint(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	reference, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if reference.IsAbs() {
		return reference.String(), nil
	}
	base, err := url.Parse(strings.TrimSuffix(strings.TrimSpace(c.config.BaseURL), "/") + "/")
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(raw, "/") {
		base.Path = "/"
	}
	return base.ResolveReference(reference).String(), nil
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func videoErrorDetail(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "the provider did not include an error detail"
	}
	var message string
	if json.Unmarshal(raw, &message) == nil && strings.TrimSpace(message) != "" {
		return strings.Join(strings.Fields(message), " ")
	}
	var object struct {
		Message string `json:"message"`
		Detail  string `json:"detail"`
		Code    string `json:"code"`
	}
	if json.Unmarshal(raw, &object) == nil {
		detail := strings.TrimSpace(object.Message)
		if detail == "" {
			detail = strings.TrimSpace(object.Detail)
		}
		if detail != "" && object.Code != "" {
			detail = object.Code + ": " + detail
		}
		if detail != "" {
			return strings.Join(strings.Fields(detail), " ")
		}
	}
	return strings.Join(strings.Fields(string(raw)), " ")
}

// OpenRouter's image response carries usage in JSON. Its raw speech response
// has no body slot for usage, so accept the cost header when the deployment
// supplies one; Calls is still accounted by the executor on every success.
func usageFromHeaders(headers http.Header) *ai.Usage {
	for _, name := range []string{"X-OpenRouter-Cost", "OpenRouter-Cost"} {
		raw := strings.TrimSpace(headers.Get(name))
		if raw == "" {
			continue
		}
		cost, err := strconv.ParseFloat(raw, 64)
		if err == nil && cost >= 0 {
			return &ai.Usage{Cost: &cost}
		}
	}
	return nil
}
