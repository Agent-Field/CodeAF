package inbox

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// The limits on one entry. THEY ARE THE ONE SPELLING every door quotes: the
// wire, the command line and the HTTP endpoint all refuse through [Validate],
// and the manual page states these same figures.
const (
	MaxTitle      = 200
	MaxBody       = 32000
	MaxExternalID = 200
	MaxURL        = 2048
	MaxAuthor     = 200
	MaxLabels     = 30
	MaxLabel      = 100
	// MaxRequest is the most one entry's details may be, in bytes.
	MaxRequest = 64 << 10
)

// sourceName is a source as Entry.Source spells it: a lowercase word, and for
// a trigger a second lowercase word after one colon (`inbox:ops`).
var sourceName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}(:[a-z0-9][a-z0-9._/-]{0,63})?$`)

// repoName is owner/name as GitHub spells one.
var repoName = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]{1,100}$`)

// reservedSources are the floor's own origins. AN ENTRY MAY NOT CLAIM ONE: a
// job whose Origin is `github` would be taken for an issue the forge reported
// and be commented on, closed and rechecked there.
var reservedSources = map[string]bool{
	string(factory.OriginForge):    true,
	string(factory.OriginTerminal): true,
	string(factory.OriginChat):     true,
}

// priorityWords maps every priority an entry may carry to the word the floor
// keeps ([factory.ValidPriority]). p0..p3 is the sender's scale; the floor's
// own words are taken as well.
var priorityWords = map[string]string{
	"p0": "urgent", "p1": "high", "p2": "medium", "p3": "low",
	"urgent": "urgent", "high": "high", "medium": "medium", "low": "low",
}

// Validate is the one check every door makes on an entry before it is filed.
// It answers the entry trimmed and normalised (a priority in p0..p3, labels
// without blanks or repeats) or a refusal in one sentence a sender can act on.
func Validate(e Entry) (Entry, error) {
	e.Source = strings.TrimSpace(e.Source)
	switch {
	case e.Source == "":
		return e, errors.New("source is required: name the system the work comes from, such as inbox:ops")
	case reservedSources[e.Source]:
		return e, fmt.Errorf("source %q is the factory's own; name the outside system instead", e.Source)
	case !sourceName.MatchString(e.Source):
		return e, errors.New("source must be a lowercase name, such as jira or inbox:ops")
	}
	var err error
	if e.ExternalID, err = line("id", e.ExternalID, MaxExternalID, true); err != nil {
		return e, err
	}
	if e.Title, err = line("title", e.Title, MaxTitle, true); err != nil {
		return e, err
	}
	if e.Author, err = line("author", e.Author, MaxAuthor, false); err != nil {
		return e, err
	}
	e.Body = strings.TrimSpace(e.Body)
	if !utf8.ValidString(e.Body) || utf8.RuneCountInString(e.Body) > MaxBody {
		return e, fmt.Errorf("body must be text of at most %d characters", MaxBody)
	}
	if e.URL = strings.TrimSpace(e.URL); e.URL != "" {
		u, err := url.Parse(e.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(e.URL) > MaxURL {
			return e, fmt.Errorf("url must be an http or https address of at most %d characters", MaxURL)
		}
	}
	if e.Repo = strings.TrimSpace(e.Repo); e.Repo != "" && !repoName.MatchString(e.Repo) {
		return e, errors.New("repo must be owner/name")
	}
	if e.Priority = strings.ToLower(strings.TrimSpace(e.Priority)); e.Priority != "" {
		word, ok := priorityWords[e.Priority]
		if !ok {
			return e, errors.New("priority must be p0, p1, p2 or p3")
		}
		e.Priority = shortPriority[word]
	}
	if e.Labels, err = labels(e.Labels); err != nil {
		return e, err
	}
	switch e.Trust = factory.Tier(strings.ToLower(strings.TrimSpace(string(e.Trust)))); e.Trust {
	case "", factory.TierOwner, factory.TierCollab, factory.TierStranger:
	default:
		return e, errors.New("trust must be owner, collaborator or stranger")
	}
	return e, nil
}

var shortPriority = map[string]string{"urgent": "p0", "high": "p1", "medium": "p2", "low": "p3"}

// line is one field of one line of text, trimmed, at most most characters.
func line(name, s string, most int, required bool) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		if required {
			return s, fmt.Errorf("%s is required", name)
		}
		return s, nil
	}
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > most || strings.ContainsFunc(s, unicode.IsControl) {
		return s, fmt.Errorf("%s must be one line of at most %d characters", name, most)
	}
	return s, nil
}

func labels(in []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, l := range in {
		l, err := line("a label", l, MaxLabel, false)
		if err != nil {
			return nil, err
		}
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	if len(out) > MaxLabels {
		return nil, fmt.Errorf("at most %d labels", MaxLabels)
	}
	return out, nil
}
