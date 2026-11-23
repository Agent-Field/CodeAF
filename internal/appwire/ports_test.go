package appwire

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type portEngine struct {
	fakeEngine
	err error
	got PortInspection
}

func (p *portEngine) InspectPort(_ context.Context, port int) (PortInspection, error) {
	if p.err != nil {
		return PortInspection{}, p.err
	}
	out := p.got
	out.Port = port
	return out, nil
}

func (p *portEngine) SuggestPort(_ context.Context, after int) (PortSuggestion, error) {
	if p.err != nil {
		return PortSuggestion{}, p.err
	}
	return PortSuggestion{Port: after + 1, CheckedAt: time.Unix(1, 0), CheckedCount: 1}, nil
}

func wireCode(t *testing.T, err error) string {
	t.Helper()
	var w *Error
	if !errors.As(err, &w) {
		t.Fatalf("not a wire error: %v", err)
	}
	return w.Code
}

func TestPortsAbsentWithoutCapability(t *testing.T) {
	for name := range portMethods {
		_, err := methods[name].call(&fakeEngine{}, json.RawMessage(`{"port":8080,"after":8080}`))
		if wireCode(t, err) != CodeUnknownMethod {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestPortsValidateAndType(t *testing.T) {
	eng := &portEngine{got: PortInspection{State: PortInUse, CheckedAt: time.Unix(1, 0), Holder: &PortHolder{PID: 0, Name: "x"}}}
	for _, c := range []struct{ name, params string }{
		{"ports.inspect", `{}`}, {"ports.inspect", `{"port":0}`}, {"ports.inspect", `{"port":65536}`}, {"ports.inspect", `{"port":-1}`},
		{"ports.suggest", `{}`}, {"ports.suggest", `{"after":0}`}, {"ports.suggest", `{"after":65535}`},
	} {
		_, err := methods[c.name].call(eng, json.RawMessage(c.params))
		if wireCode(t, err) != CodeBadParams {
			t.Fatalf("%s %s: %v", c.name, c.params, err)
		}
	}
	r, err := methods["ports.inspect"].call(eng, json.RawMessage(`{"port":8080}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.(*PortInspection); got.Port != 8080 || got.State != PortInUse || got.Holder != nil {
		t.Fatalf("a holder with pid 0 must be dropped: %+v", got)
	}
	r, err = methods["ports.suggest"].call(eng, json.RawMessage(`{"after":65534}`))
	if err != nil || r.(*PortSuggestion).Port != 65535 {
		t.Fatalf("%v %v", r, err)
	}
}

func TestPortsErrorCodes(t *testing.T) {
	for err, want := range map[error]string{
		ErrPortNoCandidate:       CodeNotFound,
		ErrPortBusy:              CodeBusy,
		ErrPortInvalid:           CodeBadParams,
		context.DeadlineExceeded: CodeEngine,
	} {
		_, got := methods["ports.suggest"].call(&portEngine{err: err}, json.RawMessage(`{"after":3000}`))
		if wireCode(t, got) != want {
			t.Fatalf("%v: %v", err, got)
		}
	}
}
