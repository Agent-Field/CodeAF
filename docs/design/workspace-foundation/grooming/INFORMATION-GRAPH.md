# Information structure, before choosing a view

2026-09-09. The person rejected the box-heavy year-one map as unhelpful and asked
for the simple mathematical structure of everything accumulated or learned, with
a way to inspect and zoom into it. [INFORMATION-GRAPH.svg](INFORMATION-GRAPH.svg)
is the replacement explanatory study. The prior drawings are retained as history,
not accepted layouts.

A useful conceptual description is a typed directed graph G = (V, E). V contains
existing records: folders, conversations, work, runs, artifacts, decisions and
remembered information. E contains relationships with explicit meanings, such as
membership, source, applicability, execution, production and revision. This does
not introduce new product primitives, prescribe a graph database, or claim that
all of these semantics are already implemented.

The folder membership portion gives organization. Source and applicability links
explain why information is known and where it matters. Work, execution and output
links explain what happened. Different relationship types must not be flattened
into one generic connection: contains does not imply authorizes; source does not
imply accepted instruction. Collections already permit multiple parents, so even
membership is more general than a strict folder tree.

Zoom should mean choosing less aggregation or a smaller relevant subgraph:
whole-workspace groups, a selected topic's records and connections, then one
record's exact content, sources, applicable scope and revisions. Physical zoom
and clicking are optional interface choices. A folder browser, list or graph can
show the same selected records. No requirement follows to draw every transcript
message or historic link simultaneously.

For inspecting what Forge knows, distinguish explicit user decisions and facts
from uncertain inferred memory, and current state from superseded history. A
complete inspection experience should expose retained sources and corrections;
it cannot promise access to a model's private reasoning or information never
stored. Exact retention, forgetting and historical availability remain open.

Current implementation is partial: collections, owner resolution and draft
sourced shared-context revisions exist. Automatic decision capture, complete
inferred-memory provenance, unified knowledge inspection and a zoomable global
map are not established. The graph is an explanatory abstraction of the intended
system, not a report that one universal knowledge graph already exists.

Next: agree whether this distinction between records, typed relationships and
zoomed subsets matches the person's question; then inspect the minimum record
information needed to make learned knowledge understandable and correctable.
No new capability or layout is confirmed by this study.
