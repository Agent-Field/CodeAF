package appwire

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// postureEngine is a fake engine with the approval posture door.
type postureEngine struct {
	fakeEngine
	set []string
}

func (p *postureEngine) SetApprovalPosture(id, posture string) (Agent, error) {
	p.mu.Lock()
	p.set = append(p.set, posture)
	p.mu.Unlock()
	source := ApprovalFromChat
	if posture == "auto" {
		posture, source = "ask", ApprovalFromSettings
	}
	return Agent{ID: id, State: StateIdle, Approval: &AgentApproval{Posture: posture, Source: source}}, nil
}

// The app moves a conversation along the ladder and back to Settings, and
// never to Block: deny is Settings' answer and a chat follows it with auto.
func TestSetApprovalPostureTakesTheAppsWordsOnly(t *testing.T) {
	engine := &postureEngine{}
	ask := serveOver(t, engine)
	for _, word := range []string{"ask", "guardian", " Allow ", "auto", "deny", "block", "", "sideways"} {
		t.Run(word, func(t *testing.T) {
			request, _ := json.Marshal(map[string]any{"id": 1, "method": "agents.set_approval_posture", "params": map[string]string{"id": "a1", "posture": word}})
			got := ask(string(request))
			want := strings.ToLower(strings.TrimSpace(word))
			switch want {
			case "ask", "guardian", "allow", "auto":
			default:
				failure, _ := got["error"].(map[string]any)
				if failure["code"] != CodeBadParams || failure["message"] != "posture is ask, guardian, allow or auto" {
					t.Fatalf("%q answered %v", word, got)
				}
				return
			}
			approval := got["result"].(map[string]any)["agent"].(map[string]any)["approval"].(map[string]any)
			if want == "auto" {
				if approval["source"] != "settings" {
					t.Fatalf("auto = %v", approval)
				}
				return
			}
			if approval["posture"] != want || approval["source"] != "chat" {
				t.Fatalf("%q = %v", word, approval)
			}
		})
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if strings.Join(engine.set, ",") != "ask,guardian,allow,auto" {
		t.Fatalf("the engine was asked for %v", engine.set)
	}
}

func TestSetApprovalPostureNeedsAnID(t *testing.T) {
	ask := serveOver(t, &postureEngine{})
	if failure, _ := ask(`{"id":1,"method":"agents.set_approval_posture","params":{"posture":"ask"}}`)["error"].(map[string]any); failure["code"] != CodeBadParams {
		t.Fatalf("no id = %v", failure)
	}
}

// An engine without the door says so rather than pretending to move a gate.
func TestSetApprovalPostureOnAnOlderEngine(t *testing.T) {
	w := startWire(t, &fakeEngine{})
	w.next()
	w.send(`{"id":1,"method":"agents.set_approval_posture","params":{"id":"a1","posture":"ask"}}`)
	failure, _ := w.next()["error"].(map[string]any)
	if failure["code"] != CodeBadParams || !strings.Contains(failure["message"].(string), "newer engine") {
		t.Fatalf("older engine = %v", failure)
	}
}

// agents.create carries the new conversation's own effort and posture,
// spelled the engine's way, and refuses the words the setters refuse.
func TestCreateCarriesEffortAndPosture(t *testing.T) {
	engine := &fakeEngine{}
	w := startWire(t, engine)
	w.next()
	w.send(`{"id":1,"method":"agents.create","params":{"workspace":"/w","model":"m","budget_usd":null,"text":"","client_id":"","effort":" High ","approval_posture":"Guardian"}}`)
	if got := w.next(); got["error"] != nil {
		t.Fatalf("create = %v", got)
	}
	engine.mu.Lock()
	created := engine.created
	engine.mu.Unlock()
	if created.Effort != "high" || created.ApprovalPosture != "guardian" {
		t.Fatalf("created with effort %q and posture %q", created.Effort, created.ApprovalPosture)
	}
	w.send(`{"id":2,"method":"agents.create","params":{"workspace":"/w","model":"m","budget_usd":null,"text":"","client_id":""}}`)
	w.next()
	engine.mu.Lock()
	created = engine.created
	engine.mu.Unlock()
	if created.Effort != "" || created.ApprovalPosture != "" {
		t.Fatalf("an unnamed effort or posture arrived as %q and %q", created.Effort, created.ApprovalPosture)
	}
	for i, params := range []string{
		`"effort":"sideways"`,
		`"approval_posture":"deny"`,
		`"approval_posture":"sideways"`,
	} {
		request := `{"id":` + strconv.Itoa(i+3) + `,"method":"agents.create","params":{"workspace":"/w","model":"m","budget_usd":null,"text":"","client_id":"",` + params + `}}`
		w.send(request)
		if failure, _ := w.next()["error"].(map[string]any); failure["code"] != CodeBadParams {
			t.Fatalf("%s = %v", params, failure)
		}
	}
}

// The schema names the new words, so the app cannot send one the engine refuses.
func TestTheSchemaNamesTheApprovalWords(t *testing.T) {
	out, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Properties map[string]map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ def, field string }{
		{"in.CreateAsk", "effort"}, {"in.CreateAsk", "approval_posture"},
		{"AgentApproval", "posture"}, {"AgentApproval", "source"},
		{"Settings", "approval_posture"},
	} {
		if doc.Defs[want.def].Properties[want.field]["enum"] == nil {
			t.Errorf("%s.%s names no words", want.def, want.field)
		}
	}
}
