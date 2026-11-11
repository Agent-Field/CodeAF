package appwire

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

type mediaFrameEngine struct {
	fakeEngine
	agent, source, version string
	at                     int64
	png                    string
	calls                  int
}

func (e *mediaFrameEngine) AttachMediaFrame(agent, source, version string, at int64, png string) (Attachment, error) {
	e.calls++
	e.agent, e.source, e.version, e.at, e.png = agent, source, version, at, png
	return Attachment{ID: "owned"}, nil
}

// The route bounds the encoded length and hands it on still encoded: the engine
// decodes only after it has admitted the work.
func TestMediaAttachFrameRouteDoesNotDecodeBeforeAdmission(t *testing.T) {
	e := &mediaFrameEngine{}
	text := "not base64 !!"
	if _, err := methods["media.attach_frame"].call(e, json.RawMessage(`{"agent":"a","source":"s","version":"v","at_ms":1,"png":"`+text+`"}`)); err != nil || e.png != text {
		t.Fatalf("the route decoded or refused before the engine could admit: %v %q", err, e.png)
	}
}

func TestMediaAttachFrameRouteTakesBytesNeverAPath(t *testing.T) {
	e := &mediaFrameEngine{}
	route := methods["media.attach_frame"]
	body := base64.StdEncoding.EncodeToString([]byte("png bytes"))
	out, err := route.call(e, json.RawMessage(`{"agent":"a","source":"src_x","version":"sha256:v","at_ms":3200,"png":"`+body+`"}`))
	if err != nil || e.agent != "a" || e.source != "src_x" || e.version != "sha256:v" || e.at != 3200 || e.png != body {
		t.Fatalf("route lost identity: %v", err)
	}
	raw, _ := json.Marshal(out)
	if string(raw) != `{"attachment":{"id":"owned","kind":""}}` {
		t.Fatalf("reply: %s", raw)
	}
	// A path field is not part of the contract: it is ignored, never opened.
	if _, err := route.call(e, json.RawMessage(`{"agent":"a","source":"s","version":"v","at_ms":1,"png":"`+body+`","path":"/etc/passwd"}`)); err != nil {
		t.Fatal(err)
	}
	over := strings.Repeat("A", base64.StdEncoding.EncodedLen(MediaFrameMaxBytes)+4)
	bad := []string{
		`{}`,
		`{"agent":"a","source":"s","version":"v","at_ms":1}`,
		`{"agent":"a","source":"s","version":"v","at_ms":-1,"png":"` + body + `"}`,
		`{"agent":"a","source":"s","version":" ","at_ms":1,"png":"` + body + `"}`,
		`{"agent":"a","source":"s","version":"v","at_ms":1,"png":"` + over + `"}`,
	}
	before := e.calls
	for _, text := range bad {
		if _, err := route.call(e, json.RawMessage(text)); codeOf(err) != CodeBadParams {
			t.Fatalf("bad request accepted: %.60s %v", text, err)
		}
	}
	if e.calls != before {
		t.Fatal("a refused request reached the engine")
	}
	if _, err := route.call(&fakeEngine{}, json.RawMessage(`{"agent":"a","source":"s","version":"v","at_ms":1,"png":"`+body+`"}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatal("missing door pretended available")
	}
	has := func(h Hello) bool {
		for _, f := range h.Features {
			if f == FeatureMediaAttachFrame {
				return true
			}
		}
		return false
	}
	if !has(withMessageReadsFeature(Hello{}, e)) || has(withMessageReadsFeature(Hello{}, &fakeEngine{})) {
		t.Fatal("wrong capability advertisement")
	}
}
