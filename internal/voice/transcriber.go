package voice

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// TranscriptionOptions are the OpenRouter audio fields the TUI may choose.
// Language and Temperature are omitted when unset.
type TranscriptionOptions struct {
	Model       string
	Language    string
	Temperature *float64
}

// Usage mirrors the endpoint's open-ended usage object while retaining the
// two values aforge consumes.
type Usage struct {
	Seconds float64 `json:"seconds"`
	Cost    float64 `json:"cost"`
}

type Transcript struct {
	Text  string `json:"text"`
	Usage Usage  `json:"usage"`
}

type Transcriber interface {
	Transcribe(context.Context, []byte, TranscriptionOptions) (Transcript, error)
}

// ClientConfig carries no attribution values, and that absence is the fix for
// a real drift: this package used to take a site URL and an app name of its
// own, wrote two headers out by hand from them, and so reported spoken input as
// a different, category-less app while every word a person typed was attributed
// correctly. The app is a constant in internal/provider now, and a transcriber
// stamps it the same way the chat adapter does.
type ClientConfig struct {
	APIKey     string
	BaseURL    string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type Client struct {
	config ClientConfig
	http   *http.Client
}

func NewClient(config ClientConfig) (*Client, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("voice transcription API key is required")
	}
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, errors.New("voice transcription base URL is required")
	}
	client := config.HTTPClient
	if client == nil {
		timeout := config.Timeout
		if timeout <= 0 {
			timeout = 2 * time.Minute
		}
		client = &http.Client{Timeout: timeout}
	}
	return &Client{config: config, http: client}, nil
}

func (c *Client) Transcribe(ctx context.Context, wav []byte, options TranscriptionOptions) (Transcript, error) {
	model := strings.TrimSpace(options.Model)
	if model == "" {
		return Transcript{}, errors.New("voice transcription model is required")
	}
	if len(wav) == 0 {
		return Transcript{}, errors.New("voice transcription audio is empty")
	}
	if len(wav) > MaxAudioBytes {
		return Transcript{}, errors.New("voice transcription audio exceeds 25MB")
	}
	type audioInput struct {
		Data   string `json:"data"`
		Format string `json:"format"`
	}
	type requestPayload struct {
		Model       string     `json:"model"`
		InputAudio  audioInput `json:"input_audio"`
		Language    string     `json:"language,omitempty"`
		Temperature *float64   `json:"temperature,omitempty"`
	}
	payload, err := json.Marshal(requestPayload{
		Model:      model,
		InputAudio: audioInput{Data: base64.StdEncoding.EncodeToString(wav), Format: "wav"},
		Language:   strings.TrimSpace(options.Language), Temperature: options.Temperature,
	})
	if err != nil {
		return Transcript{}, fmt.Errorf("encode voice transcription: %w", err)
	}
	endpoint := strings.TrimRight(c.config.BaseURL, "/") + "/audio/transcriptions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Transcript{}, fmt.Errorf("create voice transcription request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	// The whole attribution set, through the one helper that owns it. This used
	// to be two hand-written headers here and it drifted from the rest of the
	// binary the moment a third was added; see [provider.ApplyAttribution].
	provider.ApplyAttribution(request.Header)
	response, err := c.http.Do(request)
	if err != nil {
		return Transcript{}, fmt.Errorf("voice transcription: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Transcript{}, fmt.Errorf("read voice transcription: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Transcript{}, fmt.Errorf("voice transcription: %s", response.Status)
	}
	var transcript Transcript
	if err := json.Unmarshal(data, &transcript); err != nil {
		return Transcript{}, fmt.Errorf("decode voice transcription: %w", err)
	}
	transcript.Text = strings.TrimSpace(transcript.Text)
	return transcript, nil
}
