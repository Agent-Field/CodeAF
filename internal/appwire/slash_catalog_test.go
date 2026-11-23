package appwire

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/slashcat"
)

// slashEngine serves slash.catalog with the programs it is given.
type slashEngine struct {
	*fakeEngine
	programs []SlashProgram
}

func (s *slashEngine) SlashPrograms() []SlashProgram { return s.programs }

func decodeSlash(t *testing.T, line map[string]any) SlashCatalogResult {
	t.Helper()
	if line["error"] != nil {
		t.Fatalf("slash.catalog failed: %v", line["error"])
	}
	raw, _ := json.Marshal(line["result"])
	var out SlashCatalogResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// THE CATALOGUE IS ANNOUNCED BY AN ENGINE THAT SERVES IT, is answered through
// the real server, and is unknown — with no hello feature — to one that cannot.
func TestSlashCatalogRoundtripAndHelloFeature(t *testing.T) {
	engine := &slashEngine{fakeEngine: &fakeEngine{}, programs: []SlashProgram{
		{Name: "review", Description: "review a pull request", BriefOptional: true, Lands: "text"},
		{Name: "senior-dev", Description: "hand the change over", Args: "<brief>", Lands: "tree"},
	}}
	w := startGraphWire(t, engine)
	found := false
	for _, f := range features(w.next()) {
		found = found || f == FeatureSlashCatalog
	}
	if !found {
		t.Fatal("hello does not announce slash.catalog")
	}
	w.send(`{"id":1,"method":"slash.catalog","params":{}}`)
	got := decodeSlash(t, w.next())
	rows := slashcat.Rows()
	want := slashcat.Commands(rows)
	if len(got.Commands) != len(want)+2 || len(got.Skipped) != 0 {
		t.Fatalf("%d commands (want %d+2), skipped %v", len(got.Commands), len(want), got.Skipped)
	}
	for i, c := range want {
		g := got.Commands[i]
		if g.Name != c.Name || g.Source != SlashSourceTable || g.Host != SlashHostUnknown || len(g.Forms) != len(c.Forms) || strings.Join(g.Aliases, ",") != strings.Join(c.Aliases, ",") {
			t.Fatalf("command %d = %+v, table has %+v", i, g, c)
		}
	}
	review := got.Commands[len(want)]
	if review.Name != "review" || review.Source != SlashSourceProgram || !review.BriefOptional || review.Lands != "text" || review.Forms[0].Args != "<brief>" {
		t.Fatalf("review = %+v", review)
	}
	if got.Limits.MaxCommands != SlashCatalogCommandsMax {
		t.Fatalf("limits = %+v", got.Limits)
	}

	plain := startGraphWire(t, &fakeEngine{})
	for _, f := range features(plain.next()) {
		if f == FeatureSlashCatalog {
			t.Fatal("an engine without the capability announced it")
		}
	}
	plain.send(`{"id":1,"method":"slash.catalog","params":{}}`)
	if e := plain.next()["error"].(map[string]any); e["code"] != CodeUnknownMethod {
		t.Fatalf("plain engine answered %v", e)
	}
}

// THE WIRE IS THE TABLE, WORD FOR WORD: every canonical name, alias and argument
// form in internal/slashcat is on the wire exactly once, nothing else is, and the
// counts are the ones the manual and the protocol document state.
func TestSlashCatalogIsTheTableWordForWord(t *testing.T) {
	rows := slashcat.Rows()
	got := BuildSlashCatalog(nil)
	// E5: dev merges b5e7ef3ac6 and 2e835658e3 added processes, helpers,
	// atlas, setup, pair and devices to the shared table.
	if len(got.Commands) != 54 || len(rows) != 93 {
		t.Fatalf("the table has %d commands over %d rows; PROTOCOL.md states 54 over 93 — change both together", len(got.Commands), len(rows))
	}
	words, aliases, forms := map[string]string{}, 0, 0
	for _, c := range got.Commands {
		if prev, dup := words[c.Name]; dup {
			t.Fatalf("/%s is listed twice (%s)", c.Name, prev)
		}
		words[c.Name] = c.Name
		for _, a := range c.Aliases {
			if words[a] != "" {
				t.Fatalf("alias /%s collides with /%s", a, words[a])
			}
			words[a] = c.Name
			aliases++
		}
		forms += len(c.Forms)
	}
	if aliases != 31 || forms != len(rows) {
		t.Fatalf("%d aliases and %d forms on the wire; the table has 31 and %d", aliases, forms, len(rows))
	}
	for _, r := range rows {
		if words[r.Name] != r.Name {
			t.Errorf("/%s is not on the wire", r.Name)
		}
		for _, a := range r.Alias {
			if words[a] != r.Name {
				t.Errorf("/%s does not resolve to /%s on the wire", a, r.Name)
			}
		}
	}
	doc, err := os.ReadFile(protocolDoc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "the table is "+strconv.Itoa(len(got.Commands))+" commands, "+strconv.Itoa(len(rows))+" rows, "+strconv.Itoa(aliases)+" aliases") {
		t.Fatal("PROTOCOL.md does not state this table's counts")
	}
}

// EVERY ROW IS INSIDE THE BOUNDS THE ANSWER ADVERTISES, so no table edit can
// make the wire cut a word or pass a limit it states.
func TestSlashCatalogTableFitsItsBounds(t *testing.T) {
	got := BuildSlashCatalog(nil)
	if len(got.Commands) > SlashCatalogCommandsMax {
		t.Fatalf("%d commands over the cap of %d", len(got.Commands), SlashCatalogCommandsMax)
	}
	for _, c := range got.Commands {
		if len(c.Name) > SlashCatalogWordMax || !slashWord(c.Name) || len(c.Forms) > SlashCatalogFormsMax || len(c.Aliases) > SlashCatalogAliasesMax {
			t.Errorf("/%s is outside the bounds: %d forms, %d aliases", c.Name, len(c.Forms), len(c.Aliases))
		}
		for _, a := range c.Aliases {
			if len(a) > SlashCatalogWordMax {
				t.Errorf("alias /%s is longer than %d", a, SlashCatalogWordMax)
			}
		}
		for _, f := range c.Forms {
			if len(f.Args) > SlashCatalogTextMax || len(f.Description) > SlashCatalogTextMax || f.Description == "" {
				t.Errorf("/%s %q has an unbounded or empty form", c.Name, f.Args)
			}
		}
	}
}

// A PROGRAM CANNOT TAKE A WORD THE TABLE HAS, cannot be called something that is
// not a command word, cannot pass the cap, and is clipped on a rune boundary;
// each refusal is named and the rest are kept.
func TestSlashCatalogProgramsAreBounded(t *testing.T) {
	long := strings.Repeat("é", SlashCatalogTextMax)
	programs := []SlashProgram{
		{Name: "clear", Description: "shadows an alias"},
		{Name: "model", Description: "shadows a name"},
		{Name: "two words"},
		{Name: strings.Repeat("a", SlashCatalogWordMax+1)},
		{Name: "  "},
		{Name: "good", Description: long, Args: long, Lands: "elsewhere"},
		{Name: "good", Description: "a second program with a taken word"},
	}
	for i := range 200 {
		programs = append(programs, SlashProgram{Name: "p" + strconv.Itoa(i)})
	}
	got := BuildSlashCatalog(programs)
	if len(got.Commands) != SlashCatalogCommandsMax {
		t.Fatalf("%d commands, want the cap %d", len(got.Commands), SlashCatalogCommandsMax)
	}
	if len(got.Skipped) < 5 {
		t.Fatalf("skipped = %v", got.Skipped)
	}
	var good SlashCommand
	for _, c := range got.Commands {
		switch c.Name {
		case "clear", "model":
			if c.Source != SlashSourceTable {
				t.Errorf("/%s was taken by a program", c.Name)
			}
		case "good":
			good = c
		}
	}
	f := good.Forms[0]
	if good.Source != SlashSourceProgram || good.Lands != "" || len(f.Description) > SlashCatalogTextMax || len(f.Args) > SlashCatalogTextMax ||
		!strings.HasSuffix(f.Description, "…") || strings.ContainsRune(f.Description, 0xFFFD) {
		t.Fatalf("good = %+v", good)
	}
	if strings.Count(strings.Join(got.Skipped, "\n"), "good") != 1 {
		t.Fatalf("the duplicate program was not named once: %v", got.Skipped)
	}
}

// slash.catalog is in the generated schema, with its enums, so a client types it.
func TestSlashCatalogIsInTheSchema(t *testing.T) {
	raw, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Methods map[string]json.RawMessage `json:"methods"`
		Defs    map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Methods["slash.catalog"] == nil {
		t.Fatal("schema lacks slash.catalog")
	}
	cmd := string(doc.Defs["SlashCommand"])
	for _, want := range []string{`"source"`, `"table"`, `"program"`, `"host"`, `"unknown"`, `"brief_optional"`} {
		if !strings.Contains(cmd, want) {
			t.Errorf("SlashCommand lacks %s: %s", want, cmd)
		}
	}
}
