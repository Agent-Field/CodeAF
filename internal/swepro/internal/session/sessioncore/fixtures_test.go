package sessioncore

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/engine/calc"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func TestFixtures(t *testing.T) {
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scan := bufio.NewScanner(file)
	for scan.Scan() {
		var f fixture
		if err := json.Unmarshal(scan.Bytes(), &f); err != nil {
			t.Fatal(err)
		}
		t.Run(f.Name, func(t *testing.T) {
			var out any
			switch f.Fn {
			case "isDefaultTitle":
				var args []string
				mustJSON(t, []byte(f.ArgsJSON), &args)
				out = IsDefaultTitle(args[0])
			case "fromRow":
				var args []Row
				mustJSON(t, []byte(f.ArgsJSON), &args)
				out = FromRow(args[0])
			case "toRow":
				var args []Info
				mustJSON(t, []byte(f.ArgsJSON), &args)
				out = ToRow(args[0])
			case "getUsage":
				var args []calc.GetUsageInput
				mustJSON(t, []byte(f.ArgsJSON), &args)
				if f.Name == "nonfinite" {
					nan := math.NaN()
					args[0].Usage.InputTokens = &nan
					args[0].Usage.TotalTokens = &nan
					inf := math.Inf(1)
					args[0].Usage.OutputTokens = &inf
				}
				out = GetUsage(args[0])
			default:
				t.Fatalf("unknown function %q", f.Fn)
			}
			got, err := jscompat.Stringify(out)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != f.OutJSON {
				t.Fatalf("got  %s\nwant %s", got, f.OutJSON)
			}
		})
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, data []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatal(err)
	}
}
