package appwire

import (
	"encoding/json"
	"reflect"
	"testing"
)

type manualEngine struct {
	sendEngine
	pages []ManualPage
	err   error
}

func (m *manualEngine) SetManualPages(pages []ManualPage) ([]string, error) {
	m.pages = pages
	names := make([]string, 0, len(pages))
	for _, p := range pages {
		names = append(names, "workaf/"+p.Name)
	}
	return names, m.err
}

// manual.corpus.set hands the engine every page whole and answers the names
// the manual files them under; the engine's refusal reaches the app as it said
// it, and an engine with no manual door answers unknown_method.
func TestManualCorpusSetHandsThePagesOver(t *testing.T) {
	set := methods["manual.corpus.set"]
	eng := &manualEngine{}
	r, err := set.call(eng, json.RawMessage(`{"pages":[{"name":"inbox","markdown":"# Inbox"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.(*ManualCorpusResult).Pages; !reflect.DeepEqual(got, []string{"workaf/inbox"}) || !reflect.DeepEqual(eng.pages, []ManualPage{{Name: "inbox", Markdown: "# Inbox"}}) {
		t.Fatalf("answered %v, the engine heard %+v", got, eng.pages)
	}
	if r, err := set.call(eng, json.RawMessage(`{"pages":[]}`)); err != nil || r.(*ManualCorpusResult).Pages == nil {
		t.Fatalf("an empty set answered %v %v", r, err)
	}
	eng.err = Errorf(CodeBadParams, "app page name \"Bad\" is not a lowercase slug")
	if _, err := set.call(eng, json.RawMessage(`{"pages":[{"name":"Bad","markdown":""}]}`)); codeOf(err) != CodeBadParams {
		t.Fatalf("a refused set answered %v", err)
	}
	if _, err := set.call(&sendEngine{}, json.RawMessage(`{"pages":[]}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatalf("an engine without the door answered %v", err)
	}
}
