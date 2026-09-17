package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

func tagKind(tag string) string {
	return codeupdate.Kind(tag)
}

type nextOptions struct {
	channel   string
	component string
	sha       string
	date      string
}

func runNext(args []string, stdin io.Reader, stdout io.Writer) error {
	options, err := parseNextOptions(args)
	if err != nil {
		return err
	}
	if options.channel == "dev" || options.channel == "staging" {
		tag, err := codeupdate.ChannelTag(options.channel, options.date, options.sha)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, tag)
		return nil
	}

	tags, err := readTags(stdin)
	if err != nil {
		return err
	}
	tag, err := nextSemverTag(tags, options.channel, options.component)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, tag)
	return nil
}

func parseNextOptions(args []string) (nextOptions, error) {
	options := nextOptions{component: "patch"}
	for len(args) > 0 {
		if len(args) < 2 {
			return options, usageErr("%s needs a value", args[0])
		}
		value := args[1]
		switch args[0] {
		case "--channel":
			options.channel = value
		case "--component":
			options.component = value
		case "--sha":
			options.sha = value
		case "--date":
			options.date = value
		default:
			return options, usageErr("unknown next option %q", args[0])
		}
		args = args[2:]
	}

	switch options.channel {
	case "stable", "rc":
		if options.sha != "" || options.date != "" {
			return options, usageErr("--sha and --date are only for dev and staging")
		}
		if _, err := (codeupdate.Version{}).Bump(options.component); err != nil {
			return options, usageErr("%s", err)
		}
	case "dev", "staging":
		if !codeupdate.ValidSHA(options.sha) {
			return options, usageErr("--sha must be 12 to 40 hexadecimal characters")
		}
		if !codeupdate.ValidDate(options.date) {
			return options, usageErr("--date must be YYYYMMDD")
		}
		if options.component != "patch" {
			return options, usageErr("--component is only meaningful for rc and stable")
		}
	default:
		return options, usageErr("--channel must be stable, rc, dev, or staging")
	}
	return options, nil
}

func readTags(reader io.Reader) ([]string, error) {
	var tags []string
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		tags = append(tags, strings.TrimSpace(scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read tags: %w", err)
	}
	return tags, nil
}

func nextSemverTag(tags []string, channel, component string) (string, error) {
	answer, err := codeupdate.NextSemverTag(tags, channel, component)
	if err != nil && (strings.Contains(err.Error(), "component") || strings.Contains(err.Error(), "channel")) {
		return "", usageErr("--%s", err)
	}
	return answer, err
}

// requireUnused is a final collision guard kept separate from version choice.
// An answer derived from the maximum tag read here cannot collide with that
// same input. The guard stands for callers comparing an answer with a tag list
// assembled somewhere else, where the refusal must name the tag rather than
// relying on GitHub's later API error.
func requireUnused(answer string, tags []string) error {
	for _, tag := range tags {
		if tag == answer {
			return fmt.Errorf("release tag %s already exists", answer)
		}
	}
	return nil
}
