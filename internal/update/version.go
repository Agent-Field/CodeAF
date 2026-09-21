// Package update finds, installs, and restarts onto codeaf releases.
package update

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	stableTagPattern  = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	rcTagPattern      = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-rc\.([1-9][0-9]*)$`)
	devTagPattern     = regexp.MustCompile(`^dev-[0-9]{8}-[0-9a-f]{12}$`)
	stagingTagPattern = regexp.MustCompile(`^staging-[0-9]{8}-[0-9a-f]{12}$`)
	shaPattern        = regexp.MustCompile(`^[0-9a-fA-F]{12,40}$`)
	datePattern       = regexp.MustCompile(`^[0-9]{8}$`)
)

// Version is the stable semantic part of a release tag.
type Version struct {
	Major int
	Minor int
	Patch int
}

func (v Version) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare orders stable semantic versions.
func (v Version) Compare(other Version) int {
	switch {
	case v.Major != other.Major:
		return compareInt(v.Major, other.Major)
	case v.Minor != other.Minor:
		return compareInt(v.Minor, other.Minor)
	default:
		return compareInt(v.Patch, other.Patch)
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

// Bump advances one semantic component.
func (v Version) Bump(component string) (Version, error) {
	switch component {
	case "patch":
		v.Patch++
	case "minor":
		v.Minor++
		v.Patch = 0
	case "major":
		v.Major++
		v.Minor = 0
		v.Patch = 0
	default:
		return Version{}, fmt.Errorf("component %q must be patch, minor, or major", component)
	}
	return v, nil
}

// RCTag is a release candidate and the stable line it belongs to.
type RCTag struct {
	Base    Version
	Counter int
}

// CompareSemverTags orders stable and release-candidate tags. A stable tag
// follows every release candidate on its own version line.
func CompareSemverTags(left, right string) (int, bool) {
	leftVersion, leftStable := ParseStable(left)
	leftRC, leftCandidate := ParseRC(left)
	rightVersion, rightStable := ParseStable(right)
	rightRC, rightCandidate := ParseRC(right)
	if (!leftStable && !leftCandidate) || (!rightStable && !rightCandidate) {
		return 0, false
	}
	if leftCandidate {
		leftVersion = leftRC.Base
	}
	if rightCandidate {
		rightVersion = rightRC.Base
	}
	if comparison := leftVersion.Compare(rightVersion); comparison != 0 {
		return comparison, true
	}
	switch {
	case leftStable && rightCandidate:
		return 1, true
	case leftCandidate && rightStable:
		return -1, true
	case leftCandidate && rightCandidate:
		return compareInt(leftRC.Counter, rightRC.Counter), true
	default:
		return 0, true
	}
}

func parseComponent(raw string) (int, bool) {
	value, err := strconv.Atoi(raw)
	// Every accepted component must leave room for the resolver's next bump.
	// A number this process cannot represent or increment is a junk tag, just
	// like a tag that does not match the grammar at all.
	if err != nil || value == int(^uint(0)>>1) {
		return 0, false
	}
	return value, true
}

func parseVersion(match []string) (Version, bool) {
	major, majorOK := parseComponent(match[1])
	minor, minorOK := parseComponent(match[2])
	patch, patchOK := parseComponent(match[3])
	if !majorOK || !minorOK || !patchOK {
		return Version{}, false
	}
	return Version{Major: major, Minor: minor, Patch: patch}, true
}

// ParseStable parses a stable release tag.
func ParseStable(tag string) (Version, bool) {
	match := stableTagPattern.FindStringSubmatch(tag)
	if match == nil {
		return Version{}, false
	}
	return parseVersion(match)
}

// ParseRC parses a release-candidate tag.
func ParseRC(tag string) (RCTag, bool) {
	match := rcTagPattern.FindStringSubmatch(tag)
	if match == nil {
		return RCTag{}, false
	}
	base, baseOK := parseVersion(match)
	counter, counterOK := parseComponent(match[4])
	if !baseOK || !counterOK {
		return RCTag{}, false
	}
	return RCTag{Base: base, Counter: counter}, true
}

// Kind names the release channel encoded by a tag.
func Kind(tag string) string {
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

// FollowedChannel names the release channel a build takes its updates from.
// IT IS NOT ALWAYS THE CHANNEL THE TAG WAS CUT ON: a release candidate follows
// STABLE, because its launch line, its bare /update and its bare `codeaf
// update` all speak about the stable release its line is heading for. A build
// from source follows stable too, so the road it is offered is the ordinary
// one. Only dev and staging follow themselves. Every place that has to answer
// "which channel is this build on" asks here, so the launch notice and the
// curl line under it can never name two different roads.
func FollowedChannel(running string) string {
	switch Kind(running) {
	case "dev", "staging":
		return Kind(running)
	default:
		return "stable"
	}
}

// ParseChannel returns the channel and calendar date carried by a dev or
// staging tag. The date is kept as YYYYMMDD because that spelling sorts in the
// same order as the days it names.
func ParseChannel(tag string) (channel, date string, ok bool) {
	channel = Kind(tag)
	if channel != "dev" && channel != "staging" {
		return "", "", false
	}
	parts := strings.Split(tag, "-")
	if len(parts) != 3 {
		return "", "", false
	}
	return channel, parts[1], true
}

// CompareChannelBuilds compares a running build with the release selected by
// the API from the same channel. A negative result means the selected release
// is newer; a positive result means the running build is ahead.
func CompareChannelBuilds(running string, runningPublished time.Time, selected string, selectedPublished time.Time) (int, bool) {
	if running == selected {
		return 0, true
	}
	runningChannel, runningDate, runningOK := ParseChannel(running)
	selectedChannel, selectedDate, selectedOK := ParseChannel(selected)
	if !runningOK || !selectedOK || runningChannel != selectedChannel {
		return 0, false
	}
	if !runningPublished.IsZero() && !selectedPublished.IsZero() {
		switch {
		case runningPublished.Before(selectedPublished):
			return -1, true
		case runningPublished.After(selectedPublished):
			return 1, true
		}
	}
	if runningDate < selectedDate {
		return -1, true
	}
	if runningDate > selectedDate {
		return 1, true
	}
	// THE API-NAMED RELEASE WINS A SAME-DAY TIE WHEN EITHER MOMENT IS
	// UNKNOWN. It is the only remaining ordering fact the check has.
	return -1, true
}

// ValidSHA reports whether a revision can form a channel tag.
func ValidSHA(value string) bool { return shaPattern.MatchString(value) }

// ValidDate reports whether a date has the channel tag's YYYYMMDD shape.
func ValidDate(value string) bool { return datePattern.MatchString(value) }

// ChannelTag forms a dev or staging tag from a date and revision.
func ChannelTag(channel, date, sha string) (string, error) {
	if channel != "dev" && channel != "staging" {
		return "", fmt.Errorf("channel must be dev or staging")
	}
	if !ValidDate(date) {
		return "", fmt.Errorf("date must be YYYYMMDD")
	}
	if !ValidSHA(sha) {
		return "", fmt.Errorf("sha must be 12 to 40 hexadecimal characters")
	}
	return fmt.Sprintf("%s-%s-%s", channel, date, strings.ToLower(sha[:12])), nil
}

// NextSemverTag returns the next stable or release-candidate tag.
func NextSemverTag(tags []string, channel, component string) (string, error) {
	stableLatest := Version{}
	for _, tag := range tags {
		if candidate, ok := ParseStable(tag); ok && candidate.Compare(stableLatest) > 0 {
			stableLatest = candidate
		}
	}

	var (
		openLine   Version
		haveOpen   bool
		maxCounter int
	)
	for _, tag := range tags {
		candidate, ok := ParseRC(tag)
		if !ok || candidate.Base.Compare(stableLatest) <= 0 {
			continue
		}
		switch comparison := candidate.Base.Compare(openLine); {
		case !haveOpen || comparison > 0:
			openLine, haveOpen, maxCounter = candidate.Base, true, candidate.Counter
		case comparison == 0 && candidate.Counter > maxCounter:
			maxCounter = candidate.Counter
		}
	}

	var answer string
	switch channel {
	case "rc":
		if haveOpen {
			answer = fmt.Sprintf("%s-rc.%d", openLine, maxCounter+1)
		} else {
			next, err := stableLatest.Bump(component)
			if err != nil {
				return "", err
			}
			answer = next.String() + "-rc.1"
		}
	case "stable":
		if component == "patch" && haveOpen {
			answer = openLine.String()
		} else {
			next, err := stableLatest.Bump(component)
			if err != nil {
				return "", err
			}
			answer = next.String()
		}
	default:
		return "", fmt.Errorf("channel must be stable or rc")
	}
	for _, tag := range tags {
		if tag == answer {
			return "", fmt.Errorf("release tag %s already exists", answer)
		}
	}
	return answer, nil
}
