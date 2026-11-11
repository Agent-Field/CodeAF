package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
	"github.com/Agent-Field/codeaf/internal/slashcat"
)

// slashCatalogOverTheWire asks the real backend for slash.catalog through the
// real server and returns hello's features and the decoded answer.
func slashCatalogOverTheWire(t *testing.T) ([]string, appwire.SlashCatalogResult) {
	t.Helper()
	backend := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { backend.Close() })
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := appwire.NewServer(outW)
	go func() {
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, backend)
		outW.Close()
	}()
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)
	t.Cleanup(func() { inW.Close() })
	if !scanner.Scan() {
		t.Fatal("no hello")
	}
	var hello struct {
		Data struct {
			Features []string `json:"features"`
		} `json:"data"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &hello); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(inW, `{"id":1,"method":"slash.catalog","params":{}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	if !scanner.Scan() {
		t.Fatal("the wire ended")
	}
	var out struct {
		Result appwire.SlashCatalogResult `json:"result"`
		Error  map[string]any             `json:"error"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &out); err != nil || out.Error != nil {
		t.Fatalf("answer %s (%v)", scanner.Text(), err)
	}
	return hello.Data.Features, out.Result
}

// THE REAL BACKEND ANNOUNCES AND SERVES THE CATALOGUE: the table's commands and
// the programs THIS BUILD carries, taken from the same list the chat's rows are.
func TestAppSlashCatalogCarriesTheBuildsPrograms(t *testing.T) {
	features, got := slashCatalogOverTheWire(t)
	announced := false
	for _, f := range features {
		announced = announced || f == appwire.FeatureSlashCatalog
	}
	if !announced {
		t.Fatalf("hello features = %v", features)
	}
	table := slashcat.Commands(slashcat.Rows())
	programs := builtin.All()
	if len(got.Commands) != len(table)+len(programs) || len(got.Skipped) != 0 {
		t.Fatalf("%d commands, skipped %v; table %d + programs %d", len(got.Commands), got.Skipped, len(table), len(programs))
	}
	for i, p := range programs {
		c := got.Commands[len(table)+i]
		if c.Name != p.Name || c.Source != appwire.SlashSourceProgram || c.Host != appwire.SlashHostUnknown || len(c.Forms) != 1 {
			t.Errorf("program %s on the wire = %+v", p.Name, c)
		}
		if c.BriefOptional != (p.DefaultBrief != "") {
			t.Errorf("/%s brief_optional = %v, program default brief %q", p.Name, c.BriefOptional, p.DefaultBrief)
		}
	}
}

// A BUILD WITH NO PROGRAMS LISTS THE TABLE ALONE, and a program whose word is a
// command is named as skipped rather than shadowing it.
func TestAppSlashCatalogFollowsTheBuildList(t *testing.T) {
	restore := builtin.Override(nil)
	_, got := slashCatalogOverTheWire(t)
	restore()
	if len(got.Commands) != len(slashcat.Commands(slashcat.Rows())) || len(got.Skipped) != 0 {
		t.Fatalf("a build with no programs listed %d commands, skipped %v", len(got.Commands), got.Skipped)
	}
	restore = builtin.Override([]delegate.Delegate{{Name: "clear", Summary: "shadows /clear"}})
	defer restore()
	_, got = slashCatalogOverTheWire(t)
	if len(got.Skipped) != 1 {
		t.Fatalf("skipped = %v", got.Skipped)
	}
}
