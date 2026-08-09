package tool

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type toolFixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadToolFixtures(t *testing.T, file string) []toolFixture {
	t.Helper()
	input, err := os.Open(file)
	if err != nil {
		t.Fatalf("open fixture %s: %v", file, err)
	}
	defer input.Close()
	var fixtures []toolFixture
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		var fixture toolFixture
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		fixtures = append(fixtures, fixture)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	return fixtures
}

func TestShellFixtureParity(t *testing.T) {
	fixtures := loadToolFixtures(t, "testdata/shell_fixtures.json")
	if len(fixtures) < 60 {
		t.Fatalf("fixture count = %d, want at least 60", len(fixtures))
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
				t.Fatal(err)
			}
			var got any
			switch fixture.Fn {
			case "name", "login", "posix", "ps", "toKind":
				var file string
				if err := json.Unmarshal(args[0], &file); err != nil {
					t.Fatal(err)
				}
				switch fixture.Fn {
				case "name":
					got = ShellName(file)
				case "login":
					got = ShellLogin(file)
				case "posix":
					got = ShellPosix(file)
				case "ps":
					got = ShellPowerShell(file)
				case "toKind":
					got = ShellKind(file)
				}
			case "args":
				var file, command, cwd string
				_ = json.Unmarshal(args[0], &file)
				_ = json.Unmarshal(args[1], &command)
				_ = json.Unmarshal(args[2], &cwd)
				got = ShellArgs(file, command, cwd)
			case "tail":
				var text string
				var maxLines, maxBytes int
				_ = json.Unmarshal(args[0], &text)
				_ = json.Unmarshal(args[1], &maxLines)
				_ = json.Unmarshal(args[2], &maxBytes)
				got = TailShellOutput(text, maxLines, maxBytes)
			case "preview":
				var text string
				_ = json.Unmarshal(args[0], &text)
				got = PreviewShellOutput(text)
			case "scan":
				var input struct {
					Command   string   `json:"command"`
					CWD       string   `json:"cwd"`
					Workspace string   `json:"workspace"`
					Home      string   `json:"home"`
					Dirs      []string `json:"dirs"`
				}
				if err := json.Unmarshal(args[0], &input); err != nil {
					t.Fatal(err)
				}
				dirs := map[string]bool{}
				for _, dir := range input.Dirs {
					dirs[dir] = true
				}
				got = ScanShellPermissions(input.Command, ShellScanOptions{
					CWD:       input.CWD,
					Workspace: input.Workspace,
					Shell:     "/bin/bash",
					Home:      input.Home,
					IsDir:     func(path string) bool { return dirs[path] },
				})
			default:
				t.Fatalf("unknown function %q", fixture.Fn)
			}
			data, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != fixture.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fixture.ArgsJSON, data, fixture.OutJSON)
			}
		})
	}
}
