// Package repomap ports src/session/repo-map.ts lines 1-165.
//
// symbol-graph.ts is outside this bundle, so BuildRepoMap accepts the narrow
// SymbolGraphBuilder interface instead of importing an unported dependency.
package repomap

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

const (
	PAGERANK_DAMPING        float64 = 0.85
	PAGERANK_TOLERANCE      float64 = 1e-8
	PAGERANK_MAX_ITERATIONS         = 100
	DEFAULT_BUDGET_CHARS    float64 = 12_000
	FOCUS_PATH_WEIGHT       float64 = 3
	FOCUS_IDENTIFIER_WEIGHT float64 = 2
)

type SourceFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type SymbolKind string

type SymbolTag struct {
	Name string            `json:"name"`
	Line jscompat.JSNumber `json:"line"`
	Kind SymbolKind        `json:"kind"`
}

type SymbolGraph struct {
	Files      []string
	DefsByFile *jscompat.OrderedMap[string, []SymbolTag]
	Edges      *jscompat.OrderedMap[string, *jscompat.OrderedMap[string, float64]]
}

type RankFilesOptions struct {
	Personalization *jscompat.OrderedMap[string, float64]
}

type RankedFile struct {
	Path  string            `json:"path"`
	Score jscompat.JSNumber `json:"score"`
}

type BuildRepoMapOptions struct {
	BudgetChars      *float64
	FocusPaths       []string
	FocusIdentifiers []string
}

type SymbolGraphBuilder interface {
	BuildSymbolGraph(files []SourceFile) SymbolGraph
}

type SymbolGraphBuilderFunc func(files []SourceFile) SymbolGraph

func (f SymbolGraphBuilderFunc) BuildSymbolGraph(files []SourceFile) SymbolGraph {
	return f(files)
}

func uniformPersonalization(paths []string) []float64 {
	if len(paths) == 0 {
		return []float64{}
	}
	value := 1 / float64(len(paths))
	out := make([]float64, len(paths))
	for i := range out {
		out[i] = value
	}
	return out
}

func personalizationVector(paths []string, personalization *jscompat.OrderedMap[string, float64]) []float64 {
	values := make([]float64, len(paths))
	for i, path := range paths {
		if personalization == nil {
			continue
		}
		value, _ := personalization.Get(path)
		if !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0 {
			values[i] = value
		}
	}
	total := float64(0)
	for _, value := range values {
		total += value
	}
	if total <= 0 {
		return uniformPersonalization(paths)
	}
	for i := range values {
		values[i] /= total
	}
	return values
}

// RankFiles computes personalized PageRank over a symbol graph.
func RankFiles(graph SymbolGraph, opts *RankFilesOptions) []RankedFile {
	paths := jscompat.LocaleSortStrings(graph.Files)
	if len(paths) == 0 {
		return []RankedFile{}
	}
	var weights *jscompat.OrderedMap[string, float64]
	if opts != nil {
		weights = opts.Personalization
	}
	personalization := personalizationVector(paths, weights)
	scores := append([]float64(nil), personalization...)

	for iteration := 0; iteration < PAGERANK_MAX_ITERATIONS; iteration++ {
		next := make([]float64, len(personalization))
		for i, value := range personalization {
			next[i] = (1 - PAGERANK_DAMPING) * value
		}
		for sourceIndex, sourcePath := range paths {
			sourceScore := scores[sourceIndex]
			sourceEdges := edgeRecord(graph, sourcePath)
			totalOutgoing := outgoingWeight(sourceEdges)
			if self, ok := recordNumber(sourceEdges, sourcePath); ok {
				totalOutgoing -= self
			}
			if totalOutgoing <= 0 {
				for targetIndex := range paths {
					next[targetIndex] += PAGERANK_DAMPING * sourceScore * personalization[targetIndex]
				}
				continue
			}
			for targetIndex, targetPath := range paths {
				weight := float64(0)
				if targetIndex != sourceIndex {
					weight, _ = recordNumber(sourceEdges, targetPath)
				}
				if weight > 0 && !math.IsNaN(weight) && !math.IsInf(weight, 0) {
					next[targetIndex] += PAGERANK_DAMPING * sourceScore * (weight / totalOutgoing)
				}
			}
		}
		delta := float64(0)
		for i, value := range next {
			delta += math.Abs(value - scores[i])
		}
		scores = next
		if delta < PAGERANK_TOLERANCE {
			break
		}
	}

	total := float64(0)
	for _, score := range scores {
		total += score
	}
	if total > 0 && !math.IsNaN(total) && !math.IsInf(total, 0) {
		for i := range scores {
			scores[i] /= total
		}
	}

	ranked := make([]RankedFile, len(paths))
	for i, path := range paths {
		ranked[i] = RankedFile{Path: path, Score: jscompat.JSNumber(scores[i])}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		left, right := float64(ranked[i].Score), float64(ranked[j].Score)
		if left == right {
			return jscompat.LocaleCompare(ranked[i].Path, ranked[j].Path) < 0
		}
		difference := right - left
		return !math.IsNaN(difference) && difference < 0
	})
	return ranked
}

// RenderRepoMap renders ranked definition sections within a UTF-16 budget.
func RenderRepoMap(graph SymbolGraph, ranked []RankedFile, budgetChars float64) string {
	budget := float64(0)
	if !math.IsNaN(budgetChars) && !math.IsInf(budgetChars, 0) {
		budget = math.Max(0, math.Floor(budgetChars))
	}
	if budget <= 0 {
		return ""
	}
	sections := []string{}
	for _, item := range ranked {
		if graph.DefsByFile == nil {
			continue
		}
		definitions, ok := graph.DefsByFile.Get(item.Path)
		if !ok {
			continue
		}
		section := formatSection(item.Path, definitions)
		candidate := section
		if len(sections) != 0 {
			candidate = strings.Join(sections, "\n") + "\n" + section
		}
		if float64(utf16Len(candidate)) <= budget {
			sections = append(sections, section)
		}
	}
	return strings.Join(sections, "\n")
}

// BuildRepoMap composes the out-of-bundle symbol graph builder with ranking
// and rendering.
func BuildRepoMap(files []SourceFile, opts *BuildRepoMapOptions, builder SymbolGraphBuilder) string {
	graph := builder.BuildSymbolGraph(files)
	personalization := buildPersonalization(graph, opts)
	ranked := RankFiles(graph, &RankFilesOptions{Personalization: personalization})
	budget := DEFAULT_BUDGET_CHARS
	if opts != nil && opts.BudgetChars != nil {
		budget = *opts.BudgetChars
	}
	return RenderRepoMap(graph, ranked, budget)
}

func formatDefinition(definition SymbolTag) string {
	return "  " + string(definition.Kind) + " " + definition.Name +
		" (line " + jscompat.FormatNumber(float64(definition.Line)) + ")"
}

func formatSection(path string, definitions []SymbolTag) string {
	ordered := append([]SymbolTag(nil), definitions...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := float64(ordered[i].Line), float64(ordered[j].Line)
		difference := left - right
		if difference != 0 && !math.IsNaN(difference) {
			return difference < 0
		}
		if cmp := jscompat.LocaleCompare(ordered[i].Name, ordered[j].Name); cmp != 0 {
			return cmp < 0
		}
		return jscompat.LocaleCompare(string(ordered[i].Kind), string(ordered[j].Kind)) < 0
	})
	lines := []string{path}
	for _, definition := range ordered {
		lines = append(lines, formatDefinition(definition))
	}
	return strings.Join(lines, "\n")
}

func buildPersonalization(graph SymbolGraph, opts *BuildRepoMapOptions) *jscompat.OrderedMap[string, float64] {
	personalization := jscompat.NewOrderedMap[string, float64]()
	if opts == nil {
		return personalization
	}
	for _, path := range opts.FocusPaths {
		if graph.DefsByFile == nil || !graph.DefsByFile.Has(path) {
			continue
		}
		value, _ := personalization.Get(path)
		personalization.Set(path, value+FOCUS_PATH_WEIGHT)
	}
	identifiers := make(map[string]bool)
	for _, identifier := range opts.FocusIdentifiers {
		identifiers[identifier] = true
	}
	if len(identifiers) != 0 {
		for _, path := range graph.Files {
			if graph.DefsByFile == nil {
				continue
			}
			definitions, _ := graph.DefsByFile.Get(path)
			found := false
			for _, definition := range definitions {
				if identifiers[definition.Name] {
					found = true
					break
				}
			}
			if found {
				value, _ := personalization.Get(path)
				personalization.Set(path, value+FOCUS_IDENTIFIER_WEIGHT)
			}
		}
	}
	return personalization
}

func edgeRecord(graph SymbolGraph, path string) *jscompat.OrderedMap[string, float64] {
	if graph.Edges == nil {
		return nil
	}
	edges, _ := graph.Edges.Get(path)
	return edges
}

func outgoingWeight(edges *jscompat.OrderedMap[string, float64]) float64 {
	total := float64(0)
	for _, entry := range objectEntries(edges) {
		weight := entry.Val
		if !math.IsNaN(weight) && !math.IsInf(weight, 0) && weight > 0 {
			total += weight
		}
	}
	return total
}

// recordNumber mirrors property access on a plain `{}` record. A missing
// Object.prototype key yields an inherited non-number rather than undefined;
// every inherited value reachable here coerces to NaN in arithmetic.
func recordNumber(record *jscompat.OrderedMap[string, float64], key string) (float64, bool) {
	if record != nil {
		if value, ok := record.Get(key); ok {
			return value, true
		}
	}
	if objectPrototypeKeys[key] {
		return math.NaN(), true
	}
	return 0, false
}

var objectPrototypeKeys = map[string]bool{
	"__defineGetter__": true, "__defineSetter__": true, "__lookupGetter__": true,
	"__lookupSetter__": true, "__proto__": true, "constructor": true,
	"hasOwnProperty": true, "isPrototypeOf": true, "propertyIsEnumerable": true,
	"toLocaleString": true, "toString": true, "valueOf": true,
}

// objectEntries applies ECMAScript ordinary-object key enumeration to an
// insertion-ordered record: canonical array-index keys first, numerically.
func objectEntries[V any](record *jscompat.OrderedMap[string, V]) []jscompat.Entry[string, V] {
	if record == nil {
		return nil
	}
	entries := record.Entries()
	indices := []jscompat.Entry[string, V]{}
	others := []jscompat.Entry[string, V]{}
	for _, entry := range entries {
		if _, ok := arrayIndex(entry.Key); ok {
			indices = append(indices, entry)
		} else {
			others = append(others, entry)
		}
	}
	sort.SliceStable(indices, func(i, j int) bool {
		left, _ := arrayIndex(indices[i].Key)
		right, _ := arrayIndex(indices[j].Key)
		return left < right
	})
	return append(indices, others...)
}

func arrayIndex(key string) (uint64, bool) {
	if key == "" {
		return 0, false
	}
	value, err := strconv.ParseUint(key, 10, 32)
	if err != nil || value == math.MaxUint32 || strconv.FormatUint(value, 10) != key {
		return 0, false
	}
	return value, true
}

func utf16Len(s string) int {
	length := 0
	for _, r := range s {
		length++
		if r >= utf8.RuneSelf && r > 0xffff {
			length++
		}
	}
	return length
}
