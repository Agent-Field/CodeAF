// Package symbolgraph ports src/session/symbol-graph.ts lines 1-417.
//
// Source masking works on bytes to preserve offsets between the masked and
// original Go strings. All syntax recognized by the TypeScript regexes is
// ASCII, so byte offsets produce the same line numbers as JavaScript UTF-16
// offsets even when astral characters occur earlier in a file.
package symbolgraph

import (
	"bytes"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type SourceFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type SymbolKind string

const (
	KindFunction SymbolKind = "function"
	KindClass    SymbolKind = "class"
	KindConst    SymbolKind = "const"
	KindType     SymbolKind = "type"
	KindMethod   SymbolKind = "method"
	KindUnknown  SymbolKind = "unknown"
)

type SymbolTag struct {
	Name string     `json:"name"`
	Line int        `json:"line"`
	Kind SymbolKind `json:"kind"`
}

type SymbolExtraction struct {
	Defs []SymbolTag `json:"defs"`
	Refs []SymbolTag `json:"refs"`

	// constructorSource preserves the reachable JavaScript prototype hazard
	// where extension "constructor" calls the inherited Object constructor and
	// returns the SourceFile rather than a SymbolExtraction.
	constructorSource *SourceFile
}

func (extraction SymbolExtraction) MarshalJSON() ([]byte, error) {
	if extraction.constructorSource != nil {
		return jscompat.Stringify(*extraction.constructorSource)
	}
	type wire struct {
		Defs []SymbolTag `json:"defs"`
		Refs []SymbolTag `json:"refs"`
	}
	return jscompat.Stringify(wire{Defs: extraction.Defs, Refs: extraction.Refs})
}

type SymbolExtractor func(file SourceFile) SymbolExtraction
type SymbolExtractorMap map[string]SymbolExtractor

const MinNameLen = 3
const SelfLoopWeight = 0.1
const MIN_NAME_LEN = MinNameLen
const SELF_LOOP_WEIGHT = SelfLoopWeight

const jsSpace = `[\t\n\x0b\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

var (
	identifierRe    = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)
	fileExtensionRe = regexp.MustCompile(`(?:^|\.)([^./]+)$`)
	realNameRe      = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)
	uppercaseRe     = regexp.MustCompile(`[A-Z]`)

	tsFunctionRe = regexp.MustCompile(
		`\b(?:export` + jsSpace + `+(?:default` + jsSpace + `+)?)?` +
			`(?:async` + jsSpace + `+)?function` + jsSpace + `+([A-Za-z_$][A-Za-z0-9_$]*)`,
	)
	tsClassRe = regexp.MustCompile(
		`\b(?:export` + jsSpace + `+(?:default` + jsSpace + `+)?)?class` +
			jsSpace + `+([A-Za-z_$][A-Za-z0-9_$]*)`,
	)
	tsInterfaceRe = regexp.MustCompile(
		`\b(?:export` + jsSpace + `+)?interface` + jsSpace + `+([A-Za-z_$][A-Za-z0-9_$]*)`,
	)
	tsTypeRe = regexp.MustCompile(
		`\b(?:export` + jsSpace + `+)?type` + jsSpace + `+([A-Za-z_$][A-Za-z0-9_$]*)`,
	)
	tsArrowConstRe = regexp.MustCompile(
		`\bexport` + jsSpace + `+const` + jsSpace + `+([A-Za-z_$][A-Za-z0-9_$]*)` +
			jsSpace + `*(?::[^=]+)?=` + jsSpace + `*(?:async` + jsSpace + `+)?[^=\n]*?=>`,
	)
	tsClassStartRe = regexp.MustCompile(`\bclass` + jsSpace + `+[A-Za-z_$][A-Za-z0-9_$]*`)
	tsMethodRe     = regexp.MustCompile(
		`(?:^|\n)` + jsSpace + `*` +
			`(?:(?:public|private|protected|static|abstract|async|get|set|override|readonly)` + jsSpace + `+)*` +
			`([A-Za-z_$][A-Za-z0-9_$]*)` + jsSpace + `*` +
			`(?:<[^\n>{}]*>)?` + jsSpace + `*\([^\n]*\)` + jsSpace + `*` +
			`(?::[^\n{]+)?` + jsSpace + `*\{`,
	)
	pythonFunctionRe = regexp.MustCompile(
		`(?:^|\n)` + jsSpace + `*(?:async` + jsSpace + `+)?def` +
			jsSpace + `+([A-Za-z_$][A-Za-z0-9_$]*)`,
	)
	pythonClassRe = regexp.MustCompile(
		`(?:^|\n)` + jsSpace + `*class` + jsSpace + `+([A-Za-z_$][A-Za-z0-9_$]*)`,
	)
	goFunctionRe = regexp.MustCompile(
		`\bfunc` + jsSpace + `+(?:\([^)]*\)` + jsSpace + `*)?` +
			`([A-Za-z_$][A-Za-z0-9_$]*)` + jsSpace + `*\(`,
	)
	goTypeRe = regexp.MustCompile(
		`\btype` + jsSpace + `+([A-Za-z_$][A-Za-z0-9_$]*)` + jsSpace +
			`+(?:struct|interface|[A-Za-z_$][A-Za-z0-9_$]*)`,
	)
	genericDefinitionRe = regexp.MustCompile(
		`(?:^|\n)` + jsSpace + `*(?:export` + jsSpace + `+)?` +
			`(?:const|let|var|function|class|interface|type)` + jsSpace +
			`+([A-Za-z_$][A-Za-z0-9_$]*)`,
	)
	genericKeywordRe = regexp.MustCompile(
		`(?:const|let|var|function|class|interface|type)` + jsSpace + `+`,
	)

	keywords = map[string]bool{
		"and": true, "as": true, "async": true, "await": true, "break": true,
		"case": true, "catch": true, "class": true, "const": true, "continue": true,
		"default": true, "def": true, "defer": true, "delete": true, "do": true,
		"else": true, "enum": true, "export": true, "extends": true, "false": true,
		"finally": true, "for": true, "from": true, "func": true, "function": true,
		"go": true, "goto": true, "if": true, "implements": true, "import": true,
		"in": true, "interface": true, "is": true, "let": true, "map": true,
		"new": true, "nil": true, "not": true, "null": true, "of": true,
		"package": true, "pass": true, "private": true, "protected": true,
		"public": true, "range": true, "readonly": true, "return": true,
		"select": true, "self": true, "static": true, "struct": true, "switch": true,
		"this": true, "throw": true, "true": true, "try": true, "type": true,
		"var": true, "where": true, "while": true, "with": true, "yield": true,
	}
	commonNonSymbols = map[string]bool{
		"any": true, "boolean": true, "console": true, "Error": true,
		"false": true, "Math": true, "never": true, "number": true,
		"object": true, "Promise": true, "string": true, "true": true,
		"undefined": true, "unknown": true, "void": true,
	}
)

// OrderedRecord represents a JavaScript ordinary object's own enumerable
// properties. Array-index keys enumerate numerically first; other keys retain
// insertion order.
type OrderedRecord[V any] struct {
	keys      []string
	values    map[string]V
	undefined map[string]bool
}

func NewOrderedRecord[V any]() *OrderedRecord[V] {
	return &OrderedRecord[V]{
		values: make(map[string]V), undefined: make(map[string]bool),
	}
}

func (record *OrderedRecord[V]) Set(key string, value V) {
	if _, exists := record.values[key]; !exists && !record.undefined[key] {
		record.keys = append(record.keys, key)
	}
	record.values[key] = value
	delete(record.undefined, key)
}

func (record *OrderedRecord[V]) SetUndefined(key string) {
	if _, exists := record.values[key]; !exists && !record.undefined[key] {
		record.keys = append(record.keys, key)
	}
	delete(record.values, key)
	record.undefined[key] = true
}

func (record *OrderedRecord[V]) Get(key string) (V, bool) {
	value, ok := record.values[key]
	return value, ok
}

func (record *OrderedRecord[V]) Keys() []string {
	indexKeys := []string{}
	otherKeys := []string{}
	for _, key := range record.keys {
		if _, ok := arrayIndex(key); ok {
			indexKeys = append(indexKeys, key)
		} else {
			otherKeys = append(otherKeys, key)
		}
	}
	sort.Slice(indexKeys, func(i, j int) bool {
		left, _ := arrayIndex(indexKeys[i])
		right, _ := arrayIndex(indexKeys[j])
		return left < right
	})
	return append(indexKeys, otherKeys...)
}

func (record *OrderedRecord[V]) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	written := 0
	for _, key := range record.Keys() {
		if record.undefined[key] {
			continue
		}
		if written != 0 {
			out.WriteByte(',')
		}
		encodedKey, err := jscompat.Stringify(key)
		if err != nil {
			return nil, err
		}
		encodedValue, err := jscompat.Stringify(record.values[key])
		if err != nil {
			return nil, err
		}
		out.Write(encodedKey)
		out.WriteByte(':')
		out.Write(encodedValue)
		written++
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

func arrayIndex(key string) (uint64, bool) {
	if key == "" {
		return 0, false
	}
	value, err := strconv.ParseUint(key, 10, 32)
	if err != nil || value == 1<<32-1 || strconv.FormatUint(value, 10) != key {
		return 0, false
	}
	return value, true
}

type SymbolGraph struct {
	Files      []string                                `json:"files"`
	DefsByFile *OrderedRecord[[]SymbolTag]             `json:"defsByFile"`
	Edges      *OrderedRecord[*OrderedRecord[float64]] `json:"edges"`
}

func extensionOf(path string) string {
	match := fileExtensionRe.FindStringSubmatch(jsLowerCase(path))
	if match == nil {
		return ""
	}
	return match[1]
}

func jsLowerCase(value string) string {
	ascii := true
	for i := 0; i < len(value); i++ {
		if value[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToLower(value)
	}
	runes := []rune(value)
	var out strings.Builder
	out.Grow(len(value))
	for index, char := range runes {
		switch {
		case char == 0x0130:
			out.WriteString("i\u0307")
		case char == 0x03A3 && isFinalSigma(runes, index):
			out.WriteRune(0x03C2)
		default:
			out.WriteRune(unicode.ToLower(char))
		}
	}
	return out.String()
}

func isFinalSigma(runes []rune, index int) bool {
	before := index - 1
	for before >= 0 && isCaseIgnorable(runes[before]) {
		before--
	}
	if before < 0 || !isCased(runes[before]) {
		return false
	}
	after := index + 1
	for after < len(runes) && isCaseIgnorable(runes[after]) {
		after++
	}
	return after >= len(runes) || !isCased(runes[after])
}

func isCased(char rune) bool {
	return unicode.IsUpper(char) || unicode.IsLower(char) || unicode.IsTitle(char) ||
		unicode.Is(unicode.Other_Lowercase, char) || unicode.Is(unicode.Other_Uppercase, char)
}

func isCaseIgnorable(char rune) bool {
	switch char {
	case '\'', 0x2019, 0x00AD, 0x02B9, 0x0385, 0x1FBF, 0x1FC1, 0x1FCD, 0x1FCE,
		0x1FCF, 0x1FDD, 0x1FDE, 0x1FDF, 0x1FED, 0x1FEE, 0x1FEF, 0x1FFD, 0x1FFE,
		0x2027:
		return true
	}
	return unicode.Is(unicode.Mn, char) || unicode.Is(unicode.Me, char) ||
		unicode.Is(unicode.Cf, char) || unicode.Is(unicode.Lm, char) ||
		unicode.Is(unicode.Sk, char)
}

func maskNonCode(source string, hashComments bool) string {
	chars := []byte(source)
	var quote byte
	escaped := false
	blockComment := false
	for i := 0; i < len(chars); i++ {
		current := chars[i]
		var next byte
		if i+1 < len(chars) {
			next = chars[i+1]
		}
		if blockComment {
			if current == '*' && next == '/' {
				chars[i] = ' '
				chars[i+1] = ' '
				i++
				blockComment = false
			} else if current != '\n' {
				chars[i] = ' '
			}
			continue
		}
		if quote != 0 {
			if current == '\n' && quote != '`' {
				quote = 0
				escaped = false
				continue
			}
			if escaped {
				if current != '\n' {
					chars[i] = ' '
				}
				escaped = false
				continue
			}
			if current == '\\' {
				chars[i] = ' '
				escaped = true
				continue
			}
			if current == quote {
				quote = 0
			}
			if current != '\n' {
				chars[i] = ' '
			}
			continue
		}
		if current == '/' && next == '*' {
			chars[i] = ' '
			chars[i+1] = ' '
			i++
			blockComment = true
			continue
		}
		if current == '/' && next == '/' {
			chars[i] = ' '
			chars[i+1] = ' '
			i++
			for i+1 < len(chars) && chars[i+1] != '\n' {
				i++
				chars[i] = ' '
			}
			continue
		}
		if hashComments && current == '#' {
			for i < len(chars) && chars[i] != '\n' {
				chars[i] = ' '
				i++
			}
			i--
			continue
		}
		if current == '\'' || current == '"' || current == '`' {
			quote = current
			chars[i] = ' '
		}
	}
	return string(chars)
}

func lineNumber(source string, offset int) int {
	return 1 + strings.Count(source[:offset], "\n")
}

func looksLikeRealName(name string) bool {
	if len(name) < MinNameLen || keywords[name] || commonNonSymbols[name] {
		return false
	}
	return realNameRe.MatchString(name) &&
		(uppercaseRe.MatchString(name) ||
			strings.Contains(name, "_") || len(name) >= MinNameLen)
}

func addDefinition(defs *[]SymbolTag, seen map[string]bool, tag SymbolTag) {
	key := string(tag.Kind) + ":" + tag.Name + ":" + strconv.Itoa(tag.Line)
	if seen[key] {
		return
	}
	seen[key] = true
	*defs = append(*defs, tag)
}

func identifiersAsRefs(file SourceFile, masked string, defs []SymbolTag) []SymbolTag {
	refs := []SymbolTag{}
	definitionNames := map[string]bool{}
	for _, tag := range defs {
		definitionNames[tag.Name] = true
	}
	for _, location := range identifierRe.FindAllStringIndex(masked, -1) {
		name := masked[location[0]:location[1]]
		var previous byte
		if location[0] > 0 {
			previous = masked[location[0]-1]
		}
		if !looksLikeRealName(name) || previous == '.' || definitionNames[name] {
			continue
		}
		refs = append(refs, SymbolTag{Name: name, Line: lineNumber(file.Content, location[0]), Kind: KindUnknown})
	}
	return refs
}

func matchingBrace(source string, opening int) int {
	depth := 0
	for i := opening; i < len(source); i++ {
		if source[i] == '{' {
			depth++
		}
		if source[i] == '}' {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(source)
}

func braceDepth(source string, start, end int) int {
	depth := 0
	for i := start; i < end; i++ {
		if source[i] == '{' {
			depth++
		} else if source[i] == '}' {
			depth--
		}
	}
	return depth
}

func addMatches(file SourceFile, masked string, defs *[]SymbolTag, seen map[string]bool, pattern *regexp.Regexp, kind SymbolKind) {
	for _, match := range pattern.FindAllStringSubmatchIndex(masked, -1) {
		name := masked[match[2]:match[3]]
		if !looksLikeRealName(name) {
			continue
		}
		addDefinition(defs, seen, SymbolTag{Name: name, Line: lineNumber(file.Content, match[0]), Kind: kind})
	}
}

func extractTypeScript(file SourceFile) SymbolExtraction {
	masked := maskNonCode(file.Content, false)
	defs := []SymbolTag{}
	seen := map[string]bool{}
	addMatches(file, masked, &defs, seen, tsFunctionRe, KindFunction)
	addMatches(file, masked, &defs, seen, tsClassRe, KindClass)
	addMatches(file, masked, &defs, seen, tsInterfaceRe, KindType)
	addMatches(file, masked, &defs, seen, tsTypeRe, KindType)
	addMatches(file, masked, &defs, seen, tsArrowConstRe, KindConst)

	for _, classMatch := range tsClassStartRe.FindAllStringIndex(masked, -1) {
		openingRelative := strings.Index(masked[classMatch[1]:], "{")
		if openingRelative < 0 {
			continue
		}
		opening := classMatch[1] + openingRelative
		closing := matchingBrace(masked, opening)
		bodyStart := opening + 1
		body := masked[bodyStart:closing]
		for _, methodMatch := range tsMethodRe.FindAllStringSubmatchIndex(body, -1) {
			name := body[methodMatch[2]:methodMatch[3]]
			relative := methodMatch[0]
			absolute := bodyStart + relative
			if !looksLikeRealName(name) || braceDepth(masked, bodyStart, absolute) != 0 {
				continue
			}
			full := body[methodMatch[0]:methodMatch[1]]
			nameOffset := absolute + strings.LastIndex(full, name)
			addDefinition(&defs, seen, SymbolTag{
				Name: name, Line: lineNumber(file.Content, nameOffset), Kind: KindMethod,
			})
		}
	}

	sort.SliceStable(defs, func(i, j int) bool {
		if defs[i].Line != defs[j].Line {
			return defs[i].Line < defs[j].Line
		}
		if compare := jscompat.LocaleCompare(defs[i].Name, defs[j].Name); compare != 0 {
			return compare < 0
		}
		return jscompat.LocaleCompare(string(defs[i].Kind), string(defs[j].Kind)) < 0
	})
	return SymbolExtraction{Defs: defs, Refs: identifiersAsRefs(file, masked, defs)}
}

func extractPython(file SourceFile) SymbolExtraction {
	masked := maskNonCode(file.Content, true)
	defs := []SymbolTag{}
	seen := map[string]bool{}
	for _, patternKind := range []struct {
		pattern *regexp.Regexp
		kind    SymbolKind
	}{
		{pythonFunctionRe, KindFunction},
		{pythonClassRe, KindClass},
	} {
		for _, match := range patternKind.pattern.FindAllStringSubmatchIndex(masked, -1) {
			name := masked[match[2]:match[3]]
			if !looksLikeRealName(name) {
				continue
			}
			full := masked[match[0]:match[1]]
			addDefinition(&defs, seen, SymbolTag{
				Name: name, Line: lineNumber(file.Content, match[0]+strings.Index(full, name)), Kind: patternKind.kind,
			})
		}
	}
	sort.SliceStable(defs, func(i, j int) bool {
		if defs[i].Line != defs[j].Line {
			return defs[i].Line < defs[j].Line
		}
		return jscompat.LocaleCompare(defs[i].Name, defs[j].Name) < 0
	})
	return SymbolExtraction{Defs: defs, Refs: identifiersAsRefs(file, masked, defs)}
}

func extractGo(file SourceFile) SymbolExtraction {
	masked := maskNonCode(file.Content, false)
	defs := []SymbolTag{}
	seen := map[string]bool{}
	for _, patternKind := range []struct {
		pattern *regexp.Regexp
		kind    SymbolKind
	}{
		{goFunctionRe, KindFunction},
		{goTypeRe, KindType},
	} {
		for _, match := range patternKind.pattern.FindAllStringSubmatchIndex(masked, -1) {
			name := masked[match[2]:match[3]]
			if !looksLikeRealName(name) {
				continue
			}
			full := masked[match[0]:match[1]]
			addDefinition(&defs, seen, SymbolTag{
				Name: name, Line: lineNumber(file.Content, match[0]+strings.Index(full, name)), Kind: patternKind.kind,
			})
		}
	}
	sort.SliceStable(defs, func(i, j int) bool {
		if defs[i].Line != defs[j].Line {
			return defs[i].Line < defs[j].Line
		}
		return jscompat.LocaleCompare(defs[i].Name, defs[j].Name) < 0
	})
	return SymbolExtraction{Defs: defs, Refs: identifiersAsRefs(file, masked, defs)}
}

func extractGenericDefinitions(file SourceFile) SymbolExtraction {
	masked := maskNonCode(file.Content, true)
	defs := []SymbolTag{}
	seen := map[string]bool{}
	for _, match := range genericDefinitionRe.FindAllStringSubmatchIndex(masked, -1) {
		name := masked[match[2]:match[3]]
		if !looksLikeRealName(name) {
			continue
		}
		full := masked[match[0]:match[1]]
		keyword := ""
		if keywordMatch := genericKeywordRe.FindString(full); keywordMatch != "" {
			keyword = jscompat.Trim(keywordMatch)
		}
		kind := KindUnknown
		switch keyword {
		case "function":
			kind = KindFunction
		case "class":
			kind = KindClass
		case "type", "interface":
			kind = KindType
		case "const":
			kind = KindConst
		}
		addDefinition(&defs, seen, SymbolTag{
			Name: name, Line: lineNumber(file.Content, match[0]+strings.Index(full, name)), Kind: kind,
		})
	}
	sort.SliceStable(defs, func(i, j int) bool {
		if defs[i].Line != defs[j].Line {
			return defs[i].Line < defs[j].Line
		}
		return jscompat.LocaleCompare(defs[i].Name, defs[j].Name) < 0
	})
	return SymbolExtraction{Defs: defs, Refs: []SymbolTag{}}
}

var DefaultExtractors = SymbolExtractorMap{
	"ts": extractTypeScript, "tsx": extractTypeScript, "js": extractTypeScript,
	"jsx": extractTypeScript, "mjs": extractTypeScript, "cjs": extractTypeScript,
	"py": extractPython, "go": extractGo,
}

var DEFAULT_EXTRACTORS = DefaultExtractors

// ExtractSymbols selects a language extractor by the lowercased final
// extension, falling back to generic definition extraction.
func ExtractSymbols(file SourceFile, extractors ...SymbolExtractorMap) SymbolExtraction {
	selected := DefaultExtractors
	if len(extractors) != 0 {
		selected = extractors[0]
	}
	if extractor := selected[extensionOf(file.Path)]; extractor != nil {
		return extractor(file)
	}
	extension := extensionOf(file.Path)
	if extension == "constructor" {
		copy := file
		return SymbolExtraction{constructorSource: &copy}
	}
	if extension == "__proto__" {
		panic("symbolgraph: JavaScript __proto__ extractor is not callable")
	}
	return extractGenericDefinitions(file)
}

func utf16Compare(left, right string) int {
	a := utf16.Encode([]rune(left))
	b := utf16.Encode([]rune(right))
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

func defaultSort(values []string) {
	sort.SliceStable(values, func(i, j int) bool {
		return utf16Compare(values[i], values[j]) < 0
	})
}

// BuildSymbolGraph extracts definitions and references, resolves references to
// the first other owning file, and adds self-loop weight for unreferenced defs.
func BuildSymbolGraph(files []SourceFile) SymbolGraph {
	orderedFiles := append([]SourceFile(nil), files...)
	sort.SliceStable(orderedFiles, func(i, j int) bool {
		return jscompat.LocaleCompare(orderedFiles[i].Path, orderedFiles[j].Path) < 0
	})
	defsByFile := NewOrderedRecord[[]SymbolTag]()
	refsByFile := NewOrderedRecord[[]SymbolTag]()
	edges := NewOrderedRecord[*OrderedRecord[float64]]()
	for _, file := range orderedFiles {
		extracted := ExtractSymbols(file)
		if extracted.constructorSource != nil {
			defsByFile.SetUndefined(file.Path)
			refsByFile.SetUndefined(file.Path)
		} else {
			defsByFile.Set(file.Path, extracted.Defs)
			refsByFile.Set(file.Path, extracted.Refs)
		}
		edges.Set(file.Path, NewOrderedRecord[float64]())
	}

	owners := map[string][]string{}
	defPaths := defsByFile.Keys()
	defaultSort(defPaths)
	for _, path := range defPaths {
		definitions, _ := defsByFile.Get(path)
		for _, definition := range definitions {
			paths := owners[definition.Name]
			found := false
			for _, existing := range paths {
				if existing == path {
					found = true
					break
				}
			}
			if !found {
				paths = append(paths, path)
			}
			owners[definition.Name] = paths
		}
	}

	referencedDefinitions := map[string]bool{}
	refPaths := refsByFile.Keys()
	defaultSort(refPaths)
	for _, sourcePath := range refPaths {
		refs, _ := refsByFile.Get(sourcePath)
		for _, ref := range refs {
			targets := owners[ref.Name]
			targetPath := ""
			for _, candidate := range targets {
				if candidate != sourcePath {
					targetPath = candidate
					break
				}
			}
			if targetPath == "" {
				continue
			}
			sourceEdges, _ := edges.Get(sourcePath)
			weight, _ := sourceEdges.Get(targetPath)
			sourceEdges.Set(targetPath, weight+1)
			referencedDefinitions[targetPath+":"+ref.Name] = true
		}
	}

	defPaths = defsByFile.Keys()
	defaultSort(defPaths)
	for _, path := range defPaths {
		definitions, _ := defsByFile.Get(path)
		hasUnreferencedDefinition := false
		for _, definition := range definitions {
			if !referencedDefinitions[path+":"+definition.Name] {
				hasUnreferencedDefinition = true
				break
			}
		}
		if hasUnreferencedDefinition {
			pathEdges, _ := edges.Get(path)
			weight, _ := pathEdges.Get(path)
			pathEdges.Set(path, weight+SelfLoopWeight)
		}
	}

	graphFiles := defsByFile.Keys()
	defaultSort(graphFiles)
	return SymbolGraph{Files: graphFiles, DefsByFile: defsByFile, Edges: edges}
}
