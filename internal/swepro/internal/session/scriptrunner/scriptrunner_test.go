package scriptrunner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

func passthroughOffloader() OutputOffloader {
	return OutputOffloaderFunc(func(input OutputOffloadInput) OutputOffloadResult {
		return OutputOffloadResult{Inline: input.Output}
	})
}

func TestRunEmittedScriptInjectedShell(t *testing.T) {
	workspace := t.TempDir()
	timeout := 321.0
	env := jscompat.NewOrderedMap[string, string]()
	env.Set("A", "B")
	var scriptPath string
	var offloadInput OutputOffloadInput
	result, err := RunEmittedScript(RunEmittedScriptOptions{
		Language:    LanguageTypeScript,
		Source:      `console.log("hello")`,
		CWD:         workspace,
		TimeoutMS:   &timeout,
		Env:         env,
		RuntimePath: "/opt/bun",
		Exec: ScriptExecFunc(func(command []string, options ScriptExecOptions) (ScriptExecResult, error) {
			if len(command) != 3 || command[0] != "/opt/bun" || command[1] != "run" {
				t.Fatalf("command = %#v", command)
			}
			scriptPath = command[2]
			content, err := os.ReadFile(scriptPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != `console.log("hello")` {
				t.Fatalf("source = %q", content)
			}
			envA, _ := options.Env.Get("A")
			if options.CWD != workspace || options.TimeoutMS != timeout || envA != "B" {
				t.Fatalf("exec options = %#v", options)
			}
			return ScriptExecResult{Stdout: "out\n", Stderr: "err\n", ExitCode: 7}, nil
		}),
		Offloader: OutputOffloaderFunc(func(input OutputOffloadInput) OutputOffloadResult {
			offloadInput = input
			return OutputOffloadResult{Inline: input.Output}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 7 || result.Stdout != "out\n" || result.Stderr != "err\n" {
		t.Fatalf("result = %#v", result)
	}
	if offloadInput.Output != "out\n\nerr\n" ||
		offloadInput.Workspace != workspace ||
		offloadInput.ToolName != "emitted-script" ||
		offloadInput.CallID != "emitted-script" {
		t.Fatalf("offload input = %#v", offloadInput)
	}
	if _, err := os.Stat(scriptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary script remains: %v", err)
	}
}

func TestRunEmittedScriptRunsTypeScriptWithBun(t *testing.T) {
	result, err := RunEmittedScript(RunEmittedScriptOptions{
		Language:  LanguageTypeScript,
		Source:    `console.log(JSON.stringify({ok:true,n:3}))`,
		CWD:       t.TempDir(),
		Offloader: passthroughOffloader(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.TimedOut ||
		strings.TrimSpace(result.Stdout) != `{"ok":true,"n":3}` {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunEmittedScriptOffloadedShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "full.log")
	result, err := RunEmittedScript(RunEmittedScriptOptions{
		Language:    LanguageBash,
		Source:      "echo unused",
		CWD:         t.TempDir(),
		RuntimePath: "/unused",
		Exec: ScriptExecFunc(func([]string, ScriptExecOptions) (ScriptExecResult, error) {
			return ScriptExecResult{Stdout: "large", ExitCode: 0}, nil
		}),
		Offloader: OutputOffloaderFunc(func(OutputOffloadInput) OutputOffloadResult {
			return OutputOffloadResult{Inline: "extract", OffloadPath: path}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "extract" || result.Stderr != "extract" || result.Offloaded == nil {
		t.Fatalf("result = %#v", result)
	}
	if result.Offloaded.Path != path || !strings.Contains(result.Offloaded.Note, path) {
		t.Fatalf("offloaded = %#v", result.Offloaded)
	}
}

func TestRunEmittedScriptRunsBash(t *testing.T) {
	result, err := RunEmittedScript(RunEmittedScriptOptions{
		Language:  LanguageBash,
		Source:    "printf 'one\\ntwo\\nthree\\n'",
		CWD:       t.TempDir(),
		Offloader: passthroughOffloader(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.TimedOut || result.Stdout != "one\ntwo\nthree\n" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunEmittedScriptTimeout(t *testing.T) {
	timeout := 20.0
	result, err := RunEmittedScript(RunEmittedScriptOptions{
		Language:  LanguageBash,
		Source:    "while :; do :; done",
		CWD:       t.TempDir(),
		TimeoutMS: &timeout,
		Offloader: passthroughOffloader(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut {
		t.Fatalf("result = %#v", result)
	}
	if result.ExitCode != 143 {
		t.Fatalf("timeout exit code = %v, want Bun-compatible 143", result.ExitCode)
	}
}

func TestRunEmittedScriptRequiresOffloadBinding(t *testing.T) {
	_, err := RunEmittedScript(RunEmittedScriptOptions{})
	if err == nil || err.Error() != "script-runner: output offloader is required" {
		t.Fatalf("error = %v", err)
	}
}
