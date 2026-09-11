package main

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

var (
	stableTagPattern  = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	rcTagPattern      = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-rc\.([1-9][0-9]*)$`)
	devTagPattern     = regexp.MustCompile(`^dev-[0-9]{8}-[0-9a-f]{12}$`)
	stagingTagPattern = regexp.MustCompile(`^staging-[0-9]{8}-[0-9a-f]{12}$`)
	shaPattern        = regexp.MustCompile(`^[0-9a-fA-F]{12,40}$`)
	datePattern       = regexp.MustCompile(`^[0-9]{8}$`)
)

type version struct {
	major int
	minor int
	patch int
}

func (v version) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.major, v.minor, v.patch)
}

func (v version) compare(other version) int {
	switch {
	case v.major != other.major:
		return compareInt(v.major, other.major)
	case v.minor != other.minor:
		return compareInt(v.minor, other.minor)
	default:
		return compareInt(v.patch, other.patch)
	}
}

func compareInt(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func (v version) bump(component string) (version, error) {
	switch component {
	case "patch":
		v.patch++
	case "minor":
		v.minor++
		v.patch = 0
	case "major":
		v.major++
		v.minor = 0
		v.patch = 0
	default:
		return version{}, usageErr("component %q must be patch, minor, or major", component)
	}
	return v, nil
}

type rcTag struct {
	base    version
	counter int
}

func parseComponent(raw string) (int, bool) {
	value, err := strconv.Atoi(raw)
	// Every accepted component must leave room for the resolver's next bump.
	// A number that this process cannot represent or increment is a junk tag,
	// just like a tag that does not match the grammar at all.
	if err != nil || value == int(^uint(0)>>1) {
		return 0, false
	}
	return value, true
}

func parseVersion(match []string) (version, bool) {
	major, majorOK := parseComponent(match[1])
	minor, minorOK := parseComponent(match[2])
	patch, patchOK := parseComponent(match[3])
	if !majorOK || !minorOK || !patchOK {
		return version{}, false
	}
	return version{major: major, minor: minor, patch: patch}, true
}

func parseStable(tag string) (version, bool) {
	match := stableTagPattern.FindStringSubmatch(tag)
	if match == nil {
		return version{}, false
	}
	return parseVersion(match)
}

func parseRC(tag string) (rcTag, bool) {
	match := rcTagPattern.FindStringSubmatch(tag)
	if match == nil {
		return rcTag{}, false
	}
	base, baseOK := parseVersion(match)
	counter, counterOK := parseComponent(match[4])
	if !baseOK || !counterOK {
		return rcTag{}, false
	}
	return rcTag{base: base, counter: counter}, true
}

func tagKind(tag string) string {
	switch {
	case stableTagPattern.MatchString(tag):
		return "stable"
	case rcTagPattern.MatchString(tag):
		return "rc"
	case devTagPattern.MatchString(tag):
		return "dev"
	case stagingTagPattern.MatchString(tag):
		return "staging"
	default:
		return "other"
	}
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
		tag := fmt.Sprintf("%s-%s-%s", options.channel, options.date, strings.ToLower(options.sha[:12]))
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
		if _, err := (version{}).bump(options.component); err != nil {
			return options, err
		}
	case "dev", "staging":
		if !shaPattern.MatchString(options.sha) {
			return options, usageErr("--sha must be 12 to 40 hexadecimal characters")
		}
		if !datePattern.MatchString(options.date) {
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
	stableLatest := version{}
	for _, tag := range tags {
		if candidate, ok := parseStable(tag); ok && candidate.compare(stableLatest) > 0 {
			stableLatest = candidate
		}
	}

	var (
		openLine   version
		haveOpen   bool
		maxCounter int
	)
	for _, tag := range tags {
		candidate, ok := parseRC(tag)
		if !ok || candidate.base.compare(stableLatest) <= 0 {
			continue
		}
		switch comparison := candidate.base.compare(openLine); {
		case !haveOpen || comparison > 0:
			openLine, haveOpen, maxCounter = candidate.base, true, candidate.counter
		case comparison == 0 && candidate.counter > maxCounter:
			maxCounter = candidate.counter
		}
	}

	var answer string
	switch channel {
	case "rc":
		if haveOpen {
			answer = fmt.Sprintf("%s-rc.%d", openLine, maxCounter+1)
		} else {
			next, err := stableLatest.bump(component)
			if err != nil {
				return "", err
			}
			answer = next.String() + "-rc.1"
		}
	case "stable":
		if component == "patch" && haveOpen {
			answer = openLine.String()
		} else {
			next, err := stableLatest.bump(component)
			if err != nil {
				return "", err
			}
			answer = next.String()
		}
	default:
		return "", usageErr("--channel must be stable, rc, dev, or staging")
	}

	return answer, requireUnused(answer, tags)
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
