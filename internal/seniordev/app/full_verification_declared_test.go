//go:build !windows

package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/session/fullverification"
)

// These tests describe the project's own check named on the command line
// (`--verify-build`, `--verify-test`): the shape of a fuzz target or a
// benchmark rig, whose command lives beside the checkout rather than in it.

func declaredVerificationRunner(t *testing.T, args cliArgs, files map[string]string) *pipeline {
	t.Helper()
	workspace := t.TempDir()
	for name, content := range files {
		if err := writeFile(filepath.Join(workspace, name), content); err != nil {
			t.Fatal(err)
		}
	}
	runner := newPipeline(args, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	t.Cleanup(runner.runtime.Close)
	return runner
}

// A DECLARED COMMAND IS RUN BY SENIOR-DEV ITSELF, IN PLACE OF DISCOVERY. The
// command here lives outside the workspace, as a harness's would, and leaves a
// mark to prove it ran; the Makefile's own red test is never run, because the
// person said what this project's test is.
func TestADeclaredTestRunsInPlaceOfTheDiscoveredOne(t *testing.T) {
	harness := t.TempDir()
	mark := filepath.Join(harness, "ran")
	script := filepath.Join(harness, "validate.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+mark+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := declaredVerificationRunner(t, cliArgs{VerifyTest: script}, map[string]string{
		"Makefile": "build:\n\t@true\ntest:\n\texit 1\n",
	})
	verification := runner.runProjectVerification(context.Background())
	if verification.Failed != nil {
		t.Fatalf("a green declared test failed verification: %#v (%s)", verification.Failed, verification.Failure)
	}
	if _, err := os.Stat(mark); err != nil {
		t.Fatal("the declared command never ran")
	}
	if !soloVerificationPassed(&verification) {
		t.Fatalf("a green build and a green declared test did not pass: %#v", verification)
	}
	sources := []string{}
	for _, command := range verification.Commands {
		evidence, _ := command.(map[string]any)
		sources = append(sources, evidence["source"].(string))
		if evidence["cmd"] == "make test" {
			t.Fatal("the discovered test ran although a test was declared")
		}
	}
	if strings.Join(sources, ",") != "Makefile#build,"+fullverification.DeclaredTestSource {
		t.Fatalf("sources = %v, want the discovered build and the declared test", sources)
	}
	if !strings.Contains(verification.Prompt, "named by the person who started this run") {
		t.Fatalf("the evidence the model reads does not say where the check came from:\n%s", verification.Prompt)
	}
}

// A declared command is judged exactly as a discovered one: red is a failure,
// and it is the command's failure, never a missing-entrypoint one.
func TestARedDeclaredCommandFailsVerification(t *testing.T) {
	runner := declaredVerificationRunner(t, cliArgs{VerifyBuild: "true", VerifyTest: "exit 3"},
		map[string]string{"NOTES.txt": "a harness-shaped project\n"})
	verification := runner.runProjectVerification(context.Background())
	if verification.Failed == nil {
		t.Fatal("a declared test exiting 3 passed verification")
	}
	if verification.Failed.Source != fullverification.DeclaredTestSource || missingEntrypointFailure(verification) {
		t.Fatalf("Failed = %#v, want the declared test's own failure", verification.Failed)
	}
}

// The strict preamble holds for a declared command too, so the person's
// command cannot be passed by a masked pipeline any more than a discovered one.
func TestADeclaredCommandRunsUnderTheStrictPreamble(t *testing.T) {
	runner := declaredVerificationRunner(t, cliArgs{VerifyBuild: "true", VerifyTest: "false | cat"},
		map[string]string{"NOTES.txt": "a harness-shaped project\n"})
	if verification := runner.runProjectVerification(context.Background()); verification.Failed == nil {
		t.Fatal("a pipeline whose first command fails passed verification")
	}
}

// THE 5C2 SHAPE. A header-only CMake library used to fail for want of a
// command; it now has CMake's own, and nothing in it is a missing entrypoint.
// The commands are not run here — this machine may have no cmake — only
// planned, which is where the old failure was decided.
func TestAHeaderOnlyCMakeLibraryIsNoLongerMissingAnEntrypoint(t *testing.T) {
	runner := declaredVerificationRunner(t, cliArgs{}, map[string]string{
		"CMakeLists.txt":              "cmake_minimum_required(VERSION 3.20)\nproject(lib CXX)\ninclude(CTest)\n",
		"src/lib.hpp":                 "#pragma once\n",
		"extras/tests/CMakeLists.txt": "add_test(NAME smoke COMMAND true)\n",
	})
	plan := newProjectVerificationRun(runner, context.Background()).plan
	if !planHasKind(plan, fullverification.KindBuild) || !planHasKind(plan, fullverification.KindTest) {
		t.Fatalf("plan = %#v, want CMake's build and test", plan)
	}
	for _, entrypoint := range plan.Entrypoints {
		// The build tree is the run's own folder, which no recorder reads, so
		// configuring the project never puts a build directory in the answer.
		if !strings.Contains(entrypoint.Command, seniorDevDataDirectory+"/") {
			t.Fatalf("%q builds outside %s/", entrypoint.Command, seniorDevDataDirectory)
		}
	}
}
