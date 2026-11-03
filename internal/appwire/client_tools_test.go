package appwire

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

type clientToolsEngine struct {
	sendEngine
	sets    []ClientToolsSetParams
	answers []ClientToolResultParams
}

func (e *clientToolsEngine) SetClientTools(p ClientToolsSetParams) (ClientToolsSetResult, error) {
	e.sets = append(e.sets, p)
	names := []string{}
	for _, tool := range p.Tools {
		names = append(names, tool.Name)
	}
	return ClientToolsSetResult{Agent: p.Agent, Set: p.Set, Tools: names}, nil
}
func (e *clientToolsEngine) ClientToolResult(p ClientToolResultParams) (ClientToolResultAck, error) {
	e.answers = append(e.answers, p)
	return ClientToolResultAck{Accepted: true}, nil
}

func validClientToolsParams() ClientToolsSetParams {
	return ClientToolsSetParams{Agent: "agent-1", Set: "app.notes", Tools: []session.ClientTool{{Name: "app_note", Description: "Read a note",
		InputSchema: json.RawMessage(`{"type":"object"}`), Category: session.ActionRead}}}
}

// Contract (f): the actual Serve hello advertises only an implemented door.
func TestClientToolsHelloFeature(t *testing.T) {
	for _, test := range []struct {
		engine Engine
		want   bool
	}{{&sendEngine{}, false}, {&clientToolsEngine{}, true}} {
		var out bytes.Buffer
		server := NewServer(&out)
		if err := server.Serve(context.Background(), strings.NewReader(""), Hello{}, test.engine); err != nil {
			t.Fatal(err)
		}
		var frame struct {
			Data Hello `json:"data"`
		}
		if err := json.Unmarshal(bytes.Split(out.Bytes(), []byte("\n"))[0], &frame); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, feature := range frame.Data.Features {
			found = found || feature == FeatureClientTools
		}
		if found != test.want {
			t.Fatalf("features=%v want client tools=%v", frame.Data.Features, test.want)
		}
	}
}

// Contract (b), (h): both typed methods preserve their declared shapes.
func TestClientToolsTypedMethods(t *testing.T) {
	e := &clientToolsEngine{}
	raw, _ := json.Marshal(validClientToolsParams())
	result, err := methods["agents.tools_set"].call(e, raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	if string(encoded) != `{"agent":"agent-1","set":"app.notes","tools":["app_note"]}` {
		t.Fatal(string(encoded))
	}
	for _, raw := range []string{`{"call":"ct_1","result":null}`, `{"call":"ct_1","result":[1,true]}`, `{"call":"ct_1","error":"no access"}`} {
		result, err := methods["agents.tool_result"].call(e, json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(result)
		if string(encoded) != `{"accepted":true}` {
			t.Fatal(string(encoded))
		}
	}
	if len(e.sets) != 1 || len(e.answers) != 3 || string(e.answers[0].Result) != "null" {
		t.Fatal("typed arguments were lost")
	}
	if _, err := methods["agents.tools_set"].call(&sendEngine{}, raw); codeOf(err) != CodeUnknownMethod {
		t.Fatal(err)
	}
}

// Contract (h): every scalar and per-set limit refuses before the optional engine.
func TestClientToolsValidationRefusals(t *testing.T) {
	changes := map[string]func(*ClientToolsSetParams){
		"agent":             func(p *ClientToolsSetParams) { p.Agent = "" },
		"set":               func(p *ClientToolsSetParams) { p.Set = "Bad set" },
		"set length":        func(p *ClientToolsSetParams) { p.Set = strings.Repeat("a", 65) },
		"name":              func(p *ClientToolsSetParams) { p.Tools[0].Name = "Bad-name" },
		"name length":       func(p *ClientToolsSetParams) { p.Tools[0].Name = strings.Repeat("a", 65) },
		"duplicate":         func(p *ClientToolsSetParams) { p.Tools = append(p.Tools, p.Tools[0]) },
		"description empty": func(p *ClientToolsSetParams) { p.Tools[0].Description = "" },
		"description bytes": func(p *ClientToolsSetParams) {
			p.Tools[0].Description = strings.Repeat("é", session.ClientToolDescriptionMax)
		},
		"schema array": func(p *ClientToolsSetParams) { p.Tools[0].InputSchema = json.RawMessage(`[]`) },
		"schema null":  func(p *ClientToolsSetParams) { p.Tools[0].InputSchema = json.RawMessage(`null`) },
		"schema type":  func(p *ClientToolsSetParams) { p.Tools[0].InputSchema = json.RawMessage(`{"type":"array"}`) },
		"schema bytes": func(p *ClientToolsSetParams) {
			p.Tools[0].InputSchema = json.RawMessage(`{"type":"object","padding":"` + strings.Repeat("x", session.ClientToolSchemaMax) + `"}`)
		},
		"category": func(p *ClientToolsSetParams) { p.Tools[0].Category = session.ActionRun },
		"tools":    func(p *ClientToolsSetParams) { p.Tools = make([]session.ClientTool, session.ClientToolMaxPerSet+1) },
		"instructions": func(p *ClientToolsSetParams) {
			p.Instructions = strings.Repeat("x", session.ClientToolInstructionsMax+1)
		},
		"negative deadline": func(p *ClientToolsSetParams) { p.DeadlineMS = -1 },
		"short deadline":    func(p *ClientToolsSetParams) { p.DeadlineMS = session.ClientToolMinDeadlineMS - 1 },
		"long deadline":     func(p *ClientToolsSetParams) { p.DeadlineMS = session.ClientToolMaxDeadlineMS + 1 },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			p := validClientToolsParams()
			change(&p)
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			e := &clientToolsEngine{}
			if _, err := methods["agents.tools_set"].call(e, raw); codeOf(err) != CodeBadParams {
				t.Fatalf("accepted %s: %v", name, err)
			}
			if len(e.sets) != 0 {
				t.Fatal("invalid set reached the engine")
			}
		})
	}
}

// Contract (h): null is a result, while an error must be a nonempty string.
func TestClientToolResultValidation(t *testing.T) {
	for _, raw := range []string{`{}`, `{"call":"c","result":1,"error":"bad"}`, `{"call":"c","error":""}`, `{"call":"c","error":null}`,
		`{"call":"c","result":1,"error":null}`, `{"call":"c","error":7}`, `{"call":"c","error":"` + strings.Repeat("x", session.ClientToolErrorMax+1) + `"}`} {
		e := &clientToolsEngine{}
		if _, err := methods["agents.tool_result"].call(e, json.RawMessage(raw)); codeOf(err) != CodeBadParams {
			t.Fatalf("accepted %.90s: %v", raw, err)
		}
		if len(e.answers) > 0 {
			t.Fatal("invalid answer reached the engine")
		}
	}
}

// Contract (h): the shipped section stays bounded and names the actual limits.
func TestClientToolsManualLimits(t *testing.T) {
	raw, err := os.ReadFile("../manual/chat/client-tools.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	if strings.Count(page, "\n## ") != 1 || len(raw) >= 2000 {
		t.Fatal("client tools manual section is not self-contained and bounded")
	}
	for _, phrase := range []string{
		fmt.Sprintf("%d sets", session.ClientToolMaxSets), fmt.Sprintf("%d tools per set", session.ClientToolMaxPerSet),
		fmt.Sprintf("%d tools total", session.ClientToolMaxPerAgent), fmt.Sprintf("%d seconds", session.ClientToolDefaultDeadlineMS/1000),
		fmt.Sprintf("%d seconds is the maximum", session.ClientToolMaxDeadlineMS/1000), fmt.Sprintf("%d KiB", session.ClientToolOutputMax/1024),
		"outcome unknown", "Child tasks", "connected",
	} {
		if !strings.Contains(page, phrase) {
			t.Errorf("manual omits %q", phrase)
		}
	}
}
