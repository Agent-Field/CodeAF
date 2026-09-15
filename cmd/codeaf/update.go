package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

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
	channel := "stable"
	switch {
	case *rc:
		channel = "rc"
	case *dev:
		channel = "dev"
	case *staging:
		channel = "staging"
	}
	choice := codeupdate.Choice{Channel: channel, Version: strings.TrimSpace(*version)}
	running := updateRevision()
	var target string
	if !*check {
		var err error
		target, err = codeupdate.ExecutableTarget(updateExecutable)
		if err != nil {
			return err
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
	client := updateClient(running, 3*time.Second)
	release, err := client.Select(context.Background(), choice)
	if err != nil {
		fmt.Fprintln(updateErr, "codeaf: could not check for an update:", err)
		return exitStatus(1)
	}
	if *check {
		return sayUpdateCheck(running, release.Tag)
	}

	result, err := codeupdate.Install(context.Background(), codeupdate.InstallOptions{
		Client: client, Release: release, Target: target,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(updateOut, "codeaf: installed %s at %s\n", result.Release.Tag, result.Path)
	if err := updateVersionLine(result.Path, updateOut, updateErr); err != nil {
		return fmt.Errorf("run the installed codeaf: %w", err)
	}
	return nil
}

func sayUpdateCheck(running, latest string) error {
	answer := codeupdate.Available{Latest: latest, Running: running}
	switch {
	case answer.Newer():
		fmt.Fprintf(updateOut, "codeaf %s is available · you have %s\n", latest, running)
		return exitStatus(3)
	case codeupdate.Kind(running) == "stable" || codeupdate.Kind(running) == "rc":
		fmt.Fprintf(updateOut, "you are on the newest codeaf, %s\n", running)
		return nil
	default:
		shown := strings.TrimSpace(running)
		if shown == "" {
			shown = "an unstamped source build"
		}
		fmt.Fprintf(updateOut, "the newest stable codeaf is %s · this codeaf is %s\n", latest, shown)
		return nil
	}
}

func runInstalledVersion(path string, stdout, stderr io.Writer) error {
	command := exec.Command(path, "version")
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}
