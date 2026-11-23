package appwire

import (
	"encoding/json"
	"testing"
)

type mediaRangeEngine struct {
	fakeEngine
	agent, source, version string
	from, to               int64
	calls                  int
}

func (e *mediaRangeEngine) AttachMediaRange(agent, source, version string, from, to int64) (Attachment, error) {
	e.calls++
	e.agent, e.source, e.version, e.from, e.to = agent, source, version, from, to
	return Attachment{ID: "owned", Kind: AttachFile}, nil
}

func TestMediaAttachRangeRouteTakesIdentityAndSpanNeverAPath(t *testing.T) {
	e := &mediaRangeEngine{}
	route := methods["media.attach_range"]
	out, err := route.call(e, json.RawMessage(`{"agent":"a","source":"src_x","version":"sha256:v","from_ms":250,"to_ms":2000,"path":"/etc/passwd"}`))
	if err != nil || e.agent != "a" || e.source != "src_x" || e.version != "sha256:v" || e.from != 250 || e.to != 2000 {
		t.Fatalf("route lost identity: %v %+v", err, e)
	}
	raw, _ := json.Marshal(out)
	if string(raw) != `{"attachment":{"id":"owned","kind":"file"}}` {
		t.Fatalf("reply: %s", raw)
	}
	before := e.calls
	for _, text := range []string{
		`{}`,
		`{"agent":"a","source":"s","version":"v"}`,
		`{"agent":"a","source":"s","version":"v","from_ms":5,"to_ms":5}`,
		`{"agent":"a","source":"s","version":"v","from_ms":9,"to_ms":5}`,
		`{"agent":"a","source":"s","version":"v","from_ms":-1,"to_ms":5}`,
		`{"agent":"a","source":"s","version":"v","from_ms":0,"to_ms":31622400001}`,
		`{"agent":"a","source":"s","version":"v","from_ms":1.5,"to_ms":5}`,
		`{"agent":"a","source":"s","version":"v","from_ms":"1","to_ms":5}`,
		`{"agent":"a","source":"s","version":" ","from_ms":0,"to_ms":5}`,
		`{"agent":"","source":"s","version":"v","from_ms":0,"to_ms":5}`,
	} {
		if _, err := route.call(e, json.RawMessage(text)); codeOf(err) != CodeBadParams {
			t.Fatalf("a bad request was accepted: %s %v", text, err)
		}
	}
	if e.calls != before {
		t.Fatal("a refused request reached the engine")
	}
	if _, err := route.call(&fakeEngine{}, json.RawMessage(`{"agent":"a","source":"s","version":"v","from_ms":0,"to_ms":5}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatal("an engine without the door pretended to have it")
	}
}

func TestMediaAttachRangeIsAdvertisedOnlyByAnEngineThatServesIt(t *testing.T) {
	has := func(h Hello) bool {
		for _, f := range h.Features {
			if f == FeatureMediaAttachRange {
				return true
			}
		}
		return false
	}
	if !has(withMessageReadsFeature(Hello{}, &mediaRangeEngine{})) || has(withMessageReadsFeature(Hello{}, &fakeEngine{})) {
		t.Fatal("wrong capability advertisement")
	}
}

func TestAPictureSourceNamesNoRangeAndTheRangeWordsAreOptional(t *testing.T) {
	from, to := int64(1), int64(2)
	if err := checkMediaReference(&MediaReference{SourceID: "s", FromMS: &from, ToMS: &to}); codeOf(err) != CodeBadParams {
		t.Fatalf("a picture claimed a range: %v", err)
	}
	var plain MediaReference
	if err := json.Unmarshal([]byte(`{"source_id":"s","at_ms":5}`), &plain); err != nil || plain.FromMS != nil || plain.ToMS != nil {
		t.Fatalf("an old reference gained a range: %+v %v", plain, err)
	}
	raw, _ := json.Marshal(MediaReference{SourceID: "s", Version: "v", FromMS: &from, ToMS: &to})
	if string(raw) != `{"source_id":"s","version":"v","from_ms":1,"to_ms":2}` {
		t.Fatalf("wire spelling: %s", raw)
	}
}
