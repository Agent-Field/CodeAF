package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/id"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/permission"
)

const (
	webOutputMaxLines  = 2000
	webOutputMaxBytes  = 50 * 1024
	webOutputRetention = 7 * 24 * time.Hour
)

type webExecutionOptions struct {
	client      *http.Client
	exaURL      string
	parallelURL string
	outputDir   string
	version     string
	resolveHost func(context.Context, string) ([]net.IP, error)
}

// WithWebHostResolver injects redirect-boundary DNS resolution for hermetic
// tests. Production uses net.DefaultResolver.
func WithWebHostResolver(
	ctx context.Context, resolver func(context.Context, string) ([]net.IP, error),
) context.Context {
	options := webOptions(ctx)
	options.resolveHost = resolver
	return context.WithValue(ctx, webExecutionOptionsKey{}, options)
}

type webExecutionOptionsKey struct{}

// WithWebHTTPClient injects the HTTP seam used by webfetch and websearch.
// Production calls use http.DefaultClient, which follows redirects like the
// FetchHttpClient supplied at registry.ts:417.
func WithWebHTTPClient(ctx context.Context, client *http.Client) context.Context {
	options := webOptions(ctx)
	options.client = client
	return context.WithValue(ctx, webExecutionOptionsKey{}, options)
}

// WithWebSearchEndpoints redirects the provider MCP endpoints. It exists so
// tests can exercise the complete registry path without external network I/O.
func WithWebSearchEndpoints(ctx context.Context, exaURL, parallelURL string) context.Context {
	options := webOptions(ctx)
	options.exaURL = exaURL
	options.parallelURL = parallelURL
	return context.WithValue(ctx, webExecutionOptionsKey{}, options)
}

// WithWebOutputDir redirects the shared 50 KiB/2000-line truncation spill.
func WithWebOutputDir(ctx context.Context, directory string) context.Context {
	options := webOptions(ctx)
	options.outputDir = directory
	return context.WithValue(ctx, webExecutionOptionsKey{}, options)
}

func webOptions(ctx context.Context) webExecutionOptions {
	options, _ := ctx.Value(webExecutionOptionsKey{}).(webExecutionOptions)
	return options
}

func webClient(ctx context.Context) *http.Client {
	if client := webOptions(ctx).client; client != nil {
		return client
	}
	return http.DefaultClient
}

func webHostResolver(ctx context.Context) func(context.Context, string) ([]net.IP, error) {
	if resolver := webOptions(ctx).resolveHost; resolver != nil {
		return resolver
	}
	return func(ctx context.Context, host string) ([]net.IP, error) {
		return net.DefaultResolver.LookupIP(ctx, "ip", host)
	}
}

type redirectBlockedError struct{ destination string }

func (err *redirectBlockedError) Error() string {
	return "redirect destination blocked: " + err.destination
}

func redirectSafeWebClient(ctx context.Context, base *http.Client, originalURL string) *http.Client {
	client := *base
	resolve := webHostResolver(ctx)
	var originalOnce sync.Once
	var originalRestricted bool
	originalHost := ""
	if parsed, err := url.Parse(originalURL); err == nil {
		originalHost = parsed.Hostname()
	}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		originalOnce.Do(func() {
			originalRestricted = hostResolvesRestricted(request.Context(), resolve, originalHost)
		})
		if !originalRestricted && hostResolvesRestricted(request.Context(), resolve, request.URL.Hostname()) {
			return &redirectBlockedError{destination: request.URL.String()}
		}
		return nil
	}
	return &client
}

func hostResolvesRestricted(
	ctx context.Context,
	resolve func(context.Context, string) ([]net.IP, error),
	host string,
) bool {
	if host == "" {
		return false
	}
	addresses := []net.IP(nil)
	if literal := net.ParseIP(host); literal != nil {
		addresses = []net.IP{literal}
	} else {
		resolved, err := resolve(ctx, host)
		if err != nil {
			return false
		}
		addresses = resolved
	}
	metadata4 := net.ParseIP("169.254.169.254")
	metadata6 := net.ParseIP("fd00:ec2::254")
	for _, address := range addresses {
		if address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() ||
			address.IsLinkLocalMulticast() || address.Equal(metadata4) || address.Equal(metadata6) {
			return true
		}
	}
	return false
}

func webOutputDirectory(ctx context.Context) string {
	if directory := webOptions(ctx).outputDir; directory != "" {
		return directory
	}
	if directory := os.Getenv("XDG_DATA_HOME"); directory != "" {
		return filepath.Join(directory, "codeaf", "tool-output")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "codeaf", "tool-output")
}

type webTruncationMetadata struct {
	Truncated  bool   `json:"truncated"`
	OutputPath string `json:"outputPath,omitempty"`
}

func (r *Registry) truncateWebOutput(
	ctx context.Context,
	call steploop.ToolCall,
	output string,
) (string, webTruncationMetadata, error) {
	directory := webOutputDirectory(ctx)
	// TS starts a seven-day cleanup sweep with the truncation service. Go's
	// registry is lazy, so the equivalent sweep runs on first output handling
	// for each process/directory; cleanup failures remain non-fatal.
	cleanupWebOutput(directory, time.Now())
	// tool.ts:110-123 sends every ordinary tool result through truncate.output;
	// truncate.ts:16-18,86-141 defines the 2000-line/50-KiB head preview,
	// marker text, spill file, and metadata used here.
	maxLines, maxBytes := r.webOutputLimits()
	lines := strings.Split(output, "\n")
	if len(lines) <= maxLines && len([]byte(output)) <= maxBytes {
		return output, webTruncationMetadata{Truncated: false}, nil
	}

	preview := make([]string, 0, min(len(lines), maxLines))
	bytesUsed := 0
	hitBytes := false
	for index := 0; index < len(lines) && index < maxLines; index++ {
		size := len([]byte(lines[index]))
		if index > 0 {
			size++
		}
		if bytesUsed+size > maxBytes {
			hitBytes = true
			break
		}
		preview = append(preview, lines[index])
		bytesUsed += size
	}

	removed := len(lines) - len(preview)
	unit := "lines"
	if hitBytes {
		removed = len([]byte(output)) - bytesUsed
		unit = "bytes"
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", webTruncationMetadata{}, err
	}
	name, err := id.Ascending("tool")
	if err != nil {
		return "", webTruncationMetadata{}, err
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(output), 0o644); err != nil {
		return "", webTruncationMetadata{}, err
	}

	hint := "The tool call succeeded but the output was truncated. Full output saved to: " + path +
		"\nUse Grep to search the full content or Read with offset/limit to view specific sections."
	if r.agentHasTaskTool(ctx, call) {
		hint = "The tool call succeeded but the output was truncated. Full output saved to: " + path +
			"\nUse the Task tool to have explore agent process this file with Grep and Read (with offset/limit). Do NOT read the full file yourself - delegate to save context."
	}
	content := fmt.Sprintf("%s\n\n...%d %s truncated...\n\n%s", strings.Join(preview, "\n"), removed, unit, hint)
	return content, webTruncationMetadata{Truncated: true, OutputPath: path}, nil
}

func cleanupWebOutput(directory string, now time.Time) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	cutoffID, err := id.Create("tool", id.AscendingDirection, now.Add(-webOutputRetention).UnixMilli())
	if err != nil {
		return
	}
	cutoff, err := id.Timestamp(cutoffID)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "tool_") {
			continue
		}
		timestamp, err := id.Timestamp(entry.Name())
		if err != nil || timestamp >= cutoff {
			continue
		}
		_ = os.Remove(filepath.Join(directory, entry.Name()))
	}
}

func (r *Registry) webOutputLimits() (int, int) {
	maxLines, maxBytes := webOutputMaxLines, webOutputMaxBytes
	settings, err := r.settings()
	if err != nil {
		return maxLines, maxBytes
	}
	value, ok := settings["tool_output"].(map[string]any)
	if !ok {
		return maxLines, maxBytes
	}
	if number, ok := value["max_lines"].(float64); ok {
		maxLines = int(number)
	}
	if number, ok := value["max_bytes"].(float64); ok {
		maxBytes = int(number)
	}
	return maxLines, maxBytes
}

func (r *Registry) agentHasTaskTool(ctx context.Context, call steploop.ToolCall) bool {
	if call.Agent == "" || r.rules == nil {
		return false
	}
	rules := r.rules(ctx, call)
	return permission.Evaluate("task", "*", rules).Action != permission.ActionDeny
}

func statusCodeError(method, rawURL string, status int) error {
	return fmt.Errorf("StatusCode error (%d %s %s)", status, method, rawURL)
}

func transportError(method, rawURL string) error {
	return fmt.Errorf("Transport error (%s %s)", method, rawURL)
}

func decodeWebInput(raw json.RawMessage, destination any, known []string, required ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("input must be a JSON object: %w", err)
	}
	if fields == nil {
		return fmt.Errorf("input must be a JSON object")
	}
	for _, name := range required {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("missing required field %q", name)
		}
	}
	for _, name := range known {
		if value, ok := fields[name]; ok && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("field %q must not be null", name)
		}
	}
	// Effect Schema.Struct strips excess properties by default; unlike the
	// older Go tool validators, the frozen web schemas therefore accept them.
	if err := json.Unmarshal(raw, destination); err != nil {
		return fmt.Errorf("invalid input: %w", err)
	}
	return nil
}
