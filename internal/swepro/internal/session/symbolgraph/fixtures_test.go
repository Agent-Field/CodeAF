package symbolgraph

import (
	"bufio"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer file.Close()
	out := []fixture{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<23)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var fx fixture
		if err := json.Unmarshal(scanner.Bytes(), &fx); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		out = append(out, fx)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

func customExtractors(mode string) SymbolExtractorMap {
	switch mode {
	case "empty-map":
		return SymbolExtractorMap{}
	case "txt-custom":
		return SymbolExtractorMap{
			"txt": func(source SourceFile) SymbolExtraction {
				return SymbolExtraction{
					Defs: []SymbolTag{{Name: "Custom_" + strconv.Itoa(len(source.Path)), Line: 7, Kind: KindConst}},
					Refs: []SymbolTag{{Name: "ExternalRef", Line: 8, Kind: KindUnknown}},
				}
			},
		}
	case "override-ts":
		return SymbolExtractorMap{
			"ts": func(SourceFile) SymbolExtraction {
				return SymbolExtraction{
					Defs: []SymbolTag{{Name: "Override", Line: 1, Kind: KindClass}},
					Refs: []SymbolTag{},
				}
			},
		}
	case "unicode-lower-i":
		return SymbolExtractorMap{
			"i": func(SourceFile) SymbolExtraction {
				return SymbolExtraction{
					Defs: []SymbolTag{{Name: "WrongSimpleLower", Line: 1, Kind: KindClass}},
					Refs: []SymbolTag{},
				}
			},
		}
	case "unicode-final-sigma":
		return SymbolExtractorMap{
			"ος": func(SourceFile) SymbolExtraction {
				return SymbolExtraction{
					Defs: []SymbolTag{{Name: "FinalSigmaHit", Line: 1, Kind: KindClass}},
					Refs: []SymbolTag{},
				}
			},
		}
	default:
		panic("unknown custom extractor mode " + mode)
	}
}

func callFixture(t *testing.T, fx fixture) any {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	switch fx.Fn {
	case "extractSymbols":
		var file SourceFile
		if err := json.Unmarshal(args[0], &file); err != nil {
			t.Fatal(err)
		}
		if len(args) == 1 {
			return ExtractSymbols(file)
		}
		var mode string
		if err := json.Unmarshal(args[1], &mode); err != nil {
			t.Fatal(err)
		}
		return ExtractSymbols(file, customExtractors(mode))
	case "buildSymbolGraph":
		var files []SourceFile
		if err := json.Unmarshal(args[0], &files); err != nil {
			t.Fatal(err)
		}
		return BuildSymbolGraph(files)
	default:
		t.Fatalf("unknown function %q", fx.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 50 {
		t.Fatalf("expected at least 50 fixtures, got %d", len(fixtures))
	}
	for _, fx := range fixtures {
		t.Run(fx.Fn+"/"+fx.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(callFixture(t, fx))
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(got) != fx.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}
}

func TestFixtureCoverage(t *testing.T) {
	seen := map[string]int{}
	for _, fx := range loadFixtures(t) {
		seen[fx.Fn]++
	}
	for _, name := range []string{"extractSymbols", "buildSymbolGraph"} {
		if seen[name] == 0 {
			t.Errorf("no fixtures for %s", name)
		}
	}
}
