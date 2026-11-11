package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

type mediaAttachEngine struct {
	fakeEngine
	agent, source, version string
}

func (e *mediaAttachEngine) AttachMedia(agent, source, version string) (Attachment, error) {
	e.agent, e.source, e.version = agent, source, version
	return Attachment{ID: "owned"}, nil
}
func TestMediaAttachRouteIsOpaqueAndAdvertised(t *testing.T) {
	e := &mediaAttachEngine{}
	route := methods["media.attach"]
	out, err := route.call(e, json.RawMessage(`{"agent":"a","source":"src_x","version":"sha256:recorded"}`))
	if err != nil || e.agent != "a" || e.source != "src_x" || e.version != "sha256:recorded" {
		t.Fatalf("route lost identity: %v", err)
	}
	raw, _ := json.Marshal(out)
	if string(raw) != `{"attachment":{"id":"owned","kind":""}}` {
		t.Fatalf("reply: %s", raw)
	}
	for _, bad := range []string{`{}`, `{"agent":"a","source":"src_x"}`, `{"agent":"a","source":"src_x","version":" "}`, `{"agent":"a","source":"` + strings.Repeat("s", 129) + `","version":"v"}`} {
		if _, err := route.call(e, json.RawMessage(bad)); codeOf(err) != CodeBadParams {
			t.Fatalf("bad request accepted: %v", err)
		}
	}
	if _, err := route.call(&fakeEngine{}, json.RawMessage(`{"agent":"a","source":"s","version":"v"}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatal("missing door pretended available")
	}
	has := func(h Hello) bool {
		for _, f := range h.Features {
			if f == FeatureMediaAttach {
				return true
			}
		}
		return false
	}
	if !has(withMessageReadsFeature(Hello{}, e)) || has(withMessageReadsFeature(Hello{}, &fakeEngine{})) {
		t.Fatal("wrong capability advertisement")
	}
}
