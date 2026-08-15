package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRunContractSuccessAndFailure(t *testing.T) {
	workspace := t.TempDir()
	success := RunContract(workspace, Contract{
		Command: `printf out; printf err >&2`,
	})
	if !success.Pass || success.ExitCode != 0 || success.TimedOut {
		t.Fatalf("success result: %+v", success)
	}
	if success.TailOutput != "out\nerr" {
		t.Fatalf("combined output = %q", success.TailOutput)
	}

	failure := RunContract(workspace, Contract{Command: `printf bad >&2; exit 7`})
	if failure.Pass || failure.ExitCode != 7 || failure.TimedOut {
		t.Fatalf("failure result: %+v", failure)
	}
	if failure.TailOutput != "bad" {
		t.Fatalf("stderr tail = %q", failure.TailOutput)
	}
}

func TestRunContractTimeout(t *testing.T) {
	start := time.Now()
	result := RunContract(t.TempDir(), Contract{Command: "exec sleep 2"}, 20)
	if result.Pass || !result.TimedOut || result.ExitCode != 143 {
		t.Fatalf("timeout result: %+v", result)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("timeout took too long: %s", time.Since(start))
	}
}

func TestRunContractSpawnFailureNeverPanics(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "missing")
	result := RunContract(workspace, Contract{Command: "true"})
	if result.Pass || result.ExitCode != 1 || result.TimedOut {
		t.Fatalf("spawn failure result: %+v", result)
	}
	if !strings.HasPrefix(result.TailOutput, "contract command failed to spawn: ") {
		t.Fatalf("spawn output = %q", result.TailOutput)
	}
}

func TestReadContractFilesystemFailures(t *testing.T) {
	workspace := t.TempDir()
	if got := ReadContract(workspace); got != nil {
		t.Fatalf("missing contract = %+v", got)
	}
	path := filepath.Join(workspace, ContractPath)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ReadContract(workspace); got != nil {
		t.Fatalf("directory contract = %+v", got)
	}
}

func TestKeptDestructiveCommandBlocklistGaps(t *testing.T) {
	for _, command := range []string{"rm -r -f build", "rm --recursive --force build"} {
		got := ValidateContract(map[string]any{"command": command})
		if got == nil {
			t.Fatalf("TS blocklist gap was repaired for %q", command)
		}
	}
}

func TestTailKeepsOnlyLastUTF16Units(t *testing.T) {
	got := tail(strings.Repeat("a", outputTailChars+5), outputTailChars)
	if got != "…"+strings.Repeat("a", outputTailChars) {
		t.Fatalf("tail mismatch: len=%d", len(got))
	}
}

// TestValidateContractWithoutAssertedPaths pins resume tolerance: a
// contract.json written before asserted_paths existed must parse and behave
// exactly as it did then, since a mid-run resume rehydrates the persisted file.
func TestValidateContractWithoutAssertedPaths(t *testing.T) {
	var parsed any
	if err := json.Unmarshal(
		[]byte(`{"command":"./check.sh","paths":["check.sh"]}`), &parsed,
	); err != nil {
		t.Fatal(err)
	}
	got := ValidateContract(parsed)
	if got == nil {
		t.Fatal("legacy contract must still validate")
	}
	if got.Command != "./check.sh" ||
		!reflect.DeepEqual(got.Paths, []string{"check.sh"}) ||
		got.AssertedPaths != nil {
		t.Fatalf("legacy contract = %#v", got)
	}
}

// TestValidateContractAssertedPaths covers the new field, including the
// non-string and empty-array shapes the TS runtime guard tolerates.
func TestValidateContractAssertedPaths(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want []string
	}{
		{
			name: "populated",
			body: `{"command":"./t.sh","paths":["t.sh"],"asserted_paths":["hello.txt"]}`,
			want: []string{"hello.txt"},
		},
		{
			name: "non-string members dropped",
			body: `{"command":"./t.sh","asserted_paths":["a.txt",7,null,"b.txt"]}`,
			want: []string{"a.txt", "b.txt"},
		},
		{name: "empty array omits the field", body: `{"command":"./t.sh","asserted_paths":[]}`, want: nil},
		{name: "wrong type omits the field", body: `{"command":"./t.sh","asserted_paths":"hello.txt"}`, want: nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			var parsed any
			if err := json.Unmarshal([]byte(test.body), &parsed); err != nil {
				t.Fatal(err)
			}
			got := ValidateContract(parsed)
			if got == nil {
				t.Fatal("contract must validate")
			}
			if !reflect.DeepEqual(got.AssertedPaths, test.want) {
				t.Fatalf("AssertedPaths = %#v, want %#v", got.AssertedPaths, test.want)
			}
		})
	}
}

// TestContractRoundTripsAssertedPaths pins the JSON key, which is the wire
// contract with the coder agent and with a persisted checkpoint.
func TestContractRoundTripsAssertedPaths(t *testing.T) {
	encoded, err := json.Marshal(Contract{
		Command: "./t.sh", Paths: []string{"t.sh"}, AssertedPaths: []string{"hello.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"asserted_paths":["hello.txt"]`) {
		t.Fatalf("encoded = %s", encoded)
	}
	bare, err := json.Marshal(Contract{Command: "./t.sh"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bare), "asserted_paths") {
		t.Fatalf("empty asserted_paths must be omitted, got %s", bare)
	}
}
