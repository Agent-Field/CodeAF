package scriptrunner

import (
	"bufio"
	"encoding/json"
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
	scanner := bufio.NewScanner(file)
	count := 0
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
		got, err := jscompat.Stringify(BuildScriptGuidance())
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != fx.OutJSON {
			t.Fatalf("%s:\ngot  %s\nwant %s", fx.Name, got, fx.OutJSON)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("fixture count = %d, want 1", count)
	}
}
