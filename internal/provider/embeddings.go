package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Embed sends one batch through the configured account without changing space
// on failure. Usage survives a malformed vector so paid work is still billed.
func (c *Client) Embed(ctx context.Context, model string, texts []string, dimensions int) ([][]float32, *ai.Response, error) {
	if len(texts) == 0 {
		return nil, nil, nil
	}
	if dimensions <= 0 || strings.TrimSpace(model) == "" {
		return nil, nil, errors.New("embedding model and dimensions are required")
	}
	if c.config.RouteGate != nil {
		if err := c.config.RouteGate(ctx, model, "placeembed"); err != nil {
			return nil, nil, err
		}
	}
	key, err := c.apiKeyNow()
	if err != nil {
		return nil, nil, err
	}
	body, err := json.Marshal(struct {
		Model      string   `json:"model"`
		Input      []string `json:"input"`
		Dimensions int      `json:"dimensions"`
		Encoding   string   `json:"encoding_format"`
	}{model, texts, dimensions, "float"})
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.config.BaseURL, "/")+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	c.applyRequestIdentity(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("embeddings returned HTTP %d", resp.StatusCode)
	}
	var answer struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Usage *ai.Usage `json:"usage"`
	}
	// The bound scales with the requested shape rather than trusting the response.
	if err := json.NewDecoder(io.LimitReader(resp.Body, int64(len(texts))*int64(dimensions)*32+65536)).Decode(&answer); err != nil {
		return nil, nil, err
	}
	usage := &ai.Response{Model: model, Usage: answer.Usage}
	if len(answer.Data) != len(texts) {
		return nil, usage, errors.New("embedding count does not match input")
	}
	out := make([][]float32, len(texts))
	for _, row := range answer.Data {
		if row.Index < 0 || row.Index >= len(out) || out[row.Index] != nil || len(row.Embedding) != dimensions {
			return nil, usage, errors.New("invalid embedding index or dimensions")
		}
		var squared float64
		for _, x := range row.Embedding {
			squared += float64(x) * float64(x)
		}
		if squared == 0 || math.IsNaN(squared) || math.IsInf(squared, 0) {
			return nil, usage, errors.New("embedding must have a finite nonzero norm")
		}
		norm := math.Sqrt(squared)
		for i, x := range row.Embedding {
			row.Embedding[i] = float32(float64(x) / norm)
		}
		out[row.Index] = row.Embedding
	}
	return out, usage, nil
}
