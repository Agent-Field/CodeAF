package util

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestUtilProcessHelper(t *testing.T) {
	if os.Getenv("GO_UTIL_HELPER") != "1" {
		return
	}
	separator := 0
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i + 1
			break
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(os.Args[separator:])
	_, _ = os.Stderr.WriteString("warning\n")
	if os.Getenv("GO_UTIL_FAIL") == "1" {
		os.Exit(7)
	}
	os.Exit(0)
}

func TestRunTextLinesAndFailure(t *testing.T) {
	command := []string{os.Args[0], "-test.run=TestUtilProcessHelper", "--", "a b", "", "c"}
	options := RunOptions{ProcessOptions: ProcessOptions{Env: map[string]string{"GO_UTIL_HELPER": "1"}}}
	result, err := TextProcess(context.Background(), command, options)
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	if err := json.Unmarshal([]byte(result.Text), &args); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, []string{"a b", "", "c"}) || string(result.Stderr) != "warning\n" {
		t.Fatalf("result: args=%v stderr=%q", args, result.Stderr)
	}

	options.Env["GO_UTIL_FAIL"] = "1"
	_, err = RunProcess(context.Background(), command, options)
	failed, ok := err.(*RunFailedError)
	if !ok || failed.Code != 7 ||
		failed.Error() != "Command failed with code 7: "+strings.Join(command, " ")+"\nwarning" {
		t.Fatalf("failure: %#v %v", failed, err)
	}
	options.NoThrow = true
	nothrow, err := RunProcess(context.Background(), command, options)
	if err != nil || nothrow.Code != 7 {
		t.Fatalf("nothrow: %+v %v", nothrow, err)
	}
}

func TestProcessLinesFiltersOnlyEmptyLines(t *testing.T) {
	command := []string{"printf", "a\\n\\nb\\r\\n"}
	lines, err := ProcessLines(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lines, []string{"a", "b"}) {
		t.Fatalf("lines: %#v", lines)
	}
}
