package appwire

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type mediaEngine struct {
	fakeEngine
	agent, cursor, source string
	limit                 int
}

func (m *mediaEngine) ListMedia(agent, cursor string, limit int) (MediaPage, error) {
	m.agent, m.cursor, m.limit = agent, cursor, limit
	return MediaPage{}, nil
}

func (m *mediaEngine) GetMedia(agent, source string) (MediaSource, error) {
	m.agent, m.source = agent, source
	return MediaSource{ID: source, Kind: "image", Origin: "generated", State: MediaAvailable, Charge: "unknown"}, nil
}

// MEDIA.LIST and MEDIA.GET are an optional capability: an engine that cannot
// serve them is unknown_method, the limit is clamped, an empty page is an
// empty list and not null, and the params carry no path.
func TestMediaRoutes(t *testing.T) {
	list, get := methods["media.list"], methods["media.get"]
	if list.call == nil || get.call == nil {
		t.Fatal("media methods are not registered")
	}
	eng := &mediaEngine{}
	out, err := list.call(eng, json.RawMessage(`{"agent":"a1","cursor":"c","limit":99999}`))
	if err != nil || eng.agent != "a1" || eng.cursor != "c" || eng.limit != MediaPageMax {
		t.Fatalf("list: %v %+v", err, eng)
	}
	if raw, _ := json.Marshal(out); string(raw) != `{"sources":[]}` {
		t.Fatalf("an empty page is %s", raw)
	}
	if _, err := list.call(eng, json.RawMessage(`{"agent":"a1"}`)); err != nil || eng.limit != MediaPageDefault {
		t.Fatalf("default limit %d (%v)", eng.limit, err)
	}
	if _, err := get.call(eng, json.RawMessage(`{"agent":"a1","source":"src_x"}`)); err != nil || eng.source != "src_x" {
		t.Fatalf("get: %v", err)
	}
	var wire *Error
	for _, bad := range []string{`{}`, `{"agent":"a1","source":" "}`, `{"source":"s"}`} {
		if _, err := get.call(eng, json.RawMessage(bad)); !errors.As(err, &wire) || wire.Code != CodeBadParams {
			t.Fatalf("get %s answered %v", bad, err)
		}
	}
	if _, err := list.call(eng, json.RawMessage(`{"agent":"a1","limit":-1}`)); !errors.As(err, &wire) || wire.Code != CodeBadParams {
		t.Fatalf("negative limit answered %v", err)
	}
	for _, m := range []method{list, get} {
		if _, err := m.call(&fakeEngine{}, json.RawMessage(`{"agent":"a1","source":"s"}`)); !errors.As(err, &wire) || wire.Code != CodeUnknownMethod {
			t.Fatalf("an engine without media answered %v", err)
		}
	}
}

// A record past the field bounds is refused before it is marshaled, escaping
// counts at its worst, and a page past the reply budget is refused whole.
func TestMediaReplyBoundsAreChecked(t *testing.T) {
	ok := MediaSource{ID: "src_x", Kind: "image", Origin: "generated", State: MediaAvailable, Charge: "unknown", Said: "<>&"}
	if size, err := MediaSourceSize(ok); err != nil || size < 20 {
		t.Fatalf("a small record: %d %v", size, err)
	}
	// Escaping is counted exactly: '<' becomes six bytes.
	plain, _ := MediaSourceSize(MediaSource{Said: strings.Repeat("a", 1000)})
	escaped, _ := MediaSourceSize(MediaSource{Said: strings.Repeat("<", 1000)})
	if escaped-plain < 5000 {
		t.Fatalf("escaping is not counted: %d vs %d", escaped, plain)
	}
	for name, bad := range map[string]MediaSource{
		"a huge string":         {Said: strings.Repeat("x", mediaStringMax+1)},
		"too many references":   {References: make([]string, mediaArrayMax+1)},
		"a huge reference":      {References: []string{strings.Repeat("x", mediaStringMax+1)}},
		"worst-case escaping":   {Said: strings.Repeat("<", 200<<10), Title: strings.Repeat("<", 200<<10)},
		"many long references":  {References: repeated(200, strings.Repeat("r", 20<<10))},
		"a long path and title": {Path: strings.Repeat("p", 200<<10), Title: strings.Repeat("t", 200<<10)},
	} {
		if _, err := MediaSourceSize(bad); !errors.Is(err, ErrMediaTooLarge) {
			t.Errorf("%s answered %v", name, err)
		}
	}

	m := methods["media.list"]
	big := make([]MediaSource, 40)
	for i := range big {
		big[i] = MediaSource{ID: "src_x", Said: strings.Repeat("<", 30<<10)} // ~180 KiB each as JSON
	}
	var wire *Error
	if _, err := m.call(&bigEngine{page: MediaPage{Sources: big}}, json.RawMessage(`{"agent":"a"}`)); !errors.As(err, &wire) || wire.Code != CodeEngine {
		t.Fatalf("a page of 7 MiB answered %v", err)
	}
	if _, err := m.call(&bigEngine{page: MediaPage{Sources: big[:10]}}, json.RawMessage(`{"agent":"a"}`)); err != nil {
		t.Fatalf("a page of 1.8 MiB answered %v", err)
	}
	if _, err := methods["media.get"].call(&bigEngine{one: MediaSource{Said: strings.Repeat("x", 300<<10)}}, json.RawMessage(`{"agent":"a","source":"s"}`)); !errors.As(err, &wire) || wire.Code != CodeEngine {
		t.Fatalf("an oversize get answered %v", err)
	}
}

func repeated(n int, s string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

type bigEngine struct {
	fakeEngine
	page MediaPage
	one  MediaSource
}

func (b *bigEngine) ListMedia(string, string, int) (MediaPage, error) { return b.page, nil }
func (b *bigEngine) GetMedia(string, string) (MediaSource, error)     { return b.one, nil }

// A document source carries its parser facts as optional fields: an older
// client's source (no page count, no text layer) still decodes, an unknown fact
// is absent, and a KNOWN false text layer is present and is not mistaken for
// absence.
func TestMediaSourceDocumentFactsAreOptionalAndFalseSurvives(t *testing.T) {
	old := `{"id":"src_x","kind":"image","origin":"generated","path":"/w/a.png","state":"available","recorded":true,"created":"2026-10-16T00:00:00Z","charge":"unknown"}`
	var decoded MediaSource
	if err := json.Unmarshal([]byte(old), &decoded); err != nil || decoded.PageCount != nil || decoded.TextLayer != nil {
		t.Fatalf("an old source decoded as %+v (%v)", decoded, err)
	}
	if raw, _ := json.Marshal(decoded); strings.Contains(string(raw), "page_count") || strings.Contains(string(raw), "text_layer") {
		t.Fatalf("unknown document facts were serialized: %s", raw)
	}
	pages, layer := 14, false
	doc := MediaSource{ID: "src_d", Kind: "document", Origin: "evidence", Verb: "read", PageCount: &pages, TextLayer: &layer, Charge: "unknown"}
	raw, _ := json.Marshal(doc)
	for _, want := range []string{`"kind":"document"`, `"page_count":14`, `"text_layer":false`, `"verb":"read"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("%s missing from %s", want, raw)
		}
	}
	var back MediaSource
	if err := json.Unmarshal(raw, &back); err != nil || back.PageCount == nil || *back.PageCount != 14 || back.TextLayer == nil || *back.TextLayer {
		t.Fatalf("round trip = %+v (%v)", back, err)
	}
}
