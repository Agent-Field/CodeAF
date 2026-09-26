//go:build !windows

package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/netpolicy"
)

const (
	webFetchMaxResponseSize = 5 * 1024 * 1024
	webFetchDefaultTimeout  = 30 * time.Second
	webFetchMaxTimeout      = 120 * time.Second
	webFetchUserAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"
)

const webFetchSchema = `{
	"$schema":"https://json-schema.org/draft/2020-12/schema",
	"type":"object",
	"properties":{
		"url":{"type":"string","description":"The URL to fetch content from"},
		"format":{"type":"string","enum":["text","markdown","html"],"default":"markdown","description":"The format to return the content in (text, markdown, or html). Defaults to markdown."},
		"timeout":{"type":"number","description":"Optional timeout in seconds (max 120)"}
	},
	"required":["url"]
}`

type webFetchInput struct {
	URL     string   `json:"url"`
	Format  string   `json:"format,omitempty"`
	Timeout *float64 `json:"timeout,omitempty"`
}

type webFetchMetadata struct {
	Truncated  bool   `json:"truncated"`
	OutputPath string `json:"outputPath,omitempty"`
}

func validateWebFetch(raw json.RawMessage) error {
	var input webFetchInput
	if err := decodeWebInput(raw, &input, []string{"url", "format", "timeout"}, "url"); err != nil {
		return err
	}
	if input.Format != "" && input.Format != "text" && input.Format != "markdown" && input.Format != "html" {
		return fmt.Errorf("format must be one of text, markdown, or html")
	}
	return nil
}

func (r *Registry) executeWebFetch(ctx context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
	var input webFetchInput
	if err := decodeWebInput(call.Input, &input, []string{"url", "format", "timeout"}, "url"); err != nil {
		return steploop.ToolResult{}, err
	}
	if input.Format == "" {
		input.Format = "markdown"
	}
	// Only the literal http:// and https:// prefixes are accepted; http URLs
	// are fetched as given, not upgraded.
	if !strings.HasPrefix(input.URL, "http://") && !strings.HasPrefix(input.URL, "https://") {
		return steploop.ToolResult{}, fmt.Errorf("URL must start with http:// or https://")
	}
	// Refuse before the permission ask so the user is never prompted for a
	// request that cannot proceed. The transport wrap in webClient repeats
	// the refusal on every hop as a fast-fail backstop.
	if policy := netpolicy.Current(); policy.Restricted() {
		host := input.URL
		if parsed, parseErr := url.Parse(input.URL); parseErr == nil {
			host = parsed.Host
		}
		return steploop.ToolResult{}, policy.HostError(host)
	}
	metadata := map[string]any{"url": input.URL, "format": input.Format}
	if input.Timeout != nil {
		metadata["timeout"] = *input.Timeout
	}
	if err := r.ask(ctx, call, "webfetch", []string{input.URL}, metadata); err != nil {
		return steploop.ToolResult{}, err
	}

	timeout := webFetchDefaultTimeout
	if input.Timeout != nil {
		// Cap in floating-point space before converting to time.Duration so a
		// huge JSON number saturates at the maximum instead of overflowing the
		// Go duration.
		if *input.Timeout > webFetchMaxTimeout.Seconds() {
			timeout = webFetchMaxTimeout
		} else {
			timeout = time.Duration(*input.Timeout * float64(time.Second))
		}
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	response, err := executeWebFetchRequest(requestCtx, webClient(ctx), input)
	if err != nil {
		if requestCtx.Err() == context.DeadlineExceeded {
			return steploop.ToolResult{}, fmt.Errorf("Request timed out")
		}
		return steploop.ToolResult{}, err
	}
	defer response.Body.Close()

	// A response over 5 MiB is rejected both from a declared Content-Length
	// and after reading, before any content conversion.
	if declared := response.Header.Get("Content-Length"); declared != "" {
		if size, parseErr := strconv.ParseInt(declared, 10, 64); parseErr == nil && size > webFetchMaxResponseSize {
			return steploop.ToolResult{}, fmt.Errorf("Response too large (exceeds 5MB limit)")
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, webFetchMaxResponseSize+1))
	if err != nil {
		return steploop.ToolResult{}, err
	}
	if len(body) > webFetchMaxResponseSize {
		return steploop.ToolResult{}, fmt.Errorf("Response too large (exceeds 5MB limit)")
	}

	contentType := response.Header.Get("Content-Type")
	mime := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	title := input.URL + " (" + contentType + ")"
	// Images (every image/* type except SVG and fastbidsheet) are returned as
	// a data-URL attachment.
	if isWebFetchImage(mime) {
		attachments := []msgmodel.FilePart{{
			Type: "file", Mime: mime,
			URL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(body),
		}}
		return steploop.ToolResult{
			Title: title, Output: "Image fetched successfully",
			Metadata: rawMetadata(webFetchMetadata{Truncated: false}), Attachments: &attachments,
		}, nil
	}

	// Malformed UTF-8 is replaced rather than rejected, and no MIME type other
	// than images is treated specially.
	content := strings.ToValidUTF8(string(body), "\uFFFD")
	if strings.Contains(contentType, "text/html") {
		switch input.Format {
		case "markdown":
			content, err = convertWebHTMLToMarkdown(content)
		case "text":
			content, err = extractWebHTMLText(content)
		}
		if err != nil {
			return steploop.ToolResult{}, err
		}
	}
	output, truncation, err := r.truncateWebOutput(ctx, call, content)
	if err != nil {
		return steploop.ToolResult{}, err
	}
	return steploop.ToolResult{
		Title: title, Output: output,
		Metadata: rawMetadata(webFetchMetadata{
			Truncated: truncation.Truncated, OutputPath: truncation.OutputPath,
		}),
	}, nil
}

func executeWebFetchRequest(ctx context.Context, client *http.Client, input webFetchInput) (*http.Response, error) {
	// A direct GET with no robots.txt check. Redirects follow the client's
	// normal behaviour, bounded by redirectSafeWebClient.
	headers := map[string]string{
		"User-Agent":      webFetchUserAgent,
		"Accept":          acceptHeader(input.Format),
		"Accept-Language": "en-US,en;q=0.9",
	}
	do := func(userAgent string) (*http.Response, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, input.URL, nil)
		if err != nil {
			return nil, fmt.Errorf("InvalidUrl error (GET %s)", input.URL)
		}
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		request.Header.Set("User-Agent", userAgent)
		response, err := redirectSafeWebClient(ctx, client, input.URL).Do(request)
		if err != nil {
			// A policy refusal on a redirect hop comes back wrapped in
			// *url.Error; surface its no-retry framing instead of collapsing
			// it into a retryable-looking transport failure.
			var policyBlocked *netpolicy.BlockedError
			if errors.As(err, &policyBlocked) {
				return nil, policyBlocked
			}
			var blocked *redirectBlockedError
			if errors.As(err, &blocked) {
				return nil, transportError(http.MethodGet, blocked.destination)
			}
			return nil, transportError(http.MethodGet, input.URL)
		}
		return response, nil
	}

	response, err := do(headers["User-Agent"])
	if err != nil {
		return nil, err
	}
	// Only Cloudflare's explicit 403 challenge is retried, with an honest
	// senior-dev User-Agent. Other non-2xx responses are not retried.
	if response.StatusCode == http.StatusForbidden && response.Header.Get("cf-mitigated") == "challenge" {
		response.Body.Close()
		response, err = do("senior-dev")
		if err != nil {
			return nil, err
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, statusCodeError(http.MethodGet, input.URL, response.StatusCode)
	}
	return response, nil
}

func acceptHeader(format string) string {
	switch format {
	case "markdown":
		return "text/markdown;q=1.0, text/x-markdown;q=0.9, text/plain;q=0.8, text/html;q=0.7, */*;q=0.1"
	case "text":
		return "text/plain;q=1.0, text/markdown;q=0.9, text/html;q=0.8, */*;q=0.1"
	case "html":
		return "text/html;q=1.0, application/xhtml+xml;q=0.9, text/plain;q=0.8, text/markdown;q=0.7, */*;q=0.1"
	default:
		return "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"
	}
}

func isWebFetchImage(mime string) bool {
	return strings.HasPrefix(mime, "image/") && mime != "image/svg+xml" && mime != "image/vnd.fastbidsheet"
}
