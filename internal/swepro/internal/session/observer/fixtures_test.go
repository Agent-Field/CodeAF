package observer

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

func TestFixtures(t *testing.T) {
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scan := bufio.NewScanner(file)
	for scan.Scan() {
		var f struct {
			Name     string `json:"name"`
			Fn       string `json:"fn"`
			ArgsJSON string `json:"args_json"`
			OutJSON  string `json:"out_json"`
		}
		if err := json.Unmarshal(scan.Bytes(), &f); err != nil {
			t.Fatal(err)
		}
		t.Run(f.Name, func(t *testing.T) {
			var out any
			switch f.Fn {
			case "evaluateTriggers":
				var args []TriggerInputs
				if err := json.Unmarshal([]byte(f.ArgsJSON), &args); err != nil {
					t.Fatal(err)
				}
				out = EvaluateTriggers(args[0])
			case "isObserverEnabled":
				switch strings.TrimPrefix(f.Name, "enabled-") {
				case "unset":
					t.Setenv("CODEAF_OBSERVER", "")
				case "one":
					t.Setenv("CODEAF_OBSERVER", "1")
				case "true-upper":
					t.Setenv("CODEAF_OBSERVER", "TRUE")
				case "false":
					t.Setenv("CODEAF_OBSERVER", "false")
				case "space":
					t.Setenv("CODEAF_OBSERVER", " true ")
				}
				out = IsObserverEnabled()
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
