package appwire

import (
	"slices"
	"testing"
)

type captureLibrary struct {
	*fakeEngine
	KnowledgeLibrary
	got []KnowledgeCapture
}

func (l *captureLibrary) KnowledgeCapture(p KnowledgeCapture) error {
	l.got = append(l.got, p)
	return nil
}

func TestKnowledgeCaptureTakesAFileWithNoChat(t *testing.T) {
	lib := &captureLibrary{fakeEngine: &fakeEngine{}}
	if !slices.Contains(helloFeatures(t, lib), FeatureKnowledgeCaptureFile) {
		t.Fatal("an engine with a Library does not announce the file form")
	}
	if !slices.Contains(helloFeatures(t, lib), FeatureKnowledgeKeepAlways) {
		t.Fatal("an engine with a Library does not say a deliberate add works with learning off")
	}
	call := methods["knowledge.capture"]
	ok := []string{
		`{"file":{"workstream":"ws-1","id":"f-1","name":"Brief"},"text":"Ship on Fridays."}`,
		`{"chat":"c","message":"m","text":"words"}`,
	}
	for _, raw := range ok {
		if _, err := call.call(lib, []byte(raw)); err != nil {
			t.Fatalf("%s refused: %v", raw, err)
		}
	}
	bad := []string{
		`{"file":{"workstream":"ws-1","id":"f-1"},"text":"  "}`,
		`{"file":{"workstream":"","id":"f-1"},"text":"x"}`,
		`{"file":{"workstream":"ws-1","id":"f-1"},"chat":"c","text":"x"}`,
		`{"text":"no chat and no file"}`,
	}
	for _, raw := range bad {
		if _, err := call.call(lib, []byte(raw)); err == nil {
			t.Fatalf("%s accepted", raw)
		}
	}
	if len(lib.got) != 2 || lib.got[0].File == nil || lib.got[0].File.Name != "Brief" {
		t.Fatalf("handler saw %+v", lib.got)
	}
}
