package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const maxMediaResponseBytes = 128 << 20

// ImageRequest is OpenRouter's non-streaming image generation request.
type ImageRequest struct {
	Model           string   `json:"model"`
	Prompt          string   `json:"prompt"`
	N               int      `json:"n,omitempty"`
	Size            string   `json:"size,omitempty"`
	AspectRatio     string   `json:"aspect_ratio,omitempty"`
	OutputFormat    string   `json:"output_format"`
	InputReferences []string `json:"input_references,omitempty"`
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
	Voice          string `json:"voice"`
	ResponseFormat string `json:"response_format"`
}

type SpeechResponse struct {
	Audio []byte
	Usage *ai.Usage
}

// MediaClient owns the two non-chat OpenRouter endpoints while sharing the
// adapter's bearer key, attribution headers, timeout, and in-memory test seam.
type MediaClient struct {
	config Config
	http   *http.Client
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
		client = &http.Client{Timeout: config.Timeout}
	}
	return &MediaClient{config: config, http: client}, nil
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

func (c *MediaClient) do(ctx context.Context, path string, body []byte) (*http.Response, error) {
	endpoint := strings.TrimSuffix(strings.TrimSpace(c.config.BaseURL), "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create media request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	if c.config.SiteURL != "" {
		request.Header.Set("HTTP-Referer", c.config.SiteURL)
	}
	if c.config.SiteName != "" {
		request.Header.Set("X-OpenRouter-Title", c.config.SiteName)
		request.Header.Set("X-Title", c.config.SiteName)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("execute media request: %w", err)
	}
	return response, nil
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
