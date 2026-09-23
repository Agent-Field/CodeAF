package delegate

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"strings"
	"testing"
)

// testProgram is a program with two commands, the default one taking a flag
// of its own, whose body reports what it was handed through the host.
func testProgram(body Body) Delegate {
	if body == nil {
		body = func(context.Context, Host, []string) error { return nil }
	}
	return Delegate{
		Name: "fake", Summary: "a fake program for the tests", Default: "run", Page: "fake",
		Guide: "For the tests' fake work, with a brief that names what it touches.",
		Commands: []Command{{
			Name: "run", Usage: "[flags] -- <brief>", Summary: "does the whole task",
			Bind: func(fs *flag.FlagSet) Body {
				variant := fs.String("variant", "", "how hard the model thinks")
				return func(ctx context.Context, host Host, args []string) error {
					if *variant != "" {
						host.Stage("variant", *variant)
					}
					return body(ctx, host, args)
				}
			},
		}, {
			Name: "check", Usage: "", Summary: "says whether it could run",
			Bind: func(fs *flag.FlagSet) Body { return body },
		}},
	}
}

func TestParseRunsTheDefaultCommandOnABareBrief(t *testing.T) {
	inv, err := Parse(testProgram(nil), []string{"--max-cost", "5", "fix", "the", "flaky", "test"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Command.Name != "run" || inv.Brief() != "fix the flaky test" || inv.Ceilings.CostUSD != 5 {
		t.Fatalf("invocation = %+v", inv)
	}
	if inv.Workspace == "" || inv.Workspace[0] != '/' {
		t.Fatalf("workspace = %q, want the current folder, absolute", inv.Workspace)
	}
}

func TestParseTakesANamedCommandAndItsOwnFlags(t *testing.T) {
	inv, err := Parse(testProgram(nil), []string{"run", "--variant", "high", "--dir", "/tmp", "--", "--not-a-flag"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Command.Name != "run" || inv.Workspace != "/tmp" || inv.Brief() != "--not-a-flag" {
		t.Fatalf("invocation = %+v", inv)
	}
	if other, err := Parse(testProgram(nil), []string{"check"}, &bytes.Buffer{}); err != nil || other.Command.Name != "check" {
		t.Fatalf("check = %+v %v", other, err)
	}
}

// The line a host starts its child with is the line Parse reads back.
func TestChildArgsParseBackToTheSameInvocation(t *testing.T) {
	program := testProgram(nil)
	line := ChildArgs(program, "/work", "add a --flag to the parser", Ceilings{CostUSD: 2.5, Hours: 1})
	if line[0] != "fake" {
		t.Fatalf("line = %q, want the program's name first", line)
	}
	inv, err := Parse(program, line[1:], &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Command.Name != "run" || !inv.JSON || inv.Workspace != "/work" || inv.Brief() != "add a --flag to the parser" ||
		inv.Ceilings != (Ceilings{CostUSD: 2.5, Hours: 1}) {
		t.Fatalf("invocation = %+v", inv)
	}
}

func TestParseWritesHelpAndSaysSo(t *testing.T) {
	for _, line := range [][]string{{"--help"}, {"help"}, {"run", "-h"}} {
		var out bytes.Buffer
		if _, err := Parse(testProgram(nil), line, &out); !errors.Is(err, ErrHelp) {
			t.Fatalf("%q: err = %v, want ErrHelp", line, err)
		}
		if !strings.Contains(out.String(), "codeaf fake") {
			t.Fatalf("%q: help = %q", line, out.String())
		}
	}
	var out bytes.Buffer
	_, _ = Parse(testProgram(nil), []string{"run", "--help"}, &out)
	if !strings.Contains(out.String(), "--variant") {
		t.Fatalf("a command's help lacks its own flag:\n%s", out.String())
	}
	// A bare ask is the program's own page, with every command on it.
	out.Reset()
	_, _ = Parse(testProgram(nil), []string{"--help"}, &out)
	if !strings.Contains(out.String(), "flags every command takes") || !strings.Contains(out.String(), "codeaf fake check") {
		t.Fatalf("a bare --help is not the program's own page:\n%s", out.String())
	}
}

// EXACTLY ONE TERMINAL, ON EVERY PATH: a body that ends without one gets one,
// and a body's own is the only one written.
func TestRunChildWritesExactlyOneTerminal(t *testing.T) {
	t.Setenv(EnvModelAPI, "http://127.0.0.1:9/v1")
	t.Setenv(EnvModelToken, "token")
	cases := []struct {
		name   string
		body   Body
		status string
	}{
		{"its own", func(ctx context.Context, host Host, args []string) error {
			host.Hello([]string{"work"})
			host.Terminal(Ending{Status: StatusPass, Message: "done", Claim: "it works"})
			host.Terminal(Ending{Status: StatusFail, Message: "a second"})
			return nil
		}, StatusPass},
		{"an error", func(ctx context.Context, host Host, args []string) error {
			return errors.New("the engine broke\nwith a trace")
		}, StatusCrashed},
		{"nothing said", func(ctx context.Context, host Host, args []string) error { return nil }, StatusFail},
	}
	for _, tc := range cases {
		inv, err := Parse(testProgram(tc.body), []string{"b"}, &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		var stdout bytes.Buffer
		status := RunChild(context.Background(), inv, &stdout)
		reading, err := Read(&stdout, nil)
		if err != nil {
			t.Fatal(err)
		}
		if status != tc.status || reading.Terminal == nil || reading.Terminal.Status != tc.status {
			t.Fatalf("%s: status %q terminal %+v, want %q", tc.name, status, reading.Terminal, tc.status)
		}
		if strings.Count(stdout.String(), `"terminal"`) > 1 {
			t.Fatalf("%s: more than one terminal:\n%s", tc.name, stdout.String())
		}
	}
}

func TestRunChildRefusesToRunWithoutAModelAPI(t *testing.T) {
	t.Setenv(EnvModelAPI, "")
	ran := false
	inv, err := Parse(testProgram(func(ctx context.Context, host Host, args []string) error { ran = true; return nil }), []string{"b"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if status := RunChild(context.Background(), inv, &stdout); status != StatusCrashed || ran {
		t.Fatalf("status %q ran %v, want crashed before the body", status, ran)
	}
}

func TestValidateRefusesADefinitionThatCouldNotRun(t *testing.T) {
	good := testProgram(func(context.Context, Host, []string) error { return nil })
	if err := good.Validate(); err != nil {
		t.Fatalf("a good definition refused: %v", err)
	}
	bad := good
	bad.Default = "missing"
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "default command") {
		t.Fatalf("err = %v", err)
	}
	shadow := good
	shadow.Commands = []Command{{Name: "run", Bind: func(fs *flag.FlagSet) Body {
		fs.String("dir", "", "")
		return func(context.Context, Host, []string) error { return nil }
	}}}
	if err := shadow.Validate(); err == nil || !strings.Contains(err.Error(), "--dir") {
		t.Fatalf("err = %v, want the shared flag named", err)
	}
}

// A PROGRAM DESCRIBES ITSELF TO THE MODEL THAT HANDS IT WORK, in one paragraph
// the conversation's fixed prefix can afford: a program with no guide would be
// listed by its name alone, one with line breaks would break the list it is an
// item of, and one past GuideMax would be paid for on every request of every
// turn of every conversation that carries it.
func TestValidateHoldsTheGuideToOneAffordableParagraph(t *testing.T) {
	good := testProgram(nil)
	for _, c := range []struct {
		name, guide, want string
	}{
		{"empty", "  ", "the guide is empty"},
		{"two paragraphs", "For one thing.\n\nAnd another.", "no line breaks"},
		{"too long", strings.Repeat("x", GuideMax+1), "held to"},
	} {
		program := good
		program.Guide = c.guide
		if err := program.Validate(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: err = %v, want it to say %q", c.name, err, c.want)
		}
	}
	program := good
	program.Guide = strings.Repeat("x", GuideMax)
	if err := program.Validate(); err != nil {
		t.Fatalf("a guide of exactly GuideMax bytes refused: %v", err)
	}
}
