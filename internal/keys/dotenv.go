package keys

import "strings"

type pair struct{ name, value string }

// parseDotenv reads KEY=VALUE lines. It skips blanks and # comments, accepts
// an "export " prefix, and strips one layer of matching quotes.
func parseDotenv(text string) []pair {
	var out []pair
	for _, line := range strings.Split(text, "\n") {
		if p, ok := parseLine(line); ok {
			out = append(out, p)
		}
	}
	return out
}

func parseLine(line string) (pair, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return pair{}, false
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	name, value, found := strings.Cut(line, "=")
	name = strings.TrimSpace(name)
	if !found || name == "" {
		return pair{}, false
	}
	return pair{name, unquote(strings.TrimSpace(value))}, true
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// DotenvValues maps each name a .env text sets to its value; when a name is set
// twice the later line wins, as a dotenv loader would read it.
func DotenvValues(text string) map[string]string {
	out := map[string]string{}
	for _, p := range parseDotenv(text) {
		out[p.name] = p.value
	}
	return out
}
