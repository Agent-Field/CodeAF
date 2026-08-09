// This file replays fixtures from swe-pro/src/config/*.ts at commit 3b25a1a.
package config

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer file.Close()
	out := []fixture{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for scanner.Scan() {
		var item fixture
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		out = append(out, item)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

func fixtureArgs(t *testing.T, item fixture) []json.RawMessage {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(item.ArgsJSON), &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	return args
}

func fixtureString(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode string: %v", err)
	}
	return value
}

func callFixture(t *testing.T, item fixture) any {
	t.Helper()
	args := fixtureArgs(t, item)
	switch item.Fn {
	case "parseEnv":
		name := fixtureString(t, args[0])
		var raw *string
		if string(args[1]) != "null" {
			value := fixtureString(t, args[1])
			raw = &value
		}
		return ParseEnvValue(name, raw)
	case "configEntryNameFromPath":
		var roots []string
		if err := json.Unmarshal(args[1], &roots); err != nil {
			t.Fatalf("decode roots: %v", err)
		}
		return ConfigEntryNameFromPath(fixtureString(t, args[0]), roots)
	case "files":
		return Files(fixtureString(t, args[0]))
	case "shell":
		return Shell(fixtureString(t, args[0]))
	case "fallbackSanitization":
		return FallbackSanitization(fixtureString(t, args[0]))
	case "parseManagedPlist":
		value, err := ParseManagedPlist(fixtureString(t, args[0]))
		if err != nil {
			t.Fatalf("ParseManagedPlist: %v", err)
		}
		return value
	case "pluginSpecifier", "pluginOptions":
		var spec PluginSpec
		if err := json.Unmarshal(args[0], &spec); err != nil {
			t.Fatalf("decode plugin spec: %v", err)
		}
		if item.Fn == "pluginSpecifier" {
			return PluginSpecifier(spec)
		}
		return PluginOptions(spec)
	case "deduplicatePluginOrigins":
		var origins []PluginOrigin
		if err := json.Unmarshal(args[0], &origins); err != nil {
			t.Fatalf("decode origins: %v", err)
		}
		return DeduplicatePluginOrigins(origins)
	case "matchesAnyGlob":
		var globs []string
		if err := json.Unmarshal(args[1], &globs); err != nil {
			t.Fatalf("decode globs: %v", err)
		}
		return MatchesAnyGlob(fixtureString(t, args[0]), globs)
	case "fileInDirectory":
		return FileInDirectory(fixtureString(t, args[0]), fixtureString(t, args[1]))
	case "jsonc":
		value, err := ParseJSONC(fixtureString(t, args[0]), fixtureString(t, args[1]))
		if err != nil {
			t.Fatalf("ParseJSONC: %v", err)
		}
		return value
	case "substituteEnv":
		value, err := Substitute(SubstituteInput{
			Text: fixtureString(t, args[0]), Dir: "/tmp", Source: "fixture",
			Lookup: func(name string) (string, bool) {
				if name == "CONFIG_FIXTURE_VALUE" {
					return "hello", true
				}
				return "", false
			},
		})
		if err != nil {
			t.Fatalf("Substitute: %v", err)
		}
		return value
	default:
		t.Fatalf("unknown fixture function %q", item.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) != 1050 {
		t.Fatalf("fixture count = %d, want 1050", len(fixtures))
	}
	for _, item := range fixtures {
		t.Run(item.Name, func(t *testing.T) {
			got := callFixture(t, item)
			data, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(data) != item.OutJSON {
				t.Fatalf("args=%s\n got: %s\nwant: %s", item.ArgsJSON, data, item.OutJSON)
			}
		})
	}
}

func TestEnvironmentInventory(t *testing.T) {
	if len(VariableNames) != 144 {
		t.Fatalf("VariableNames count = %d, want 144", len(VariableNames))
	}
	got := append([]string(nil), VariableNames...)
	sort.Strings(got)
	for index := 1; index < len(got); index++ {
		if got[index] == got[index-1] {
			t.Fatalf("duplicate environment variable %q", got[index])
		}
	}
	fixtureNames := map[string]int{}
	for _, item := range loadFixtures(t) {
		if item.Fn != "parseEnv" {
			continue
		}
		args := fixtureArgs(t, item)
		fixtureNames[fixtureString(t, args[0])]++
	}
	if len(fixtureNames) != 144 {
		t.Fatalf("fixture environment names = %d, want 144", len(fixtureNames))
	}
	for _, name := range VariableNames {
		if fixtureNames[name] != 7 {
			t.Errorf("%s fixture rows = %d, want 7", name, fixtureNames[name])
		}
	}
}

func TestNoStrconvParseBool(t *testing.T) {
	data, err := os.ReadFile("env.go")
	if err != nil {
		t.Fatal(err)
	}
	if stringContains(string(data), "strconv.ParseBool(") {
		t.Fatal("environment layer must use exact source comparisons, not strconv.ParseBool")
	}
}

func stringContains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}
