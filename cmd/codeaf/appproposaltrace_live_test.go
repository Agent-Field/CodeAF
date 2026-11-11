//go:build e2e

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptrace"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

type proposalLineTiming struct {
	Line       json.RawMessage `json:"line"`
	Rejection  string          `json:"rejection,omitempty"`
	Kind       string          `json:"kind"`
	ProviderMS float64         `json:"provider_ms"`
	AppMS      float64         `json:"app_ms"`
}

type proposalTiming struct {
	Attempts         int                  `json:"attempts"`
	SentAt           string               `json:"sent_at"`
	ShapeSize        int                  `json:"shape_size"`
	ShapeTeams       int                  `json:"shape_teams"`
	Scenario         string               `json:"scenario"`
	Run              int                  `json:"run"`
	RequestBytes     int                  `json:"request_bytes"`
	Reasoning        json.RawMessage      `json:"reasoning"`
	FirstByteMS      float64              `json:"first_byte_ms"`
	FirstBodyMS      float64              `json:"first_body_ms"`
	FirstReasoningMS float64              `json:"first_reasoning_ms"`
	FirstContentMS   float64              `json:"first_content_ms"`
	ReasoningChars   int                  `json:"reasoning_chars"`
	ReasoningTokens  int                  `json:"reasoning_tokens"`
	Lines            []proposalLineTiming `json:"lines"`
	DoneMS           float64              `json:"done_ms"`
	AskMS            float64              `json:"ask_ms"`
}

type proposalTrace struct {
	mu                                                                  sync.Mutex
	sent, firstByte, firstBody, firstReasoning, firstContent, done, ask time.Time
	wire                                                                []byte
	content                                                             string
	keys                                                                []string
	appTimes                                                            map[string]time.Time
	kinds                                                               []string
	providerLines                                                       []time.Time
	timing                                                              proposalTiming
}

func (p *proposalTrace) transport(base http.RoundTripper) http.RoundTripper {
	return roundTripper(func(req *http.Request) (*http.Response, error) {
		if req.GetBody == nil {
			return base.RoundTrip(req)
		}
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(body)
		body.Close()
		if err != nil {
			return nil, err
		}
		if !bytes.Contains(raw, []byte("Propose a crew")) {
			return base.RoundTrip(req)
		}
		var request struct {
			Reasoning json.RawMessage `json:"reasoning"`
		}
		_ = json.Unmarshal(raw, &request)
		p.mu.Lock()
		p.timing.Attempts++
		p.sent = time.Now()
		p.timing.RequestBytes = len(raw)
		p.timing.Reasoning = request.Reasoning
		p.mu.Unlock()
		trace := &httptrace.ClientTrace{
			WroteRequest:         func(httptrace.WroteRequestInfo) { p.mu.Lock(); p.sent = time.Now(); p.mu.Unlock() },
			GotFirstResponseByte: func() { p.mu.Lock(); p.firstByte = time.Now(); p.mu.Unlock() },
		}
		response, err := base.RoundTrip(req.WithContext(httptrace.WithClientTrace(req.Context(), trace)))
		if err == nil {
			response.Body = &proposalTraceBody{ReadCloser: response.Body, trace: p}
		}
		return response, err
	})
}

type proposalTraceBody struct {
	io.ReadCloser
	trace *proposalTrace
}

func (b *proposalTraceBody) Read(dst []byte) (int, error) {
	n, err := b.ReadCloser.Read(dst)
	if n > 0 {
		b.trace.read(dst[:n])
	}
	return n, err
}

func (p *proposalTrace) read(chunk []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if p.firstBody.IsZero() {
		p.firstBody = now
	}
	p.wire = append(p.wire, chunk...)
	for {
		i := bytes.IndexByte(p.wire, '\n')
		if i < 0 {
			return
		}
		line := bytes.TrimSpace(p.wire[:i])
		p.wire = p.wire[i+1:]
		data, ok := bytes.CutPrefix(line, []byte("data:"))
		if !ok {
			continue
		}
		data = bytes.TrimSpace(data)
		if string(data) == "[DONE]" {
			p.done = now
			p.contentLine(now)
			continue
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					Reasoning        string `json:"reasoning"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage struct {
				Details struct {
					Reasoning int `json:"reasoning_tokens"`
				} `json:"completion_tokens_details"`
			} `json:"usage"`
		}
		if json.Unmarshal(data, &event) != nil {
			continue
		}
		if event.Usage.Details.Reasoning > 0 {
			p.timing.ReasoningTokens = event.Usage.Details.Reasoning
		}
		for _, choice := range event.Choices {
			reason := choice.Delta.Reasoning + choice.Delta.ReasoningContent
			if reason != "" {
				if p.firstReasoning.IsZero() {
					p.firstReasoning = now
				}
				p.timing.ReasoningChars += len(reason)
			}
			if choice.Delta.Content == "" {
				continue
			}
			if p.firstContent.IsZero() {
				p.firstContent = now
			}
			p.content += choice.Delta.Content
			for strings.Contains(p.content, "\n") {
				line, rest, _ := strings.Cut(p.content, "\n")
				p.content = rest
				p.recordLine(line, now)
			}
		}
	}
}

func (p *proposalTrace) contentLine(now time.Time) {
	if strings.TrimSpace(p.content) != "" {
		p.recordLine(p.content, now)
		p.content = ""
	}
}
func (p *proposalTrace) recordLine(line string, now time.Time) {
	var item struct {
		T string `json:"t"`
	}
	if json.Unmarshal([]byte(line), &item) == nil && item.T != "" {
		if item.T == "shape" {
			var shape struct {
				Size  int
				Teams int
			}
			_ = json.Unmarshal([]byte(line), &shape)
			p.timing.ShapeSize = shape.Size
			p.timing.ShapeTeams = shape.Teams
		}
		p.keys = append(p.keys, strings.TrimSpace(line))
		p.kinds = append(p.kinds, item.T)
		p.providerLines = append(p.providerLines, now)
	}
}
func (p *proposalTrace) observe(event string, data any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if event == appwire.EventCrewProposalDelta {
		delta := data.(appwire.CrewProposalDelta)
		if p.appTimes == nil {
			p.appTimes = map[string]time.Time{}
		}
		p.appTimes[strings.TrimSpace(string(delta.Line))] = time.Now()
	}
	if opened, ok := data.(appwire.AskOpened); ok && opened.Ask.Subkind == appwire.SubkindCrewProposal {
		p.ask = time.Now()
	}
}
func (p *proposalTrace) snapshot() proposalTiming {
	p.mu.Lock()
	defer p.mu.Unlock()
	ms := func(at time.Time) float64 {
		if at.IsZero() {
			return -1
		}
		return float64(at.Sub(p.sent).Microseconds()) / 1000
	}
	result := p.timing
	result.SentAt = p.sent.UTC().Format(time.RFC3339Nano)
	result.FirstByteMS = ms(p.firstByte)
	result.FirstBodyMS = ms(p.firstBody)
	result.FirstReasoningMS = ms(p.firstReasoning)
	result.FirstContentMS = ms(p.firstContent)
	result.DoneMS = ms(p.done)
	result.AskMS = ms(p.ask)
	validator := &appProposalStream{}
	for i, kind := range p.kinds {
		line := proposalLineTiming{Line: json.RawMessage(p.keys[i]), Rejection: validator.line(p.keys[i]), Kind: kind, ProviderMS: ms(p.providerLines[i]), AppMS: -1}
		if at, ok := p.appTimes[p.keys[i]]; ok {
			line.AppMS = ms(at)
		}
		result.Lines = append(result.Lines, line)
	}
	return result
}
func writeProposalTiming(path string, timing proposalTiming) error {
	if path == "" {
		return nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(timing)
}
