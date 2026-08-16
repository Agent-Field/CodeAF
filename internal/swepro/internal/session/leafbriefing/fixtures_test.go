package leafbriefing

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

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

	out := []fixture{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var fx fixture
		if err := json.Unmarshal(sc.Bytes(), &fx); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

type selectFixtureArgs struct {
	FocusPaths      []string            `json:"focusPaths"`
	AllPaths        []string            `json:"allPaths"`
	Limit           *float64            `json:"limit"`
	ActivityEntries [][]json.RawMessage `json:"activity"`
}

func callFixture(t *testing.T, fx fixture) any {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	switch fx.Fn {
	case "buildAuditFixDelta":
		var input AuditFixDeltaArgs
		if len(args) != 1 || json.Unmarshal(args[0], &input) != nil {
			t.Fatalf("bad buildAuditFixDelta args: %s", fx.ArgsJSON)
		}
		return BuildAuditFixDelta(input)
	case "formatLeafBriefing":
		var input FormatLeafBriefingArgs
		if len(args) != 1 || json.Unmarshal(args[0], &input) != nil {
			t.Fatalf("bad formatLeafBriefing args: %s", fx.ArgsJSON)
		}
		return FormatLeafBriefing(input)
	case "selectSiblingExemplars":
		var input selectFixtureArgs
		if len(args) != 1 || json.Unmarshal(args[0], &input) != nil {
			t.Fatalf("bad selectSiblingExemplars args: %s", fx.ArgsJSON)
		}
		activity := map[string]float64{}
		for _, pair := range input.ActivityEntries {
			if len(pair) != 2 {
				t.Fatalf("bad activity pair in %s", fx.ArgsJSON)
			}
			var path string
			var value float64
			if err := json.Unmarshal(pair[0], &path); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(pair[1], &value); err != nil {
				t.Fatal(err)
			}
			activity[path] = value
		}
		return SelectSiblingExemplars(SelectSiblingExemplarsArgs{
			FocusPaths: input.FocusPaths,
			AllPaths:   input.AllPaths,
			Limit:      input.Limit,
			Activity:   activity,
		})
	case "extractFocusPaths":
		if len(args) < 1 || len(args) > 2 {
			t.Fatalf("bad extractFocusPaths args: %s", fx.ArgsJSON)
		}
		var text string
		if err := json.Unmarshal(args[0], &text); err != nil {
			t.Fatal(err)
		}
		if len(args) == 1 {
			return ExtractFocusPaths(text)
		}
		var limit float64
		if err := json.Unmarshal(args[1], &limit); err != nil {
			t.Fatal(err)
		}
		return ExtractFocusPaths(text, limit)
	default:
		t.Fatalf("unknown fixture function %q", fx.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 35 {
		t.Fatalf("expected at least 35 fixtures, got %d", len(fixtures))
	}
	for _, fx := range fixtures {
		t.Run(fx.Fn+"/"+fx.Name, func(t *testing.T) {
			got := callFixture(t, fx)
			encoded, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(encoded) != fx.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, encoded, fx.OutJSON)
			}
		})
	}
}

func TestFixtureCoverage(t *testing.T) {
	seen := map[string]int{}
	for _, fx := range loadFixtures(t) {
		seen[fx.Fn]++
	}
	for _, name := range []string{
		"buildAuditFixDelta",
		"formatLeafBriefing",
		"selectSiblingExemplars",
		"extractFocusPaths",
	} {
		if seen[name] == 0 {
			t.Errorf("no fixtures for %s", name)
		}
	}
}

type refsStub struct {
	seen map[string]string
}

func (r *refsStub) Register(kind, content string) ContextRegistration {
	if ref, ok := r.seen[content]; ok {
		return ContextRegistration{RefID: ref}
	}
	ref := "[ref:" + kind + "]"
	r.seen[content] = ref
	return ContextRegistration{RefID: ref, FirstMention: true}
}

func (r *refsStub) Render(refID string) string {
	return refID + " — repeated"
}

func TestFormatLeafBriefingDelegatesContextRefs(t *testing.T) {
	refs := &refsStub{seen: map[string]string{}}
	got := FormatLeafBriefing(FormatLeafBriefingArgs{
		Workspace: "ws",
		Sections: LeafBriefingSections{
			RepoMap:   "same",
			CoChange:  "same",
			Imports:   "",
			Exemplars: "other",
		},
		Refs: refs,
	})
	if !stringsContain(got, "[ref:leaf-briefing:repo-map:ws]\nsame") {
		t.Fatalf("first context block was not embedded: %q", got)
	}
	if !stringsContain(got, "[ref:leaf-briefing:repo-map:ws] — repeated") {
		t.Fatalf("repeat context block was not rendered: %q", got)
	}
}

func stringsContain(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
