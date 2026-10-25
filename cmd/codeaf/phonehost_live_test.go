//go:build live

package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/identity"
	"golang.org/x/crypto/nacl/box"
)

// Capture only the explicitly supplied environment key before TestMain clears
// credentials. It is forwarded in memory and never stored in the test home.
var phoneLiveKey = os.Getenv("OPENROUTER_API_KEY")

const phoneLiveModel = "deepseek/deepseek-v4.1-flash"

func TestRealModelPhoneCreateSendAndEntries(t *testing.T) {
	if phoneLiveKey == "" {
		t.Skip("OPENROUTER_API_KEY is unset")
	}
	started := time.Now()
	bin := buildCodeafStamped(t, "")
	root, err := os.MkdirTemp("", "phone-live-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	state := filepath.Join(root, "home")
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	policy := map[string]any{config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: phoneLiveModel, config.KeyModelFallbacks: "", config.KeyTierReflexModel: phoneLiveModel, config.KeyTierLowModel: phoneLiveModel, config.KeyTierWorkerModel: phoneLiveModel, config.KeyTierHighModel: phoneLiveModel, config.KeyTierMastermindModel: phoneLiveModel}
	raw, _ := json.Marshal(policy)
	if err := os.WriteFile(filepath.Join(state, "config.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	requests, replies := map[string]int{}, map[string]int{}
	var problems []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		if err != nil {
			http.Error(w, "read", 502)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			var call struct {
				Model  string
				Models []string
			}
			_ = json.Unmarshal(body, &call)
			mu.Lock()
			requests[call.Model]++
			valid := call.Model == phoneLiveModel && len(call.Models) == 0
			if !valid {
				problems = append(problems, "unexpected request model: "+call.Model)
			}
			mu.Unlock()
			if !valid {
				http.Error(w, "model must be pinned", 400)
				return
			}
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, "https://openrouter.ai/api/v1"+r.URL.RequestURI(), bytes.NewReader(body))
		if err != nil {
			http.Error(w, "request", 502)
			return
		}
		req.Header = r.Header.Clone()
		req.Header.Set("Authorization", "Bearer "+phoneLiveKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, "upstream unavailable", 502)
			return
		}
		defer resp.Body.Close()
		for k, values := range resp.Header {
			for _, v := range values {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			_, _ = io.Copy(w, resp.Body)
			return
		}
		record := func(data []byte) {
			var got struct{ Model string }
			if json.Unmarshal(data, &got) == nil && got.Model != "" {
				mu.Lock()
				replies[got.Model]++
				if got.Model != phoneLiveModel {
					problems = append(problems, "unexpected response model: "+got.Model)
				}
				mu.Unlock()
			}
		}
		if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			sc := bufio.NewScanner(resp.Body)
			sc.Buffer(make([]byte, 65536), 4<<20)
			for sc.Scan() {
				line := sc.Text()
				if data, ok := strings.CutPrefix(line, "data: "); ok {
					record([]byte(data))
				}
				_, _ = io.WriteString(w, line+"\n")
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		} else {
			data, _ := io.ReadAll(resp.Body)
			record(data)
			_, _ = w.Write(data)
		}
	}))
	defer upstream.Close()
	relay, proc := startPhoneRelay(t, bin, filepath.Join(root, "relay"))
	defer killProc(proc)
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var tokens []struct {
		text string
		at   time.Time
	}
	var latencies []time.Duration
	var cumulative string
	target, _ := url.Parse(relay)
	proxy := httputil.NewSingleHostReverseProxy(target)
	relayTap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && strings.Contains(r.URL.Path, "/slots/agents.entries.") {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			var slot struct{ Sealed []byte }
			_ = json.Unmarshal(body, &slot)
			if plain, ok := box.OpenAnonymous(nil, slot.Sealed, pub, priv); ok {
				var rows []struct{ Result appwire.EntriesResult }
				_ = json.Unmarshal(plain, &rows)
				mu.Lock()
				for _, row := range rows {
					for _, entry := range row.Result.Entries {
						if entry.Kind == appwire.EntryAnswer {
							for len(tokens) > 0 && strings.Contains(entry.Text, tokens[0].text) {
								latencies = append(latencies, time.Since(tokens[0].at))
								tokens = tokens[1:]
							}
						}
					}
				}
				mu.Unlock()
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	defer relayTap.Close()
	app, logs := startPhoneApp(t, bin, state, relayTap.URL, "CODEAF_PHONE_FIXTURE=0", "OPENROUTER_API_KEY="+phoneLiveKey, "CODEAF_BASE_URL="+upstream.URL, "CODEAF_MODEL="+phoneLiveModel)
	defer killProc(app.cmd)
	select {
	case <-app.lines:
	case <-time.After(30 * time.Second):
		t.Fatalf("app startup: %s", logs.String())
	}
	go func() {
		for line := range app.lines {
			var frame struct {
				Event string
				Data  json.RawMessage
			}
			if json.Unmarshal(line, &frame) != nil {
				continue
			}
			if frame.Event == appwire.EventTurnDelta {
				var d appwire.TurnDelta
				_ = json.Unmarshal(frame.Data, &d)
				if d.Kind == appwire.DeltaText {
					mu.Lock()
					cumulative += d.Text
					tokens = append(tokens, struct {
						text string
						at   time.Time
					}{cumulative, time.Now()})
					mu.Unlock()
				}
			}
		}
	}()
	seed, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	keys, _ := json.Marshal(map[string]any{"signing_key": seed.Public(), "box_key": pub[:]})
	grant := phonePairCLI(t, bin, state, keys)
	dev, err := seed.Join(grant.Cert)
	if err != nil {
		t.Fatal(err)
	}
	client := scopedSigner{grant.Identity, dev}
	queue := func(number int, method, target string, params any) {
		raw, _ := json.Marshal(params)
		payload, _ := json.Marshal(clientrun.Call{Method: method, Params: raw})
		sealed, err := box.SealAnonymous(nil, payload, (*[32]byte)(grant.BoxKey), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		clientWire(t, relayTap.URL, client, "POST", "/v1/clients/"+dev.ID()+"/queue", map[string]any{"number": number, "at": time.Now().UnixMilli(), "method": method, "target": target, "sealed": sealed}, nil)
	}
	if err := os.MkdirAll(filepath.Join(root, "workspace"), 0700); err != nil {
		t.Fatal(err)
	}
	queue(1, "agents.create", "any", map[string]any{"workspace": filepath.Join(root, "workspace"), "model": phoneLiveModel, "automatic_models": false})
	// A blank phone-created chat is scoped even before it has a first message.
	path := "/v1/clients/" + dev.ID() + "/slots/" + clientrun.SlotName("agents.entries", grant.Writer)
	read := func() ([]struct {
		Agent  string
		Result appwire.EntriesResult
	}, bool) {
		body, status := signedGET(t, relayTap.URL, client, path)
		if status != 200 {
			return nil, false
		}
		var slot struct{ Sealed []byte }
		if json.Unmarshal(body, &slot) != nil {
			return nil, false
		}
		plain, ok := box.OpenAnonymous(nil, slot.Sealed, pub, priv)
		if !ok {
			t.Fatal("cannot decrypt entries")
		}
		var rows []struct {
			Agent  string
			Result appwire.EntriesResult
		}
		if err := json.Unmarshal(plain, &rows); err != nil {
			t.Fatal(err)
		}
		return rows, true
	}
	var version uint64
	waitChanges := func() {
		body, status := signedGET(t, relayTap.URL, client, fmt.Sprintf("/v1/clients/%s/slots?since=%d&wait=25", dev.ID(), version))
		if status == 204 {
			return
		}
		var changed struct {
			Version uint64
			Slots   []string
		}
		if status != 200 || json.Unmarshal(body, &changed) != nil || changed.Version <= version {
			t.Fatalf("long-poll: status=%d body=%s", status, body)
		}
		version = changed.Version
	}
	deadline := time.Now().Add(2 * time.Minute)
	agent := ""
	for time.Now().Before(deadline) {
		rows, _ := read()
		if len(rows) > 0 {
			agent = rows[0].Agent
			break
		}
		waitChanges()
	}
	if agent == "" {
		t.Fatalf("queued creation never published its leased chat\n%s", logs.String())
	}
	queue(2, "agents.send", grant.Writer, map[string]string{"id": agent, "text": "Reply with the single word: pong"})
	reply := ""
	deadline = time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		rows, _ := read()
		for _, row := range rows {
			for _, entry := range row.Result.Entries {
				if entry.Kind == appwire.EntryAnswer && strings.Contains(strings.ToLower(entry.Text), "pong") {
					reply = entry.Text
				}
			}
		}
		if reply != "" {
			break
		}
		waitChanges()
	}
	if reply == "" {
		t.Fatalf("assistant pong absent from entries slots\n%s", logs.String())
	}

	queue(3, "crews.create", "any", appwire.CrewCreate{Name: "phone live crew", Agents: []string{agent}, Manager: agent})
	readSnapshot := func(method string, out any) bool {
		body, status := signedGET(t, relayTap.URL, client, "/v1/clients/"+dev.ID()+"/slots/"+clientrun.SlotName(method, grant.Writer))
		if status != 200 {
			return false
		}
		var slot struct{ Sealed []byte }
		if json.Unmarshal(body, &slot) != nil {
			t.Fatal("malformed slot")
		}
		plain, ok := box.OpenAnonymous(nil, slot.Sealed, pub, priv)
		if !ok {
			t.Fatal("cannot open crew snapshot")
		}
		if err := json.Unmarshal(plain, out); err != nil {
			t.Fatal(err)
		}
		return true
	}
	crewID := ""
	deadline = time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		var rows []struct{ Result appwire.CrewsResult }
		if readSnapshot("crews.list", &rows) {
			for _, row := range rows {
				for _, crew := range row.Result.Crews {
					if crew.Name == "phone live crew" {
						crewID = crew.ID
					}
				}
			}
		}
		if crewID != "" {
			break
		}
		waitChanges()
	}
	if crewID == "" {
		t.Fatal("queued crew creation never reached its snapshot")
	}
	queue(4, "crews.message", grant.Writer, appwire.CrewMessage{Crew: crewID, Text: "Use team_post to post the exact text crew-pong to the room, then reply with crew-pong. Do not create helpers or do other work."})
	crewLogSeen := false
	deadline = time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		waitChanges()
		var rows []struct {
			Crew   string
			Result appwire.LinesCursorResult
		}
		if readSnapshot("crews.log", &rows) {
			for _, row := range rows {
				if row.Crew == crewID {
					for _, line := range row.Result.Lines {
						if strings.Contains(line.Text, "crew-pong") {
							crewLogSeen = true
						}
					}
				}
			}
		}
		if crewLogSeen {
			break
		}
	}
	if !crewLogSeen {
		t.Fatalf("crew log did not update through long-poll\n%s", logs.String())
	}
	t.Logf("REAL_CREW crew=%s message=queued log_update=long-poll model=%s", crewID, phoneLiveModel)
	mu.Lock()
	defer mu.Unlock()
	if len(problems) > 0 || len(requests) != 1 || requests[phoneLiveModel] == 0 || len(replies) != 1 || replies[phoneLiveModel] == 0 {
		t.Fatalf("model recording requests=%v replies=%v problems=%v", requests, replies, problems)
	}
	if len(latencies) == 0 {
		t.Fatal("no desktop-token to slot PUT latency recorded")
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50, p95 := latencies[(len(latencies)-1)/2], latencies[(len(latencies)*95-1)/100]
	t.Logf("REAL_MODEL model=%s reply=%q wall=%s requests=%v response_models=%v token_to_slot_PUT_n=%d p50=%s p95=%s", phoneLiveModel, reply, time.Since(started).Round(time.Millisecond), requests, replies, len(latencies), p50, p95)
	if p95 > time.Second {
		t.Fatalf("token-to-slot PUT p95 %s exceeds 1s", p95)
	}
}
