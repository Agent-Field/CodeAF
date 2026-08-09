package inputclassifier

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
)

type fixture struct {
	Name     string `json:"name"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func TestPromptFixtures(t *testing.T) {
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var value fixture
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		t.Run(value.Name, func(t *testing.T) {
			var args []json.RawMessage
			_ = json.Unmarshal([]byte(value.ArgsJSON), &args)
			var input Input
			var outputPath string
			_ = json.Unmarshal(args[0], &input)
			_ = json.Unmarshal(args[1], &outputPath)
			got, _ := jscompat.Stringify(BuildPrompt(input, outputPath))
			if string(got) != value.OutJSON {
				t.Errorf("got %s\nwant %s", got, value.OutJSON)
			}
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaStrictnessAndUTF16Limits(t *testing.T) {
	valid := Schema.SafeParse(json.RawMessage(`{"class":"vague","reason":"why"}`))
	if !valid.Success() || valid.Data.Class != "vague" {
		t.Fatalf("valid=%#v", valid)
	}
	for _, raw := range []string{
		`{"class":"other","reason":"x"}`,
		`{"class":"focused","reason":"","extra":1}`,
		`{"class":"focused","reason":null}`,
		`{"class":"focused","reason":"` + repeat("😀", 1001) + `"}`,
	} {
		if Schema.SafeParse(json.RawMessage(raw)).Success() {
			t.Fatalf("unexpected success: %.80s", raw)
		}
	}
}

func TestDispatchUsesAgentJSONContract(t *testing.T) {
	workspace := t.TempDir()
	var request agentjson.Request
	deps := agentjson.Dependencies{
		Resolver: agentjson.ResolverFunc(func(baked.Tier) []string {
			return []string{"provider/model"}
		}),
		Client: agentjson.ClientFunc(func(_ context.Context, value agentjson.Request) error {
			request = value
			return os.WriteFile(
				filepath.Join(workspace, OutputRelativePath),
				[]byte(`{"class":"trivial","reason":"small"}`), 0o644,
			)
		}),
		NewID: func(prefix string) string { return prefix + "-id" },
	}
	result, err := Dispatch(context.Background(), Input{
		Workspace: workspace, ParentSessionID: "parent", UserPrompt: "Typo.",
	}, deps)
	if err != nil || result.Data.Class != "trivial" || !result.FirstTry ||
		result.UsedFallback {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if request.Agent != "input-classifier" ||
		request.TaskPrompt != BuildPrompt(Input{
			Workspace: workspace, ParentSessionID: "parent", UserPrompt: "Typo.",
		}, filepath.Join(workspace, OutputRelativePath)) {
		t.Fatalf("request=%#v", request)
	}
}

func TestDispatchFallsBackWithoutModel(t *testing.T) {
	result, err := Dispatch(context.Background(), Input{}, agentjson.Dependencies{
		Resolver: agentjson.ResolverFunc(func(baked.Tier) []string { return nil }),
	})
	if err != nil || !result.UsedFallback || result.Data != Fallback {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func repeat(value string, count int) string {
	out := ""
	for range count {
		out += value
	}
	return out
}
