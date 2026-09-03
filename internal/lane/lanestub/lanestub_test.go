package lanestub

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// three lanes with the shape the real world had on 2026-08-30: one that starts
// fast and writes slowly, one that starts slowly and writes fast, and one that
// cannot take a tool call at all.
func lanes() []Lane {
	return []Lane{
		{Name: "CoreWeave", Profile: Profile{
			TTFT: 40 * time.Millisecond, Rate: 240, Tokens: 8, Tools: true,
			Quant: "fp8", Context: 943000, MaxOut: 943000, Uptime: 99.6,
			PriceIn: 0.00000009, PriceOut: 0.00000028, Caches: true,
		}},
		{Name: "Cloudflare", Profile: Profile{
			TTFT: 80 * time.Millisecond, Rate: 580, Tokens: 8,
			Context: 345000, MaxOut: 345000, Uptime: 100,
			PriceIn: 0.0000003, PriceOut: 0.00000132, Heartbeats: true,
		}},
		{Name: "DeepInfra", Profile: Profile{
			TTFT: 60 * time.Millisecond, Rate: 270, Tokens: 8, Tools: true,
			Quant: "fp8", Context: 65000, MaxOut: 65000, Uptime: 99.8,
			PriceIn: 0.00000006, PriceOut: 0.00000018,
		}},
	}
}

const model = "deepseek/deepseek-v4-flash"

func start(t *testing.T) *Server {
	t.Helper()
	server := New(model, lanes()...)
	t.Cleanup(server.Close)
	return server
}

// ask sends one streamed completion and returns every raw SSE line.
func ask(t *testing.T, ctx context.Context, server *Server, body string) []string {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL()+"/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	var lines []string
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestTheSheetSpellsEveryFieldTheRouterSpells is the reason this package
// exists: five lanes of work decode this JSON, and a field name that drifts
// here is a decoder that works against the stub and fails against the router.
func TestTheSheetSpellsEveryFieldTheRouterSpells(t *testing.T) {
	server := start(t)
	response, err := http.Get(server.URL() + "/models/deepseek/deepseek-v4-flash/endpoints")
	if err != nil {
		t.Fatalf("fetch the sheet: %v", err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)

	var body struct {
		Data struct {
			ID        string `json:"id"`
			Endpoints []struct {
				ProviderName        string `json:"provider_name"`
				Tag                 string `json:"tag"`
				Quantization        string `json:"quantization"`
				ContextLength       int    `json:"context_length"`
				MaxCompletionTokens int    `json:"max_completion_tokens"`
				Pricing             struct {
					Prompt         string `json:"prompt"`
					Completion     string `json:"completion"`
					InputCacheRead string `json:"input_cache_read"`
				} `json:"pricing"`
				SupportsToolChoice struct {
					Function bool `json:"function"`
				} `json:"supports_tool_choice"`
				Status                int     `json:"status"`
				UptimeLast30m         float64 `json:"uptime_last_30m"`
				UptimeLast5m          float64 `json:"uptime_last_5m"`
				UptimeLast1d          float64 `json:"uptime_last_1d"`
				SupportsImplicitCache bool    `json:"supports_implicit_caching"`
				Latency               struct {
					P50 float64 `json:"p50"`
					P75 float64 `json:"p75"`
					P90 float64 `json:"p90"`
					P99 float64 `json:"p99"`
				} `json:"latency_last_30m"`
				Throughput struct {
					P50 float64 `json:"p50"`
					P99 float64 `json:"p99"`
				} `json:"throughput_last_30m"`
			} `json:"endpoints"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode the sheet: %v", err)
	}
	if body.Data.ID != model {
		t.Fatalf("the sheet named %q, not the model asked for", body.Data.ID)
	}
	if len(body.Data.Endpoints) != 3 {
		t.Fatalf("three lanes were declared and %d were published", len(body.Data.Endpoints))
	}
	first := body.Data.Endpoints[0]
	if first.ProviderName != "CoreWeave" || first.Tag != "coreweave" {
		t.Fatalf("the first row named %q / %q", first.ProviderName, first.Tag)
	}
	if !first.SupportsToolChoice.Function {
		t.Fatal("a lane declared with tools published none")
	}
	if first.Quantization != "fp8" || first.ContextLength != 943000 || first.MaxCompletionTokens != 943000 {
		t.Fatalf("the gate facts did not survive: %+v", first)
	}
	if first.Pricing.Prompt != "9e-08" || first.Pricing.Completion != "2.8e-07" {
		t.Fatalf("prices are not the router's strings: %+v", first.Pricing)
	}
	if first.UptimeLast5m != 99.6 || first.UptimeLast30m != 99.6 || first.UptimeLast1d != 99.6 {
		t.Fatalf("uptime did not survive: %+v", first)
	}
	if !first.SupportsImplicitCache {
		t.Fatal("a caching lane published no caching")
	}
	// The derived spread: p50 is what the lane really does, and the tail is
	// wider, which is what makes a prior fit worth doing at all.
	if first.Latency.P50 != 40 || !(first.Latency.P99 > first.Latency.P90 && first.Latency.P90 > first.Latency.P50) {
		t.Fatalf("the derived latency percentiles do not rise: %+v", first.Latency)
	}
	if first.Throughput.P50 != 240 || first.Throughput.P99 <= 240 {
		t.Fatalf("the derived throughput percentiles do not rise: %+v", first.Throughput)
	}
	if server.Sheets(model) != 1 {
		t.Fatalf("one fetch was counted as %d", server.Sheets(model))
	}
}

// TestASheetStatedByHandOverridesTheDerivedOne is the case worth most: a lane
// the sheet is WRONG about, which is the whole test of a belief that learns.
func TestASheetStatedByHandOverridesTheDerivedOne(t *testing.T) {
	server := New("vendor/slow", Lane{Name: "Liar", Profile: Profile{
		TTFT: 3 * time.Second, Rate: 6,
		TTFTms: [4]float64{430, 900, 4539, 12240},
		Rates:  [4]float64{24, 30, 48, 60},
	}})
	defer server.Close()
	response, err := http.Get(server.URL() + "/models/vendor/slow/endpoints")
	if err != nil {
		t.Fatalf("fetch the sheet: %v", err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(raw), `"p99":12240`) || !strings.Contains(string(raw), `"p50":430`) {
		t.Fatalf("the stated percentiles were not published: %s", raw)
	}
}

// TestTheStreamHonoursTheRoutingPreference holds the one behaviour every lane
// of this wave asserts against: what went out on the wire decided who answered.
func TestTheStreamHonoursTheRoutingPreference(t *testing.T) {
	server := start(t)
	ctx := context.Background()

	ask(t, ctx, server, `{"model":"`+model+`","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if got := server.Served(); len(got) != 1 || got[0] != "CoreWeave" {
		t.Fatalf("with no preference the first lane declared should answer, got %v", got)
	}

	ask(t, ctx, server, `{"model":"`+model+`","stream":true,"provider":{"order":["DeepInfra","Cloudflare"]},"messages":[]}`)
	if got := server.Served(); got[len(got)-1] != "DeepInfra" {
		t.Fatalf("order was not honoured: %v", got)
	}

	ask(t, ctx, server, `{"model":"`+model+`","stream":true,"provider":{"ignore":["CoreWeave"]},"messages":[]}`)
	if got := server.Served(); got[len(got)-1] != "Cloudflare" {
		t.Fatalf("ignore was not honoured: %v", got)
	}

	ask(t, ctx, server, `{"model":"`+model+`","stream":true,"provider":{"only":["cloudflare"]},"messages":[]}`)
	if got := server.Served(); got[len(got)-1] != "Cloudflare" {
		t.Fatalf("only was not honoured, and it is matched loosely on purpose: %v", got)
	}

	asks := server.Asks()
	if len(asks) != 4 {
		t.Fatalf("four asks were recorded as %d", len(asks))
	}
	if len(asks[1].Order) != 2 || asks[1].Order[0] != "DeepInfra" {
		t.Fatalf("the preference was not recorded as it arrived: %+v", asks[1])
	}
	if server.Requests("Cloudflare") != 2 {
		t.Fatalf("Cloudflare answered twice and was counted %d times", server.Requests("Cloudflare"))
	}
}

func TestThePriceCeilingIsRecordedAndAppliedBeforeADemand(t *testing.T) {
	server := New(model,
		Lane{Name: "DeepSeek", Profile: Profile{PriceIn: 0.66e-6, PriceOut: 1.98e-6}},
		Lane{Name: "Fireworks", Profile: Profile{PriceIn: 1.32e-6, PriceOut: 3.96e-6}},
	)
	defer server.Close()
	post := func(body string) (*http.Response, []byte) {
		t.Helper()
		response, err := http.Post(server.URL()+"/chat/completions", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		return response, raw
	}

	response, raw := post(`{"model":"` + model + `","stream":true,"provider":{"only":["Fireworks"],"max_price":{"prompt":0.825,"completion":2.475}},"messages":[]}`)
	if response.StatusCode != http.StatusNotFound || !strings.Contains(string(raw), "Providers serving "+model+": deepseek") {
		t.Fatalf("the post-cap serving set answered %d %s", response.StatusCode, raw)
	}
	if strings.Contains(string(raw), "Providers serving "+model+": deepseek, fireworks") {
		t.Fatalf("the price-filtered lane remained in the serving list: %s", raw)
	}
	asks := server.Asks()
	if len(asks) != 1 || asks[0].MaxPrice == nil || asks[0].MaxPrice.Prompt != 0.825 || asks[0].MaxPrice.Completion != 2.475 {
		t.Fatalf("the recorded max_price is %+v, want 0.825/2.475", asks)
	}

	response, raw = post(`{"model":"` + model + `","stream":true,"provider":{"max_price":{"prompt":0.5,"completion":1.5}},"messages":[]}`)
	if response.StatusCode != http.StatusNotFound || !strings.Contains(string(raw), "Paid model training violation") {
		t.Fatalf("an empty capped set answered %d %s", response.StatusCode, raw)
	}
}

// TestAPreferenceThatEmptiesTheSetIsTheRoutersOwn404 stages what the relaxation
// ladder in `internal/provider` exists for.
func TestAPreferenceThatEmptiesTheSetIsTheRoutersOwn404(t *testing.T) {
	server := start(t)
	request, _ := http.NewRequest(http.MethodPost, server.URL()+"/chat/completions",
		strings.NewReader(`{"model":"`+model+`","stream":true,"provider":{"only":["Nobody"]},"messages":[]}`))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusNotFound || !strings.Contains(string(raw), "No endpoints found") {
		t.Fatalf("an emptied set answered %d %s", response.StatusCode, raw)
	}
}

func TestAnUncappedEmptySetKeepsTheGenericDataPolicyRefusal(t *testing.T) {
	server := New(model, Lane{Name: "Cloudflare", Profile: Profile{}})
	defer server.Close()
	request, _ := http.NewRequest(http.MethodPost, server.URL()+"/chat/completions",
		strings.NewReader(`{"model":"`+model+`","stream":true,"provider":{"ignore":["Cloudflare"]},"messages":[]}`))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusNotFound || !strings.Contains(string(raw), "No endpoints found matching your data policy") {
		t.Fatalf("an uncapped empty set answered %d %s", response.StatusCode, raw)
	}
	if strings.Contains(string(raw), "Paid model training violation") {
		t.Fatalf("an uncapped preference was described as the measured ceiling refusal: %s", raw)
	}
}

// TestHeartbeatsArriveBeforeTheFirstToken pins the free signal the watch reads
// to tell a dead path from a slow lane.
func TestHeartbeatsArriveBeforeTheFirstToken(t *testing.T) {
	server := start(t)
	lines := ask(t, context.Background(), server,
		`{"model":"`+model+`","stream":true,"provider":{"only":["Cloudflare"]},"messages":[]}`)
	beats, firstToken := 0, -1
	for index, line := range lines {
		switch {
		case line == ": OPENROUTER PROCESSING":
			beats++
			if firstToken >= 0 {
				t.Fatal("a heartbeat arrived after the first token")
			}
		case strings.HasPrefix(line, "data: {") && strings.Contains(line, `"content"`):
			if firstToken < 0 {
				firstToken = index
			}
		}
	}
	if beats == 0 {
		t.Fatal("a lane declared with heartbeats emitted none")
	}
	if firstToken < 0 {
		t.Fatal("no token arrived at all")
	}
}

// TestTheStreamNamesItsLaneAndEndsWithUsageThenTheSentinel is the shape the
// transport's own decoder is written against.
func TestTheStreamNamesItsLaneAndEndsWithUsageThenTheSentinel(t *testing.T) {
	server := start(t)
	lines := ask(t, context.Background(), server,
		`{"model":"`+model+`","stream":true,"provider":{"only":["DeepInfra"]},"messages":[{"role":"user","content":"a prompt of some length"}]}`)
	if len(lines) < 3 {
		t.Fatalf("the stream was %d lines long", len(lines))
	}
	if lines[len(lines)-1] != "data: [DONE]" {
		t.Fatalf("the stream did not end with the sentinel: %q", lines[len(lines)-1])
	}
	var usage struct {
		Provider string `json:"provider"`
		Usage    *struct {
			Prompt     int     `json:"prompt_tokens"`
			Completion int     `json:"completion_tokens"`
			Cost       float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[len(lines)-2], "data: ")), &usage); err != nil {
		t.Fatalf("decode the usage frame: %v", err)
	}
	if usage.Usage == nil || usage.Usage.Completion != 8 {
		t.Fatalf("the usage frame did not carry the answer's length: %+v", usage.Usage)
	}
	if usage.Usage.Prompt == 0 || usage.Usage.Cost <= 0 {
		t.Fatalf("the usage frame carried no prompt or no cost: %+v", usage.Usage)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "data: {") {
			continue
		}
		if !strings.Contains(line, `"provider":"DeepInfra"`) {
			t.Fatalf("a chunk did not name the lane serving it: %s", line)
		}
	}
}

// TestACancelledStreamIsCountedAgainstItsLane is what a hedge is judged on.
func TestACancelledStreamIsCountedAgainstItsLane(t *testing.T) {
	server := New(model, Lane{Name: "Slow", Profile: Profile{TTFT: 2 * time.Second, Rate: 10}})
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ask(t, ctx, server, `{"model":"`+model+`","stream":true,"messages":[]}`)
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done
	deadline := time.Now().Add(2 * time.Second)
	for server.Cancels("Slow") == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if server.Cancels("Slow") != 1 {
		t.Fatalf("a cancelled stream was counted %d times", server.Cancels("Slow"))
	}
}

// TestAScriptedMinuteCostsNoWallTime is the property that lets a whole wave of
// tests run on a scenario written in seconds.
func TestAScriptedMinuteCostsNoWallTime(t *testing.T) {
	server := New(model, Lane{Name: "Molasses", Profile: Profile{
		TTFT: 12 * time.Second, Rate: 1, Tokens: 30, StallAfter: 10, StallFor: 20 * time.Second,
	}})
	defer server.Close()
	clock := NewFast(time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC))
	server.SetClock(clock)

	began := time.Now()
	lines := ask(t, context.Background(), server, `{"model":"`+model+`","stream":true,"messages":[]}`)
	wall := time.Since(began)

	if lines[len(lines)-1] != "data: [DONE]" {
		t.Fatalf("the scripted stream did not finish: %d lines", len(lines))
	}
	if clock.Elapsed() < time.Minute {
		t.Fatalf("a minute was scripted and %s was spent on the fast clock", clock.Elapsed())
	}
	if wall > 250*time.Millisecond {
		t.Fatalf("a minute of scripted time cost %s of wall time", wall)
	}
}

// TestALaneMayRefuse stages the failure the endpoint ladder reads.
func TestALaneMayRefuse(t *testing.T) {
	server := New(model, Lane{Name: "Busy", Profile: Profile{FailWith: http.StatusTooManyRequests}})
	defer server.Close()
	request, _ := http.NewRequest(http.MethodPost, server.URL()+"/chat/completions",
		strings.NewReader(`{"model":"`+model+`","stream":true,"messages":[]}`))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(raw), "Busy") {
		t.Fatalf("a refusing lane answered %d %s", response.StatusCode, raw)
	}
	if server.Requests("Busy") != 1 {
		t.Fatal("a refusal is still a request and is counted")
	}
}

// TestTheCatalogKnowsTheModel keeps the catalog path usable against the stub.
func TestTheCatalogKnowsTheModel(t *testing.T) {
	server := start(t)
	response, err := http.Get(server.URL() + "/models")
	if err != nil {
		t.Fatalf("fetch the catalog: %v", err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(raw), model) {
		t.Fatalf("the catalog did not know the model: %s", raw)
	}
}
