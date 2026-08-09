package leafbriefing

import (
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/importgraph"
	"github.com/Agent-Field/swe-pro-go/internal/session/repomap"
	"github.com/Agent-Field/swe-pro-go/internal/session/symbolgraph"
)

type defaultRepoMapBuilder struct{}

// NewDefaultBuilder composes the ported repo-map and symbol-graph pipeline
// with leaf briefing's real workspace graph loader.
func NewDefaultBuilder() *Builder {
	return NewBuilder(defaultRepoMapBuilder{}, nil)
}

func (defaultRepoMapBuilder) BuildRepoMap(
	files []importgraph.SourceFile, opts RepoMapOptions,
) string {
	sources := make([]repomap.SourceFile, len(files))
	for index, file := range files {
		sources[index] = repomap.SourceFile{Path: file.Path, Content: file.Content}
	}
	budget := opts.BudgetChars
	return repomap.BuildRepoMap(sources, &repomap.BuildRepoMapOptions{
		BudgetChars: &budget, FocusPaths: opts.FocusPaths,
	}, repomap.SymbolGraphBuilderFunc(buildRepoMapSymbolGraph))
}

func buildRepoMapSymbolGraph(files []repomap.SourceFile) repomap.SymbolGraph {
	sources := make([]symbolgraph.SourceFile, len(files))
	for index, file := range files {
		sources[index] = symbolgraph.SourceFile{Path: file.Path, Content: file.Content}
	}
	graph := symbolgraph.BuildSymbolGraph(sources)
	definitions := jscompat.NewOrderedMap[string, []repomap.SymbolTag]()
	if graph.DefsByFile != nil {
		for _, path := range graph.DefsByFile.Keys() {
			items, ok := graph.DefsByFile.Get(path)
			if !ok {
				continue
			}
			converted := make([]repomap.SymbolTag, len(items))
			for index, item := range items {
				converted[index] = repomap.SymbolTag{
					Name: item.Name, Line: jscompat.JSNumber(item.Line), Kind: repomap.SymbolKind(item.Kind),
				}
			}
			definitions.Set(path, converted)
		}
	}
	edges := jscompat.NewOrderedMap[string, *jscompat.OrderedMap[string, float64]]()
	if graph.Edges != nil {
		for _, path := range graph.Edges.Keys() {
			items, ok := graph.Edges.Get(path)
			if !ok {
				continue
			}
			converted := jscompat.NewOrderedMap[string, float64]()
			for _, target := range items.Keys() {
				weight, defined := items.Get(target)
				if defined {
					converted.Set(target, weight)
				}
			}
			edges.Set(path, converted)
		}
	}
	return repomap.SymbolGraph{
		Files: append([]string(nil), graph.Files...), DefsByFile: definitions, Edges: edges,
	}
}
