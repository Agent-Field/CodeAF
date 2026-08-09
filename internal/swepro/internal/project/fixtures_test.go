// This file replays pure fixtures from swe-pro/src/project at commit 3b25a1a.
package project

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func TestFixtureParity(t *testing.T) {
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		count++
		var item fixture
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			t.Fatal(err)
		}
		var args []json.RawMessage
		if err := json.Unmarshal([]byte(item.ArgsJSON), &args); err != nil {
			t.Fatal(err)
		}
		var path string
		var instance InstanceContext
		if err := json.Unmarshal(args[0], &path); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(args[1], &instance); err != nil {
			t.Fatal(err)
		}
		var got any
		switch item.Fn {
		case "containsPath":
			got = ContainsPath(path, instance)
		case "redirectIntoDirectory":
			got = RedirectIntoDirectory(path, instance)
		default:
			t.Fatalf("unknown fixture function %q", item.Fn)
		}
		data, err := jscompat.Stringify(got)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != item.OutJSON {
			t.Fatalf("%s args=%s\n got: %s\nwant: %s", item.Name, item.ArgsJSON, data, item.OutJSON)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 16 {
		t.Fatalf("fixture count = %d, want 16", count)
	}
}
