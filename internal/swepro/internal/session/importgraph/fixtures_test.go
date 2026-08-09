package importgraph

import (
	"bufio"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Golden-fixture replay against testdata/fixtures.json, produced from the real
// src/session/import-graph.ts by tools/fixtures/gen-importgraph.ts. The gate is
// byte-for-byte equality between jscompat.Stringify(goResult) and the TS
// JSON.stringify output — not a semantic compare — so `[]` vs `null`, Map
// insertion order and UTF-16 sort order all fail loudly.
//
// ImportGraph results are compared through graphOut, which spreads each Map
// into its [key, value][] entry list exactly like the generator's
// `[...map.entries()]`. JSON.stringify of a Map is "{}", and
// Object.fromEntries would hoist integer-like paths to the front, so entry
// pairs are the only faithful shape.

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()

	var out []fixture
	sc := bufio.NewScanner(f)
	// The capContent boundary fixture carries a 5010-line source file.
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var fx fixture
		if err := json.Unmarshal([]byte(line), &fx); err != nil {
			t.Fatalf("decode fixture line: %v", err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

// extractorSets mirrors EXTRACTOR_SETS in tools/fixtures/gen-importgraph.ts
// byte-for-byte in behaviour; the fixtures name a set instead of serializing a
// function.
var rsUseRE = regexp.MustCompile(`use` + jsWSPlus + `crate::(\w+)`)

func extractorSets(key string) ImportExtractorMap {
	switch key {
	case "rs":
		return ImportExtractorMap{
			".rs": func(_p string, content string) []string {
				// `content.match(re)` with a non-global regex → first match only.
				m := rsUseRE.FindStringSubmatch(content)
				if m == nil {
					return []string{}
				}
				return []string{m[1] + ".rs"}
			},
		}
	case "tsOverride":
		return ImportExtractorMap{
			".ts": func(_p string, content string) []string {
				out := []string{}
				for _, l := range strings.Split(content, "\n") {
					if strings.HasPrefix(l, "@dep ") {
						out = append(out, l[5:])
					}
				}
				return out
			},
		}
	case "empty":
		// `{}` — non-nil, so the TS spread branch is taken.
		return ImportExtractorMap{}
	case "xyz":
		return ImportExtractorMap{
			".xyz": func(p string, _c string) []string {
				return []string{"marker:" + p}
			},
		}
	}
	return nil
}

// graphOut mirrors the generator's graphOut(): each Map becomes its
// [key, value][] entry list.
type graphOutJSON struct {
	Deps       [][]any `json:"deps"`
	Dependents [][]any `json:"dependents"`
	External   [][]any `json:"external"`
}

func mapEntries(m *jscompat.OrderedMap[string, []string]) [][]any {
	out := [][]any{}
	for _, e := range m.Entries() {
		v := e.Val
		if v == nil {
			v = []string{}
		}
		out = append(out, []any{e.Key, v})
	}
	return out
}

func graphOut(g ImportGraph) graphOutJSON {
	return graphOutJSON{
		Deps:       mapEntries(g.Deps),
		Dependents: mapEntries(g.Dependents),
		External:   mapEntries(g.External),
	}
}

type eiOptsJSON struct {
	KnownFiles    []string `json:"knownFiles"`
	ExtractorsKey *string  `json:"extractorsKey"`
}

type bigOptsJSON struct {
	ExtractorsKey *string `json:"extractorsKey"`
}

func stringify(t *testing.T, v any) string {
	t.Helper()
	b, err := jscompat.Stringify(v)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	return string(b)
}

func decodeString(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("decode string %s: %v", raw, err)
	}
	return s
}

func decodeFiles(t *testing.T, raw json.RawMessage) []SourceFile {
	t.Helper()
	files := []SourceFile{}
	if err := json.Unmarshal(raw, &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	return files
}

func TestFixtures(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", len(fixtures))
	}

	seen := map[string]int{}
	for _, fx := range fixtures {
		fx := fx
		seen[fx.Fn]++
		t.Run(fx.Name, func(t *testing.T) {
			var raw []json.RawMessage
			if err := json.Unmarshal([]byte(fx.ArgsJSON), &raw); err != nil {
				t.Fatalf("decode args: %v", err)
			}

			var got string
			switch fx.Fn {
			case "consts":
				got = stringify(t, struct {
					MaxImportScanLines  jscompat.JSNumber `json:"MAX_IMPORT_SCAN_LINES"`
					JSResolveExtensions []string          `json:"JS_RESOLVE_EXTENSIONS"`
				}{
					MaxImportScanLines:  jscompat.JSNumber(MAX_IMPORT_SCAN_LINES),
					JSResolveExtensions: JS_RESOLVE_EXTENSIONS,
				})

			case "extractTsJsImports":
				if len(raw) != 2 {
					t.Fatalf("extractTsJsImports expects 2 args, got %d", len(raw))
				}
				got = stringify(t, ExtractTsJsImports(decodeString(t, raw[0]), decodeString(t, raw[1])))

			case "extractPythonImports":
				if len(raw) != 2 {
					t.Fatalf("extractPythonImports expects 2 args, got %d", len(raw))
				}
				got = stringify(t, ExtractPythonImports(decodeString(t, raw[0]), decodeString(t, raw[1])))

			case "extractGoImports":
				if len(raw) != 2 {
					t.Fatalf("extractGoImports expects 2 args, got %d", len(raw))
				}
				got = stringify(t, ExtractGoImports(decodeString(t, raw[0]), decodeString(t, raw[1])))

			case "extractImports":
				if len(raw) != 3 {
					t.Fatalf("extractImports expects 3 args, got %d", len(raw))
				}
				var oj *eiOptsJSON
				if err := json.Unmarshal(raw[2], &oj); err != nil {
					t.Fatalf("decode extractImports opts: %v", err)
				}
				var opts *ExtractImportsOptions
				if oj != nil {
					opts = &ExtractImportsOptions{KnownFiles: oj.KnownFiles}
					if oj.ExtractorsKey != nil {
						set := extractorSets(*oj.ExtractorsKey)
						if set == nil {
							t.Fatalf("unknown extractorsKey %q", *oj.ExtractorsKey)
						}
						opts.Extractors = set
					}
				}
				got = stringify(t, ExtractImports(decodeString(t, raw[0]), decodeString(t, raw[1]), opts))

			case "buildImportGraph":
				if len(raw) != 2 {
					t.Fatalf("buildImportGraph expects 2 args, got %d", len(raw))
				}
				var oj *bigOptsJSON
				if err := json.Unmarshal(raw[1], &oj); err != nil {
					t.Fatalf("decode buildImportGraph opts: %v", err)
				}
				var opts *BuildImportGraphOptions
				if oj != nil && oj.ExtractorsKey != nil {
					set := extractorSets(*oj.ExtractorsKey)
					if set == nil {
						t.Fatalf("unknown extractorsKey %q", *oj.ExtractorsKey)
					}
					opts = &BuildImportGraphOptions{Extractors: set}
				}
				got = stringify(t, graphOut(BuildImportGraph(decodeFiles(t, raw[0]), opts)))

			case "dependenciesOf":
				if len(raw) != 2 {
					t.Fatalf("dependenciesOf expects 2 args, got %d", len(raw))
				}
				g := BuildImportGraph(decodeFiles(t, raw[0]), nil)
				got = stringify(t, DependenciesOf(g, decodeString(t, raw[1])))

			case "dependentsOf":
				if len(raw) != 2 {
					t.Fatalf("dependentsOf expects 2 args, got %d", len(raw))
				}
				g := BuildImportGraph(decodeFiles(t, raw[0]), nil)
				got = stringify(t, DependentsOf(g, decodeString(t, raw[1])))

			default:
				t.Fatalf("unknown fn %q", fx.Fn)
			}

			if got != fx.OutJSON {
				t.Fatalf("args=%.400s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}

	for _, fn := range []string{
		"consts",
		"extractTsJsImports",
		"extractPythonImports",
		"extractGoImports",
		"extractImports",
		"buildImportGraph",
		"dependenciesOf",
		"dependentsOf",
	} {
		if seen[fn] == 0 {
			t.Errorf("no fixture coverage for exported fn %q", fn)
		}
	}
}
