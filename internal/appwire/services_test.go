package appwire

import (
	"encoding/json"
	"errors"
	"testing"
)

type servicesEngine struct {
	fakeEngine
	verb, id, agent, policy string
	from, max               int
}

func (s *servicesEngine) ListServices(a string) ([]Process, error) { s.agent = a; return nil, nil }
func (s *servicesEngine) row(verb, id string) (Process, error) {
	s.verb, s.id = verb, id
	return Process{ID: id, Agent: "a1", State: "running", Service: &Service{Lifecycle: ServiceStopped, Running: true}}, nil
}
func (s *servicesEngine) GetService(id string) (Process, error) { return s.row("get", id) }
func (s *servicesEngine) SetServicePolicy(id, p string) (Process, error) {
	s.policy = p
	return s.row("policy", id)
}
func (s *servicesEngine) RestartService(id string) (Process, error) { return s.row("restart", id) }
func (s *servicesEngine) StartService(id string) (Process, error)   { return s.row("start", id) }
func (s *servicesEngine) StopService(id string) (Process, error)    { return s.row("stop", id) }
func (s *servicesEngine) RemoveService(id string) error             { s.id = id; s.verb = "remove"; return nil }
func (s *servicesEngine) StopAllServices() (int, error)             { s.verb = "stop_all"; return 2, nil }
func (s *servicesEngine) ServiceOutput(id string, from, max int) (OutputPage, error) {
	s.id, s.from, s.max = id, from, max
	return OutputPage{}, nil
}
func (s *servicesEngine) ServiceTail(id string, n int) (OutputPage, error) {
	s.id, s.max = id, n
	return OutputPage{}, nil
}

func TestServicesRoutesRequireCapabilityAndValidArguments(t *testing.T) {
	eng := &servicesEngine{}
	for name := range serviceMethods {
		_, err := methods[name].call(&fakeEngine{}, json.RawMessage(`{"id":"a1:s1","policy":"never"}`))
		var wire *Error
		if !errors.As(err, &wire) || wire.Code != CodeUnknownMethod {
			t.Fatalf("%s without capability: %v", name, err)
		}
	}
	for _, tc := range []struct{ name, params string }{
		{"services.get", `{}`}, {"services.stop", `{"id":" "}`}, {"services.restart", `{}`}, {"services.start", `{}`}, {"services.remove", `{}`},
		{"services.policy", `{"id":"a1:s1","policy":"sometimes"}`}, {"services.policy", `{"policy":"never"}`},
		{"services.output", `{"id":"a1:s1","from_line":-1}`}, {"services.output", `{"id":"a1:s1","max_lines":-1}`},
		{"services.tail", `{"id":"a1:s1","lines":-1}`},
	} {
		_, err := methods[tc.name].call(eng, json.RawMessage(tc.params))
		var wire *Error
		if !errors.As(err, &wire) || wire.Code != CodeBadParams {
			t.Fatalf("%s %s: %v", tc.name, tc.params, err)
		}
	}
}

func TestServicesRoutesPreserveOwnershipAndTypedReplies(t *testing.T) {
	eng := &servicesEngine{}
	call := func(name, params string) map[string]json.RawMessage {
		t.Helper()
		v, err := methods[name].call(eng, json.RawMessage(params))
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]json.RawMessage
		if err = json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	out := call("services.list", `{}`)
	if eng.agent != "" || string(out["processes"]) != "[]" {
		t.Fatalf("all owners/empty array: %s %+v", out["processes"], eng)
	}
	call("services.list", `{"agent":" a1 "}`)
	if eng.agent != "a1" {
		t.Fatal("agent owner not forwarded")
	}
	for _, verb := range []string{"get", "start", "restart", "stop"} {
		out = call("services."+verb, `{"id":"a1:s1"}`)
		var p Process
		if err := json.Unmarshal(out["process"], &p); err != nil {
			t.Fatal(err)
		}
		if eng.verb != verb || eng.id != "a1:s1" || p.ID != "a1:s1" || p.Service == nil || !p.Service.Running || p.State != "running" {
			t.Fatalf("%s lost owner or live service: %+v", verb, p)
		}
	}
	call("services.policy", `{"id":"a1:s1","policy":"on_failure"}`)
	if eng.id != "a1:s1" || eng.policy != "on_failure" {
		t.Fatal("policy target lost")
	}
	out = call("services.output", `{"id":"a1:s1","from_line":7,"max_lines":12}`)
	if eng.from != 7 || eng.max != 12 || string(out["lines"]) != "[]" {
		t.Fatal("output cursor or empty array lost")
	}
	out = call("services.tail", `{"id":"a1:s1","lines":6}`)
	if eng.max != 6 || string(out["lines"]) != "[]" {
		t.Fatal("tail count or empty array lost")
	}
	call("services.remove", `{"id":"a1:s1"}`)
	if eng.verb != "remove" || eng.id != "a1:s1" {
		t.Fatal("remove target lost")
	}
	out = call("services.stop_all", `{}`)
	if string(out["stopped"]) != "2" {
		t.Fatal("stop-all count lost")
	}
}
