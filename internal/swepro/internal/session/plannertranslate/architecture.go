package plannertranslate

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

type architectureComponent struct {
	index   int
	name    string
	tagline string
	body    string
	raw     string
}

type headerMatch struct {
	index     int
	rawTitle  string
	offset    int
	endOffset int
}

var (
	componentHeaderRE = regexp.MustCompile(`(?m)^###\s+([0-9]+)\.\s*(.+?)$`)
	backtickTitleRE   = regexp.MustCompile("^`([^`]+)`\\s*(?:—|--)\\s*(.+)$")
	parenTitleRE      = regexp.MustCompile(`^(.+?)\s+\(([^)]+)\)$`)
	dependencyLineRE  = regexp.MustCompile(`(?i)\*\*Dependencies?\*\*\s*:\s*([^\n]+)`)
	dependencyRuleRE  = regexp.MustCompile(`(?im)^[0-9]+\.\s*\*?\*?([a-zA-Z][a-zA-Z0-9_./-]*?)\/?\*?\*?\s+(?:has no dependencies|depends? (?:only )?on|requires?)\s+(.+?)\.?$`)
)

// ParseArchitectureToDAG ports the programmatic fallback parser at
// planner-translate.ts:364-581. Filesystem failures and unparseable manifests
// return nil, matching the source's catch-all null.
func ParseArchitectureToDAG(workspace string) *DAGData {
	archPath := filepath.Join(workspace, ".codeaf", "plan", "architecture.md")
	contents, err := os.ReadFile(archPath)
	if err != nil {
		return nil
	}
	raw := string(contents)
	componentsBlock := sliceSection(raw, "## Components", "\n## ")
	if componentsBlock == "" {
		return nil
	}

	indices := componentHeaderRE.FindAllStringSubmatchIndex(componentsBlock, -1)
	headers := make([]headerMatch, 0, len(indices))
	for _, match := range indices {
		index, _ := strconv.Atoi(componentsBlock[match[2]:match[3]])
		title := strings.TrimSpace(componentsBlock[match[4]:match[5]])
		headers = append(headers, headerMatch{
			index: index, rawTitle: title,
			offset:    utf16Length(componentsBlock[:match[1]]),
			endOffset: utf16Length(componentsBlock),
		})
	}
	for index := 0; index < len(headers)-1; index++ {
		// Keep the source's approximate offset arithmetic verbatim.
		headers[index].endOffset =
			headers[index+1].offset - utf16Length(headers[index+1].rawTitle) - 8
	}

	components := make([]architectureComponent, 0, len(headers))
	for _, header := range headers {
		body := strings.TrimSpace(sliceUTF16Range(
			componentsBlock, header.offset, header.endOffset,
		))
		name := ""
		tagline := ""
		if match := backtickTitleRE.FindStringSubmatch(header.rawTitle); match != nil {
			name, tagline = match[1], match[2]
		} else if match := parenTitleRE.FindStringSubmatch(header.rawTitle); match != nil {
			tagline = strings.TrimSpace(match[1])
			name = strings.TrimRight(match[2], "/")
		} else {
			tokens := regexp.MustCompile(`\s+(?:—|--|\s+)\s+`).Split(header.rawTitle, -1)
			name = strings.NewReplacer("`", "", "'", "", `"`, "").Replace(tokens[0])
			if len(tokens) > 1 {
				tagline = strings.TrimSpace(strings.Join(tokens[1:], " "))
			}
		}
		components = append(components, architectureComponent{
			index: header.index, name: name, tagline: tagline, body: body, raw: header.rawTitle,
		})
	}
	if len(components) == 0 {
		return nil
	}

	knownNames := make([]string, 0, len(components))
	known := make(map[string]bool, len(components))
	for _, component := range components {
		knownNames = append(knownNames, component.name)
		known[component.name] = true
	}
	adjacency := map[string][]string{}

	// Edge source 1: per-component Dependencies lines.
	for _, component := range components {
		match := dependencyLineRE.FindStringSubmatch(component.body)
		if match == nil || regexp.MustCompile(`(?i)none|stdlib`).MatchString(match[1]) {
			continue
		}
		for _, rawDep := range regexp.MustCompile(`[,;]`).Split(match[1], -1) {
			dep := strings.TrimSpace(strings.TrimRight(
				strings.NewReplacer("`", "", "'", "", `"`, "").Replace(rawDep), "/",
			))
			if dep != "" && known[dep] {
				adjacency[component.name] = appendUnique(adjacency[component.name], dep)
			}
		}
	}

	// Edge source 2: numbered prose rules.
	graphBlock := sliceSection(raw, "## Module Dependency Graph", "\n## ")
	if graphBlock != "" {
		for _, match := range dependencyRuleRE.FindAllStringSubmatch(graphBlock, -1) {
			subject := strings.TrimRight(match[1], "/")
			rest := match[2]
			if regexp.MustCompile(`(?i)no dependencies|nothing|stdlib only|none`).MatchString(rest) {
				continue
			}
			for _, rawDep := range regexp.MustCompile(`(?i)(?:,|and)`).Split(rest, -1) {
				dep := strings.TrimSpace(strings.TrimRight(
					strings.NewReplacer("`", "", "'", "", `"`, "", "*", "").Replace(rawDep), "/",
				))
				if dep == "" {
					continue
				}
				resolved := resolveKnownName(dep, knownNames)
				subjectResolved := resolveKnownName(subject, knownNames)
				if resolved != "" && subjectResolved != "" {
					adjacency[subjectResolved] =
						appendUnique(adjacency[subjectResolved], resolved)
				}
			}
		}
	}

	tasks := make([]DAGTask, 0, len(components))
	edgeCount := 0
	for _, component := range components {
		deps := make([]DAGDep, 0, len(adjacency[component.name]))
		for _, upstream := range adjacency[component.name] {
			deps = append(deps, DAGDep{
				FromTask: nameToKey(upstream), Kind: "feeds_into",
			})
		}
		edgeCount += len(deps)
		firstParagraph := component.tagline
		if paragraphs := regexp.MustCompile(`\n\s*\n`).Split(component.body, -1); len(paragraphs) > 0 {
			firstParagraph = paragraphs[0]
		}
		createPath := component.name
		if !strings.Contains(createPath, ".") {
			createPath += ".go"
		}
		description := strings.Join([]string{
			"## Description",
			chooseTagline(component),
			"",
			"## Files",
			"- Create: " + createPath,
			"",
			"## Acceptance",
			"- [ ] Compiles cleanly",
			"- [ ] Matches architect's spec",
			"",
			"Reference: .codeaf/plan/architecture.md",
			"",
			"<!-- Programmatic-fallback DAG: LLM-derived description not available.",
			"     First paragraph from architecture.md follows for grounding:",
			sliceUTF16(firstParagraph, 0, 600),
			"-->",
		}, "\n")
		title := "Implement " + component.name + " — " +
			sliceUTF16(component.tagline, 0, 60)
		tasks = append(tasks, DAGTask{
			TaskKey:     nameToKey(component.name),
			Title:       sliceUTF16(title, 0, 140),
			Kind:        "code",
			Description: description,
			Tags: []string{
				"agent:fixer", "scope:medium", "source:programmatic-fallback",
			},
			Deps: deps,
		})
	}
	summary := "Programmatic fallback: parsed " + strconv.Itoa(len(tasks)) +
		" components and " + strconv.Itoa(edgeCount) + " edges from architecture.md."
	return &DAGData{Summary: &summary, Tasks: tasks}
}

func chooseTagline(component architectureComponent) string {
	if component.tagline != "" {
		return component.tagline
	}
	return "Module " + component.name + " per architecture.md."
}

// sliceSection keeps the source's doubled-newline regexp construction:
// nextHeaderPrefix already begins with "\n", and the function prepends one.
func sliceSection(text, startHeader, nextHeaderPrefix string) string {
	startIndex := strings.Index(text, startHeader)
	if startIndex < 0 {
		return ""
	}
	afterStart := startIndex + len(startHeader)
	nextNeedle := "\n" + nextHeaderPrefix
	nextRelative := strings.Index(text[afterStart:], nextNeedle)
	endIndex := len(text)
	if nextRelative >= 0 {
		endIndex = afterStart + nextRelative
	}
	return text[afterStart:endIndex]
}

func resolveKnownName(candidate string, known []string) string {
	for _, name := range known {
		if name == candidate {
			return name
		}
	}
	parts := strings.Split(candidate, "/")
	last := parts[len(parts)-1]
	for _, name := range known {
		if name == last || strings.HasSuffix(name, "/"+last) {
			return name
		}
	}
	return ""
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func nameToKey(name string) string {
	value := strings.NewReplacer("`", "", "'", "", `"`, "").Replace(name)
	value = strings.NewReplacer("/", "-", ".", "-").Replace(value)
	value = regexp.MustCompile(`[^a-zA-Z0-9-]`).ReplaceAllString(value, "-")
	value = regexp.MustCompile(`-+`).ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	return strings.ToLower(value)
}

// parseDependencyGraph ports planner-translate.ts:599-637. It is deliberately
// retained even though ParseArchitectureToDAG never calls it in the source.
func parseDependencyGraph(graphBlock string, knownNames []string) map[string][]string {
	out := map[string][]string{}
	body := graphBlock
	fence := regexp.MustCompile("```[a-zA-Z]*\\n([\\s\\S]*?)```").FindStringSubmatch(graphBlock)
	if fence != nil {
		body = fence[1]
	}
	type stackEntry struct {
		indent int
		name   string
	}
	stack := []stackEntry{}
	lineNameRE := regexp.MustCompile("[`a-zA-Z0-9_./-]+")
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		prefix := regexp.MustCompile(`^[\s│├└─| ]+`).FindString(line)
		indent := utf16Length(prefix)
		stripped := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		match := lineNameRE.FindString(stripped)
		if match == "" {
			continue
		}
		rawName := strings.NewReplacer("`", "", "'", "", `"`, "").Replace(match)
		candidates := []string{
			rawName,
			regexp.MustCompile(`\.go$`).ReplaceAllString(rawName, ""),
			regexp.MustCompile(`/[^/]+\.go$`).ReplaceAllString(rawName, ""),
		}
		name := ""
		for _, candidate := range candidates {
			for _, known := range knownNames {
				if candidate == known {
					name = candidate
					break
				}
			}
			if name != "" {
				break
			}
		}
		if name == "" {
			name = regexp.MustCompile(`\.go$`).ReplaceAllString(rawName, "")
		}
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			parent := stack[len(stack)-1].name
			out[parent] = appendUnique(out[parent], name)
		}
		stack = append(stack, stackEntry{indent: indent, name: name})
	}
	return out
}

func utf16Length(value string) int { return len(utf16.Encode([]rune(value))) }

func sliceUTF16(value string, start, end int) string {
	units := utf16.Encode([]rune(value))
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if start > len(units) {
		start = len(units)
	}
	if end > len(units) {
		end = len(units)
	}
	return string(utf16.Decode(units[start:end]))
}

func sliceUTF16Range(value string, start, end int) string {
	return sliceUTF16(value, start, end)
}
