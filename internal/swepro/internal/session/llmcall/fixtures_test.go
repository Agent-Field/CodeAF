package llmcall

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
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
			var args [][]msgmodel.ModelMessage
			if err := json.Unmarshal([]byte(f.ArgsJSON), &args); err != nil {
				t.Fatal(err)
			}
			got, err := jscompat.Stringify(HasToolCalls(args[0]))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != f.OutJSON {
				t.Fatalf("got %s want %s", got, f.OutJSON)
			}
		})
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
}
