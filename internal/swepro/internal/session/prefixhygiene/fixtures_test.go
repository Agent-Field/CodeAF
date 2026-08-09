package prefixhygiene

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

func TestTypeScriptFixtures(t *testing.T) {
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	seen := map[string]int{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var fx struct {
			Name     string `json:"name"`
			Fn       string `json:"fn"`
			ArgsJSON string `json:"args_json"`
			OutJSON  string `json:"out_json"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &fx); err != nil {
			t.Fatal(err)
		}
		var args []string
		if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
			t.Fatal(err)
		}
		var result any
		switch fx.Fn {
		case "checkPrefixHygiene":
			result = CheckPrefixHygiene(args[0])
		case "assertStablePrefix":
			result = AssertStablePrefix(args[0], args[1])
		default:
			t.Fatal(fmt.Errorf("unknown function %q", fx.Fn))
		}
		got, err := jscompat.Stringify(result)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != fx.OutJSON {
			t.Errorf("%s:\ngot  %s\nwant %s", fx.Name, got, fx.OutJSON)
		}
		seen[fx.Fn]++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"checkPrefixHygiene", "assertStablePrefix"} {
		if seen[name] == 0 {
			t.Errorf("no fixtures for %s", name)
		}
	}
}

func TestJSLineSeparatorsAreWhitespaceAndLineTerminators(t *testing.T) {
	for _, separator := range []string{"\u2028", "\u2029"} {
		result := CheckPrefixHygiene("Date:" + separator + "value")
		if result.Clean || len(result.Violations) != 1 {
			t.Fatalf("separator %q: %#v", separator, result)
		}
		if string(result.Violations[0].Excerpt) != "Date:"+separator+"value" {
			t.Fatalf("separator %q excerpt = %q", separator, result.Violations[0].Excerpt)
		}
	}
}
