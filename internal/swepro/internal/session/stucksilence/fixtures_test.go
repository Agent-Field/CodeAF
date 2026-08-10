package stucksilence

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
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
		var args []StuckSilenceInput
		if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
			t.Fatal(err)
		}
		got, err := jscompat.Stringify(IsLeafStuck(args[0]))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != fx.OutJSON {
			t.Errorf("%s: got %s, want %s", fx.Name, got, fx.OutJSON)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 9 {
		t.Fatalf("fixture count = %d, want 9", count)
	}
}
