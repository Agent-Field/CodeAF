package system

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/assets"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func TestProviderFixtures(t *testing.T) {
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	markers := map[string]string{}
	for _, name := range []string{
		"anthropic", "default", "beast", "gemini", "gpt", "kimi", "codex", "trinity",
	} {
		value, ok := assets.Get("src/session/prompt/" + name + ".txt")
		if !ok {
			t.Fatalf("missing asset %s", name)
		}
		markers[value] = upper(name)
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var value fixture
		_ = json.Unmarshal(scanner.Bytes(), &value)
		t.Run(value.Name, func(t *testing.T) {
			var args []json.RawMessage
			_ = json.Unmarshal([]byte(value.ArgsJSON), &args)
			var model Model
			_ = json.Unmarshal(args[0], &model)
			selected := Provider(model)
			got, _ := jscompat.Stringify([]string{markers[selected[0]]})
			if string(got) != value.OutJSON {
				t.Errorf("got %s want %s", got, value.OutJSON)
			}
		})
	}
}

func TestEnvironmentAndSkills(t *testing.T) {
	service := &Service{
		Context: Context{
			Directory: "/repo/sub", Worktree: "/repo", Project: Project{VCS: "git"},
		},
		Now: func() time.Time {
			return time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
		},
		Platform: "linux",
	}
	got := service.Environment(Model{
		ProviderID: "openrouter", API: API{ID: "model/x"},
	})
	const want = "You are powered by the model named model/x. The exact model ID is openrouter/model/x\n" +
		"Here is some useful information about the environment you are running in:\n" +
		"<env>\n" +
		"  Working directory: /repo/sub\n" +
		"  Workspace root folder: /repo\n" +
		"  Is directory a git repo: yes\n" +
		"  Platform: linux\n" +
		"  Today's date: Tue Jul 28 2026\n" +
		"</env>"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("environment=%q", got)
	}
	if service.Skills(nil) != nil {
		t.Fatal("stripped skills service returned a block")
	}
}

func upper(value string) string {
	out := make([]byte, len(value))
	for i := range value {
		ch := value[i]
		if ch >= 'a' && ch <= 'z' {
			ch -= 'a' - 'A'
		}
		out[i] = ch
	}
	return string(out)
}
