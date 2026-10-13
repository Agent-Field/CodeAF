package custom

import (
	"bytes"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/evanw/esbuild/pkg/api"
)

const (
	MaxFileBytes = 64 * 1024
	Rule         = "kit/custom"
)

// IsFile accepts one canonical definition path, never a filesystem escape.
func IsFile(name string) bool {
	return path.Dir(name) == "custom" && path.Clean(name) == name &&
		strings.HasSuffix(name, ".tsx") && path.Base(name) != ".tsx" &&
		!strings.ContainsAny(name, "\\\x00\r\n")
}

var forbidden = regexp.MustCompile(`\b(?:fetch|XMLHttpRequest|WebSocket|EventSource|eval|localStorage)\b|\b(?:Function|import)\s*\(|\bwindow\s*(?:\.|\?\.)\s*top\b|\bparent\s*(?:\.|\?\.)|\bdocument\s*(?:\.|\?\.)\s*cookie\b`)
var fromImport = regexp.MustCompile(`(?s)\b(?:import|export)\s+(?:type\s+)?(?:[\p{L}_$][\p{L}\p{N}_$]*\s*,?\s*)?(?:\{[^}]*\}|\*\s*(?:as\s+[\p{L}_$][\p{L}\p{N}_$]*)?)?\s*\bfrom\s*["']([^"']+)["']`)
var bareImport = regexp.MustCompile(`\bimport\s*["']([^"']+)["']`)
var identifierEscape = regexp.MustCompile(`\\u(?:\{[0-9a-fA-F]{1,6}\}|[0-9a-fA-F]{4})`)

// Validate uses the same parser and import door as Bundle, including imports
// that TypeScript would erase before compilation.
func Validate(file string, source []byte) []spec.Issue {
	_, issues := compile(file, source)
	return issues
}

func compile(file string, source []byte) (string, []spec.Issue) {
	if !IsFile(file) {
		return "", []spec.Issue{issue(file, 1, "file must be custom/<name>.tsx")}
	}
	if len(source) > MaxFileBytes {
		return "", []spec.Issue{issue(file, 1, fmt.Sprintf("custom file is %d bytes; limit is %d bytes", len(source), MaxFileBytes))}
	}
	if issues := sourceIssues(file, source); len(issues) != 0 {
		return "", issues
	}
	result := api.Build(api.BuildOptions{
		Stdin:       &api.StdinOptions{Contents: string(source), Sourcefile: file, Loader: api.LoaderTSX},
		Bundle:      true,
		Write:       false,
		Format:      api.FormatESModule,
		Platform:    api.PlatformBrowser,
		Target:      api.ES2020,
		LogLevel:    api.LogLevelSilent,
		TsconfigRaw: `{"compilerOptions":{"verbatimModuleSyntax":true}}`,
		Plugins:     []api.Plugin{importDoor()},
	})
	var issues []spec.Issue
	for _, message := range result.Errors {
		line := 1
		if message.Location != nil {
			line = message.Location.Line
		}
		issues = append(issues, issue(file, line, message.Text))
	}
	if len(issues) != 0 {
		return "", issues
	}
	return string(result.OutputFiles[0].Contents), nil
}

func importDoor() api.Plugin {
	return api.Plugin{Name: "custom-imports", Setup: func(build api.PluginBuild) {
		// Every resolution ends here. The compiler must never read a local file,
		// node_modules, or a URL supplied by an app's source.
		build.OnResolve(api.OnResolveOptions{Filter: ".*"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
			if allowedImport(args.Path) {
				return api.OnResolveResult{Path: args.Path, External: true}, nil
			}
			return api.OnResolveResult{Errors: []api.Message{{Text: "imports may use only react and @codeaf/bridge"}}}, nil
		})
	}}
}

func allowedImport(name string) bool { return name == "react" || name == "@codeaf/bridge" }

func sourceIssues(file string, source []byte) []spec.Issue {
	clean := normalizeIdentifiers(withoutComments(source))
	var issues []spec.Issue
	for _, match := range forbidden.FindAllIndex(clean, -1) {
		issues = append(issues, issue(file, sourceLine(clean, match[0]), "forbidden custom code: "+string(clean[match[0]:match[1]])))
	}
	for _, pattern := range []*regexp.Regexp{fromImport, bareImport} {
		for _, match := range pattern.FindAllSubmatchIndex(clean, -1) {
			name := string(clean[match[2]:match[3]])
			if !allowedImport(name) {
				issues = append(issues, issue(file, sourceLine(clean, match[2]), fmt.Sprintf("import %q is not allowed; use only react and @codeaf/bridge", name)))
			}
		}
	}
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].Message < issues[j].Message })
	return issues
}

func sourceLine(source []byte, offset int) int { return 1 + bytes.Count(source[:offset], []byte{'\n'}) }

func normalizeIdentifiers(source []byte) []byte {
	// JavaScript resolves identifier escapes before using browser globals.
	// Normalising letters keeps their source line while closing that spelling gap.
	return identifierEscape.ReplaceAllFunc(source, func(escape []byte) []byte {
		hex := strings.Trim(string(escape[2:]), "{}")
		value, _ := strconv.ParseUint(hex, 16, 32)
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' {
			return []byte{byte(value)}
		}
		return escape
	})
}

func issue(file string, line int, message string) spec.Issue {
	// The wave-1 issue shape has a JSON pointer but no numeric line field.
	// Keep source coordinates in the message instead of inventing a JSON path.
	return spec.Issue{File: file, Rule: Rule, Message: fmt.Sprintf("line %d: %s", line, message)}
}

func withoutComments(source []byte) []byte {
	clean := bytes.Clone(source)
	var quote byte
	for i := 0; i < len(clean); i++ {
		if quote != 0 {
			if clean[i] == '\\' {
				i++
			} else if clean[i] == quote {
				quote = 0
			}
			continue
		}
		if clean[i] == '\'' || clean[i] == '"' || clean[i] == '`' {
			quote = clean[i]
			continue
		}
		if clean[i] != '/' || i+1 == len(clean) || (clean[i+1] != '/' && clean[i+1] != '*') {
			continue
		}
		i = maskComment(clean, i)
	}
	return clean
}

func maskComment(clean []byte, start int) int {
	block := clean[start+1] == '*'
	clean[start], clean[start+1] = ' ', ' '
	for i := start + 2; i < len(clean); i++ {
		if block && clean[i] == '*' && i+1 < len(clean) && clean[i+1] == '/' {
			clean[i], clean[i+1] = ' ', ' '
			return i + 1
		}
		if clean[i] == '\n' {
			if !block {
				return i
			}
		} else {
			clean[i] = ' '
		}
	}
	return len(clean)
}
