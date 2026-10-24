package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/agentsession"
	"github.com/Agent-Field/codeaf/internal/delegate"
)

// fakeFolder is the name of the fake that works in its folder.
const fakeFolder = "fake-folder"

// fakeFolderProgram is a program that edits files the way senior-dev does:
// it says whether it was told it works without git, writes one file of work
// and one of its own notes into the folder it was handed, and passes.
func fakeFolderProgram() delegate.Delegate {
	return delegate.Delegate{
		Name: fakeFolder, Summary: "a program the tests carry, which writes a file where it is told to work", Default: "run", Page: "delegates",
		Guide:       "For the tests' folder work, with a brief that names the file.",
		PlainFolder: []string{"--in-place"},
		Notes:       ".fake-folder",
		Commands: []delegate.Command{{
			Name: "run", Usage: "[flags] -- <brief>", Summary: "does the whole task",
			Bind: func(fs *flag.FlagSet) delegate.Body {
				inPlace := fs.Bool("in-place", false, "work without git")
				return func(ctx context.Context, host delegate.Host, args []string) error {
					host.Hello([]string{"implement"})
					mode := "git"
					if *inPlace {
						mode = "in place"
					}
					host.Step(delegate.StepRecord{Command: "folder", Observation: mode})
					if err := os.WriteFile(filepath.Join(host.Workspace(), "made.txt"), []byte("made\n"), 0o644); err != nil {
						return err
					}
					if err := os.MkdirAll(filepath.Join(host.Workspace(), ".fake-folder"), 0o755); err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join(host.Workspace(), ".fake-folder", "spec.md"), []byte(strings.Join(args, " ")+"\n"), 0o644); err != nil {
						return err
					}
					host.Terminal(delegate.Ending{Status: delegate.StatusPass, Message: "made it"})
					return nil
				}
			},
		}},
	}
}

// A real child asks both declared model flags, so resolving only the main
// flag cannot hide behind a passing parent parser or a tolerant provider.
func fakeModelFlagsProgram() delegate.Delegate {
	return delegate.Delegate{Name: "fake-model-flags", Summary: "tests model resolution", Guide: "Tests the shell's model flags.", Page: "delegates", Default: "run", ModelFlag: "model", AuxiliaryModelFlags: []string{"light"}, Commands: []delegate.Command{{Name: "run", Usage: "[flags] -- <brief>", Summary: "asks both models", Bind: func(fs *flag.FlagSet) delegate.Body {
		model := fs.String("model", "", "the working model")
		light := fs.String("light", "", "the light model")
		return func(ctx context.Context, host delegate.Host, _ []string) error {
			client, err := agentsession.NewClient(host.Models(), 0)
			if err != nil {
				return err
			}
			for _, selected := range []string{*model, *light} {
				if selected == "" {
					selected = *model
				}
				reply, err := client.Complete(ctx, agentsession.Request{Model: selected, Messages: []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "answer"}}}}})
				if err != nil {
					host.Terminal(delegate.Ending{Status: delegate.StatusFail, Message: err.Error()})
					return nil
				}
				if reply.Text() != "done" {
					return fmt.Errorf("unexpected answer")
				}
			}
			host.Terminal(delegate.Ending{Status: delegate.StatusPass, Message: "both models answered"})
			return nil
		}
	}}}}
}
