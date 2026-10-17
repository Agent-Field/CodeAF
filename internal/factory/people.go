package factory

import (
	"regexp"
	"sort"
	"strings"
)

// PersonSummary is the small identity shared by every queue row.
type PersonSummary struct {
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// GitHubPerson is public profile data, including people who never signed in.
type GitHubPerson struct {
	PersonSummary
	HTMLURL string `json:"html_url"`
}

// CodeOwner preserves a CODEOWNERS pattern and its individual handles.
// Team and email owners remain patterns but cannot be invented as people.
type CodeOwner struct {
	Pattern string
	Handles []string
}

// ParseCodeOwners skips unsupported syntax rather than guessing its owners.
func ParseCodeOwners(text string) []CodeOwner {
	var out []CodeOwner
	for _, line := range strings.Split(text, "\n") {
		words := strings.Fields(strings.SplitN(line, "#", 2)[0])
		if len(words) == 0 || strings.ContainsAny(words[0], "![]\\") {
			continue
		}
		rule := CodeOwner{Pattern: words[0]}
		for _, word := range words[1:] {
			if strings.HasPrefix(word, "@") && !strings.Contains(word, "/") {
				if handle := NormalizeHandle(word); handle != "" {
					rule.Handles = append(rule.Handles, handle)
				}
			}
		}
		out = append(out, rule)
	}
	return out
}

// OwnersFor applies the last matching line to each planned or changed file.
func OwnersFor(rules []CodeOwner, files []string) []string {
	by := map[string]bool{}
	for _, file := range files {
		var owners []string
		for _, rule := range rules {
			if ownerMatch(rule.Pattern, file) {
				owners = rule.Handles
			}
		}
		for _, handle := range owners {
			by[handle] = true
		}
	}
	out := make([]string, 0, len(by))
	for handle := range by {
		out = append(out, handle)
	}
	sort.Strings(out)
	return out
}

func ownerMatch(pattern, file string) bool {
	anchored := strings.HasPrefix(pattern, "/") || strings.Contains(strings.TrimSuffix(pattern, "/"), "/")
	pattern = strings.TrimPrefix(pattern, "/")
	directory := strings.HasSuffix(pattern, "/")
	pattern = strings.TrimSuffix(pattern, "/")
	var b strings.Builder
	if anchored {
		b.WriteString("^")
	} else {
		b.WriteString("(^|/)")
	}
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	if directory {
		b.WriteString("/")
	} else {
		b.WriteString("($|/)")
	}
	re, err := regexp.Compile(b.String())
	return err == nil && re.MatchString(strings.TrimPrefix(file, "/"))
}

// MentionedHandles includes every identity a queue row or its details names.
func MentionedHandles(it Item) []string {
	out := []string{it.Person(), it.Author, it.Assignee, it.Starter, it.Taker}
	out = append(out, it.Owners...)
	if it.Ask != nil {
		out = append(out, it.Ask.By)
	}
	for _, c := range it.Comments {
		out = append(out, c.Author)
	}
	for _, q := range it.AnsweredQuestions {
		out = append(out, q.RoutedTo)
		if q.Answer != nil {
			out = append(out, q.Answer.By)
		}
	}
	if it.PullRequest != nil {
		out = append(out, it.PullRequest.MergedBy)
		for _, review := range it.PullRequest.Reviews {
			out = append(out, review.By)
		}
	}
	if it.Stream != nil {
		for _, phase := range it.Stream.Phases {
			out = append(out, phase.Handle)
		}
	}
	return out
}
