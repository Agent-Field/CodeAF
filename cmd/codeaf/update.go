package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

var (
	updateOut         io.Writer = os.Stdout
	updateErr         io.Writer = os.Stderr
	updateExecutable            = os.Executable
	updateClient                = codeupdate.NewClient
	updateVersionLine           = runInstalledVersion
	updateRevision              = buildinfo.Revision
)

func runUpdate(args []string) error {
	flags := commandFlags("update")
	check := flags.Bool("check", false, "check without installing")
	stable := flags.Bool("stable", false, "use the newest stable release")
	rc := flags.Bool("rc", false, "use the newest release candidate")
	dev := flags.Bool("dev", false, "use the newest dev build")
	staging := flags.Bool("staging", false, "use the newest staging build")
	version := flags.String("version", "", "install one exact release tag")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: codeaf update [--check] [--stable|--rc|--dev|--staging] [--version tag]")
	}
	chosen := 0
	for _, set := range []bool{*stable, *rc, *dev, *staging, strings.TrimSpace(*version) != ""} {
		if set {
			chosen++
		}
	}
	if chosen > 1 {
		return fmt.Errorf("choose one of --stable, --rc, --dev, --staging, or --version")
	}
	running := updateRevision()
	channel := "stable"
	if kind := codeupdate.Kind(running); kind == "dev" || kind == "staging" {
		channel = kind
	}
	switch {
	case *stable:
		channel = "stable"
	case *rc:
		channel = "rc"
	case *dev:
		channel = "dev"
	case *staging:
		channel = "staging"
	}
	choice := codeupdate.Choice{Channel: channel, Version: strings.TrimSpace(*version), Running: running}
	var target string
	curl := codeupdate.CurlCommand
	if !*check {
		executable, err := updateExecutable()
		if err != nil {
			return updateFailure(fmt.Errorf("find the running codeaf: %w", err), curl)
		}
		curl = codeupdate.CurlLine(executable, codeupdate.Kind(running))
		target, err = codeupdate.ExecutableTarget(func() (string, error) { return executable, nil })
		if err != nil {
			return updateFailure(err, curl)
		}
		if codeupdate.Kind(running) == "other" {
			shown := strings.TrimSpace(running)
			if shown == "" {
				shown = "no revision"
			}
			fmt.Fprintf(updateErr, "this codeaf was built from source (%s) at %s — rebuild with make build, or install a release: %s\n", shown, target, codeupdate.CurlCommand)
			return exitStatus(2)
		}
	}
	client := updateClient(running, codeupdate.CheckTimeout)
	var release codeupdate.Release
	var err error
	if *check {
		release, err = client.Check(context.Background(), choice)
	} else {
		release, err = client.Select(context.Background(), choice)
	}
	if err != nil {
		if *check {
			fmt.Fprintln(updateErr, "codeaf: could not check for an update:", err)
			return exitStatus(1)
		}
		return updateFailure(fmt.Errorf("could not select a release: %w", err), curl)
	}
	if *check {
		return sayUpdateCheck(running, release, choice)
	}
	if choice.Version == "" {
		available := codeupdate.Available{
			Latest: release.Tag, Running: running,
			LatestPublished: release.PublishedAt, RunningPublished: release.RunningPublishedAt,
		}
		if available.Ahead() {
			fmt.Fprintf(updateErr, "this codeaf is %s, ahead of the newest %s %s — pass --version %s to install it anyway\n", running, choice.Channel, release.Tag, release.Tag)
			return exitStatus(2)
		}
	}

	result, err := codeupdate.Install(context.Background(), codeupdate.InstallOptions{
		Client: client, Release: release, Target: target, Curl: curl,
	})
	if err != nil {
		return updateFailure(err, curl)
	}
	fmt.Fprintf(updateOut, "codeaf: installed %s at %s\n", result.Release.Tag, result.Path)
	if err := updateVersionLine(result.Path, updateOut, updateErr); err != nil {
		return updateFailure(fmt.Errorf("run the installed codeaf: %w", err), curl)
	}
	return nil
}

func sayUpdateCheck(running string, release codeupdate.Release, choice codeupdate.Choice) error {
	selected := release.Tag
	shown := strings.TrimSpace(running)
	if shown == "" {
		shown = "an unstamped source build"
	}
	if choice.Version != "" {
		if running == selected {
			fmt.Fprintf(updateOut, "this codeaf matches the selected tag, %s\n", selected)
			return nil
		}
		fmt.Fprintf(updateOut, "selected codeaf %s differs · this codeaf is %s\n", selected, shown)
		return exitStatus(3)
	}
	channel := choice.Channel
	if channel == "dev" || channel == "staging" {
		if running == selected {
			fmt.Fprintf(updateOut, "you are on the newest %s codeaf, %s\n", channel, selected)
			return nil
		}
		available := codeupdate.Available{
			Latest: selected, Running: running,
			LatestPublished: release.PublishedAt, RunningPublished: release.RunningPublishedAt,
		}
		if codeupdate.Kind(running) == channel && available.Ahead() {
			fmt.Fprintf(updateOut, "the newest %s codeaf is %s · this codeaf is %s\n", channel, selected, shown)
			return nil
		}
		fmt.Fprintf(updateOut, "codeaf %s is available · you have %s\n", selected, shown)
		return exitStatus(3)
	}
	comparison, comparable := codeupdate.CompareSemverTags(selected, running)
	if comparable && comparison > 0 {
		fmt.Fprintf(updateOut, "codeaf %s is available · you have %s\n", selected, shown)
		return exitStatus(3)
	}
	if comparable && comparison == 0 {
		if channel == "stable" {
			fmt.Fprintf(updateOut, "you are on the newest codeaf, %s\n", selected)
		} else {
			fmt.Fprintf(updateOut, "you are on the newest %s codeaf, %s\n", channel, selected)
		}
		return nil
	}
	fmt.Fprintf(updateOut, "the newest %s codeaf is %s · this codeaf is %s\n", channel, selected, shown)
	return nil
}

func updateFailure(err error, curl string) error {
	if strings.TrimSpace(curl) == "" {
		curl = codeupdate.CurlCommand
	}
	if strings.Contains(err.Error(), curl) {
		return err
	}
	return fmt.Errorf("%w; install a release with: %s", err, curl)
}

func runInstalledVersion(path string, stdout, stderr io.Writer) error {
	command := exec.Command(path, "version")
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}
