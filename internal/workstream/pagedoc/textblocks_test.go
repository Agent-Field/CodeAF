package pagedoc

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"testing"
)

// WP-6d (g): the shared fixture fixes trimming, code-point offsets, JSON and Markdown.
func TestSuggestionKeepSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/suggestion-keep.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Before         json.RawMessage `json:"doc_before"`
		After          json.RawMessage `json:"doc_after"`
		OnlyE3         json.RawMessage `json:"doc_after_e3"`
		MarkdownBefore string          `json:"markdown_before"`
		MarkdownAfter  string          `json:"markdown_after"`
		Edits          []struct {
			ID, Find, Replace, Delete, Insert string
			Block                             []int
			Offset                            int
			BlockText                         string `json:"block_text"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	root, err := Decode(fixture.Before)
	if err != nil {
		t.Fatal(err)
	}
	for _, edit := range fixture.Edits {
		block, ok := Locate(root, edit.Block, edit.BlockText)
		if !ok || !slices.Equal(block.Path, edit.Block) {
			t.Fatalf("%s block: %+v", edit.ID, block)
		}
		prefix, del, ins := TrimEdit(edit.Find, edit.Replace)
		// Locate the quoted text in code points, independently of byte indexes.
		text, find := []rune(block.Text), []rune(edit.Find)
		index := -1
		for i := 0; i+len(find) <= len(text); i++ {
			if slices.Equal(text[i:i+len(find)], find) {
				index = i
				break
			}
		}
		if index < 0 || index+prefix != edit.Offset || del != edit.Delete || ins != edit.Insert {
			t.Fatalf("%s trim: offset=%d delete=%q insert=%q", edit.ID, index+prefix, del, ins)
		}
		if err := Apply(block, edit.Offset, del, ins); err != nil {
			t.Fatal(err)
		}
	}
	want, err := Decode(fixture.After)
	if err != nil || !reflect.DeepEqual(root, want) {
		t.Fatalf("all edits: %+v; %v", root, err)
	}
	root, err = Decode(fixture.Before)
	if err != nil {
		t.Fatal(err)
	}
	e3 := fixture.Edits[2]
	block, ok := Locate(root, e3.Block, e3.BlockText)
	if !ok {
		t.Fatal("e3 missing")
	}
	if err := Apply(block, e3.Offset, e3.Delete, e3.Insert); err != nil {
		t.Fatal(err)
	}
	want, err = Decode(fixture.OnlyE3)
	if err != nil || !reflect.DeepEqual(root, want) {
		t.Fatal("e3 changed other text", err)
	}
	for _, test := range []struct {
		doc      json.RawMessage
		markdown string
	}{{fixture.Before, fixture.MarkdownBefore}, {fixture.After, fixture.MarkdownAfter}} {
		got, err := Markdown(test.doc)
		if err != nil || got != test.markdown {
			t.Fatalf("Markdown: %q; %v", got, err)
		}
	}
}

// WP-6d (g), Tool: trim stops at word boundaries, including pure insertion.
func TestSuggestionWordBoundaryTrim(t *testing.T) {
	for _, test := range []struct {
		find, replace string
		at            int
		del, ins      string
	}{
		{"within 5 minutes", "within 10 minutes", 7, "5", "10"},
		{"Retries reuse the first signature.", "Retries are signed again at send time.", 8, "reuse the first signature.", "are signed again at send time."},
		{"Signing", "Signing and retries", 7, "", " and retries"},
		{"foobar", "foobaz", 0, "foobar", "foobaz"},
		{"α β γ", "α δ γ", 2, "β", "δ"},
	} {
		at, del, ins := TrimEdit(test.find, test.replace)
		if at != test.at || del != test.del || ins != test.ins {
			t.Errorf("%q: %d %q %q", test.find, at, del, ins)
		}
	}
}

// WP-6d (g): replacement inherits first-deleted marks, insertion intersects
// both sides, ends inherit none, empty nodes vanish and exact numbers survive.
func TestSuggestionKeepMarksAndUnknownAttributes(t *testing.T) {
	for _, test := range []struct {
		name              string
		at                int
		del, ins, content string
	}{
		{"replacement", 1, "bc", "X", `[{"type":"text","text":"aX","marks":[{"type":"code"}]},{"type":"text","text":"d","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]}]`},
		{"intersection", 2, "", "X", `[{"type":"text","text":"ab","marks":[{"type":"code"}]},{"type":"text","text":"X"},{"type":"text","text":"cd","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]}]`},
		{"end", 4, "", "X", `[{"type":"text","text":"ab","marks":[{"type":"code"}]},{"type":"text","text":"cd","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]},{"type":"text","text":"X"}]`},
		{"start", 0, "", "X", `[{"type":"text","text":"X"},{"type":"text","text":"ab","marks":[{"type":"code"}]},{"type":"text","text":"cd","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]}]`},
		{"deletion", 0, "abcd", "", `[]`},
		{"same marks", 1, "", "X", `[{"type":"text","text":"aXb","marks":[{"type":"code"}]},{"type":"text","text":"cd","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := Decode(json.RawMessage(`{"type":"doc","unknown":9007199254740993,"content":[{"type":"paragraph","attrs":{"keep":9007199254740993},"content":[{"type":"text","text":"ab","marks":[{"type":"code"}]},{"type":"text","text":"cd","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]}]},{"type":"liveBlock","attrs":{"kind":"jobs"}}]}`))
			if err != nil {
				t.Fatal(err)
			}
			if err := Apply(TextBlocks(doc)[0], test.at, test.del, test.ins); err != nil {
				t.Fatal(err)
			}
			want, err := Decode(json.RawMessage(`{"type":"doc","unknown":9007199254740993,"content":[{"type":"paragraph","attrs":{"keep":9007199254740993},"content":` + test.content + `},{"type":"liveBlock","attrs":{"kind":"jobs"}}]}`))
			if err != nil || !reflect.DeepEqual(doc, want) {
				t.Fatalf("rewrote attributes or marks: %+v; %v", doc, err)
			}
		})
	}
}

// WP-6d (g): keeping an edit preserves every field on untouched text nodes.
func TestSuggestionKeepPreservesTextNodeFields(t *testing.T) {
	for _, test := range []struct {
		name, before, after string
	}{
		{
			"right-only attrs",
			`{"type":"text","text":" suffix"},{"type":"text","text":" tail","attrs":{"revision":9007199254740993}}`,
			`{"type":"text","text":" suffix"},{"type":"text","text":" tail","attrs":{"revision":9007199254740993}}`,
		},
		{
			"left-only field",
			`{"type":"text","text":" suffix","revision":9007199254740993},{"type":"text","text":" tail"}`,
			`{"type":"text","text":" suffix","revision":9007199254740993},{"type":"text","text":" tail"}`,
		},
		{
			"different nested attrs",
			`{"type":"text","text":" suffix","attrs":{"meta":{"revision":9007199254740992}}},{"type":"text","text":" tail","attrs":{"meta":{"revision":9007199254740993}}}`,
			`{"type":"text","text":" suffix","attrs":{"meta":{"revision":9007199254740992}}},{"type":"text","text":" tail","attrs":{"meta":{"revision":9007199254740993}}}`,
		},
		{
			"equal nested fields",
			`{"type":"text","text":" suffix","attrs":{"revision":9007199254740993},"extra":[{"value":1}]},{"type":"text","text":" tail","extra":[{"value":1}],"attrs":{"revision":9007199254740993}}`,
			`{"type":"text","text":" suffix tail","attrs":{"revision":9007199254740993},"extra":[{"value":1}]}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := Decode(json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Old","marks":[{"type":"code"}]},` + test.before + `]}]}`))
			if err != nil {
				t.Fatal(err)
			}
			if err := Apply(TextBlocks(doc)[0], 0, "Old", "New"); err != nil {
				t.Fatal(err)
			}
			want, err := Decode(json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"New","marks":[{"type":"code"}]},` + test.after + `]}]}`))
			if err != nil || !reflect.DeepEqual(doc, want) {
				t.Fatalf("rewrote untouched text fields: %+v; %v", doc, err)
			}
			if test.name == "right-only attrs" {
				tail := children(TextBlocks(doc)[0].Node)[2]
				if revision, ok := object(tail["attrs"])["revision"].(json.Number); !ok || revision.String() != "9007199254740993" {
					t.Fatalf("revision lost precision: %#v", tail["attrs"])
				}
			}
		})
	}
}

// WP-6d (g): absent and empty marks both mean no marks for keeps and insertions.
func TestSuggestionKeepTreatsEmptyAndAbsentMarksEqually(t *testing.T) {
	for _, test := range []struct {
		name, before, after string
		at                  int
		del, ins            string
	}{
		{"replacement", `[{"type":"text","text":"one","marks":[]},{"type":"text","text":" two"}]`, `[{"type":"text","text":"one three","marks":[]}]`, 4, "two", "three"},
		{"insertion", `[{"type":"text","text":"one","marks":[]},{"type":"text","text":" two"}]`, `[{"type":"text","text":"one more two","marks":[]}]`, 3, "", " more"},
		{"reverse insertion", `[{"type":"text","text":"one"},{"type":"text","text":" two","marks":[]}]`, `[{"type":"text","text":"one more two"}]`, 3, "", " more"},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := Decode(json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":` + test.before + `}]}`))
			if err != nil {
				t.Fatal(err)
			}
			if err := Apply(TextBlocks(doc)[0], test.at, test.del, test.ins); err != nil {
				t.Fatal(err)
			}
			want, err := Decode(json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":` + test.after + `}]}`))
			if err != nil || !reflect.DeepEqual(doc, want) {
				t.Fatalf("no-mark text did not merge: %+v; %v", doc, err)
			}
		})
	}
}

// WP-6d (d): path wins over duplicate text; fallback needs exactly one block,
// and nested list paragraphs retain their full content-array path.
func TestSuggestionLocatePathAndUniqueFallback(t *testing.T) {
	doc, err := Decode(json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Same"}]},{"type":"paragraph","content":[{"type":"text","text":"Same"}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Unique"}]}]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if block, ok := Locate(doc, []int{1}, "Same"); !ok || !slices.Equal(block.Path, []int{1}) {
		t.Fatalf("stored path: %+v %v", block, ok)
	}
	if _, ok := Locate(doc, []int{9}, "Same"); ok {
		t.Fatal("ambiguous fallback applied")
	}
	if block, ok := Locate(doc, []int{9}, "Unique"); !ok || !slices.Equal(block.Path, []int{2, 0, 0}) {
		t.Fatalf("nested fallback: %+v %v", block, ok)
	}
	if _, ok := Locate(doc, []int{0}, "Changed"); ok {
		t.Fatal("stale text applied")
	}
}
