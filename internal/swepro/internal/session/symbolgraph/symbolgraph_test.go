package symbolgraph

import "testing"

func TestJavaScriptWhitespaceInDefinitions(t *testing.T) {
	got := ExtractSymbols(SourceFile{
		Path:    "a.ts",
		Content: "export\u3000function\u00a0WideSpaceName() {}",
	})
	if len(got.Defs) != 1 || got.Defs[0].Name != "WideSpaceName" {
		t.Fatalf("unexpected definitions: %#v", got.Defs)
	}
}

func TestOrderedRecordArrayIndexKeys(t *testing.T) {
	record := NewOrderedRecord[int]()
	record.Set("10", 10)
	record.Set("2", 2)
	record.Set("01", 1)
	keys := record.Keys()
	if len(keys) != 3 || keys[0] != "2" || keys[1] != "10" || keys[2] != "01" {
		t.Fatalf("unexpected keys: %#v", keys)
	}
}

func TestPrototypeExtractorBugPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected the inherited __proto__ extractor to panic")
		}
	}()
	ExtractSymbols(SourceFile{Path: "a.__proto__", Content: "function RealName() {}"})
}
