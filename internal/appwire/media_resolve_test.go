package appwire

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

type mediaResolveEngine struct {
	fakeEngine
	agent, path, version string
	source               MediaSource
	err                  error
}

func (m *mediaResolveEngine) ResolveMediaReference(agent, path, version string) (MediaSource, error) {
	m.agent, m.path, m.version = agent, path, version
	return m.source, m.err
}
func TestMediaResolveOptionalTypedRoutePreservesDescriptorAndErrors(t *testing.T) {
	method := methods["media.resolve"]
	eng := &mediaResolveEngine{source: MediaSource{ID: "actual-source", Origin: "evidence", State: MediaAvailable, Version: "sha256:actual", Tool: "look", Run: "run"}}
	out, err := method.call(eng, json.RawMessage(`{"agent":"owner","path":"/actual/chart.png","expect_version":"file:actual"}`))
	if err != nil || eng.agent != "owner" || eng.path != "/actual/chart.png" || eng.version != "file:actual" {
		t.Fatalf("route %+v %v", eng, err)
	}
	got := out.(*MediaResult).Source
	if got.ID != "actual-source" || got.Origin != "evidence" || got.Tool != "look" || got.Version != "sha256:actual" {
		t.Fatalf("rewrote source %+v", got)
	}
	for _, raw := range []string{`{}`, `{"agent":"owner"}`, `{"agent":"owner","path":"/a\u0000b"}`, `{"agent":"owner","path":"` + strings.Repeat("x", 4097) + `"}`} {
		_, err = method.call(eng, json.RawMessage(raw))
		var wire *Error
		if !errors.As(err, &wire) || wire.Code != CodeBadParams {
			t.Fatalf("bad input %v", err)
		}
	}
	_, err = method.call(&fakeEngine{}, json.RawMessage(`{"agent":"owner","path":"/actual"}`))
	var wire *Error
	if !errors.As(err, &wire) || wire.Code != CodeUnknownMethod {
		t.Fatalf("optional %v", err)
	}
	eng.err = Errorf(CodeBusy, "not finished")
	_, err = method.call(eng, json.RawMessage(`{"agent":"owner","path":"/actual"}`))
	if !errors.As(err, &wire) || wire.Code != CodeBusy {
		t.Fatalf("lost busy %v", err)
	}
	eng.err = nil
	eng.source.Said = strings.Repeat("x", 300<<10)
	if _, err = method.call(eng, json.RawMessage(`{"agent":"owner","path":"/actual"}`)); err == nil {
		t.Fatal("oversize reply")
	}
}

func TestMediaResolveFeatureAdvertisesOnlySupportedCapability(t *testing.T) {
	supported := withMessageReadsFeature(Hello{}, &mediaResolveEngine{})
	if !slices.Contains(supported.Features, FeatureMediaResolve) {
		t.Fatal("resolve feature absent")
	}
	supported = withMessageReadsFeature(supported, &mediaResolveEngine{})
	count := 0
	for _, feature := range supported.Features {
		if feature == FeatureMediaResolve {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicated feature %d", count)
	}
	if slices.Contains(withMessageReadsFeature(Hello{}, &fakeEngine{}).Features, FeatureMediaResolve) {
		t.Fatal("unsupported advertised")
	}
}
