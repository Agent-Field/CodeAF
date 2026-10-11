package placeorg

import "context"

// One interface per stage, in pipeline order. A stage that holds a Judge is
// an AI stage and must also implement Judged; the structural test checks it.
// Rule stages take no Judge and cannot reach one. Every stage returns what it
// computed and, where it changes the tree, the Ops it proposes; none of them
// mutates the Tree or the Corpus it is handed.

// Deduper folds near-identical chats (MinHash over word shingles, LSH bands,
// union-find) and names a representative per group: the earliest chat. The
// others are KindDuplicate and are filed beside the representative later.
type Deduper interface {
	Dedup(ctx context.Context, chats []Chat) (representative map[string]string, err error)
}

// Kinder sorts chats by kind before anything semantic happens. It reads
// structure (length, reply count, repetition, a canned opening) and never
// a list of subjects, so it cannot be tuned to one person's library.
type Kinder interface {
	Kinds(ctx context.Context, chats []Chat, representative map[string]string) (map[string]Kind, error)
}

// Anchorer derives, per chat, the things it is about in the person's own
// terms: issue and pull-request references, the workspace family and repo
// key, high-IDF proper tokens that recur across chats, and the names of
// places the person made or confirmed. It replaces any fixed list of
// subjects; the weights are inverse document frequencies over the corpus.
type Anchorer interface {
	Anchors(ctx context.Context, chats []Chat, confirmedNames []string) (map[string]AnchorSet, error)
}

// AnchorSet is one chat's anchors with their weights.
type AnchorSet map[string]float32

// Vectorizer embeds the corpus in one space, through the cache, and reports
// the space it ended in. It tries the primary Embedder and, when that fails
// for any batch, ABANDONS the primary space for this run and embeds
// everything in the local one, because a tree must live in one space. The
// fall is journaled; the Plan is marked provisional.
type Vectorizer interface {
	Vectorize(ctx context.Context, corpus *Corpus, primary, local Embedder) (space SpaceID, vectors [][]float32, fell bool, err error)
}

// Neighbours builds the kNN graph: for every chat its K nearest by cosine,
// an edge only above Floor, weighted by similarity plus the IDF-weighted
// overlap of the two chats' anchors. Memory is O(n·K); the search is blocked
// and parallel, exact, with no index library.
type Neighbours interface {
	KNN(ctx context.Context, vectors [][]float32, anchors map[string]AnchorSet, ids []string) (Graph, error)
}

// Graph is a sparse weighted undirected graph over chat indices.
type Graph struct {
	N     int
	Edges []Edge
}

// Edge is one weighted pair with I < J.
type Edge struct {
	I, J int
	W    float32
}

// Communities partitions the graph by seeded consensus Leiden at one
// resolution: Runs partitions with seeds Seed+i, a sparse co-association
// counted only over the graph's own edges, then one final partition over the
// pairs that agreed in at least Agree of the runs.
type Communities interface {
	Partition(ctx context.Context, g Graph, resolution float64, seed uint64) (labels []int, err error)
}

// Nester nests three partitions (fine, mid, coarse) into a forest by maximal
// overlap, so every fine community has exactly one mid parent and every mid
// exactly one coarse parent. It returns the tree with members and depth set
// and names empty.
type Nester interface {
	Nest(ctx context.Context, corpus *Corpus, fine, mid, coarse []int) (*Tree, error)
}

// Auditor is the op algebra that makes the tree right-sized: collapse a
// dominant child, dissolve below quorum bottom-up, interpose a parent under
// fan-out pressure behind the mega-bucket and separability guards, demote a
// group that is an activity rather than a thing, and split a leaf while
// destination children emerge. Only the demote question needs a Judge: when
// no anchor discriminates, the facet vote asks THING or ACTIVITY, and its
// fallback is KEEP (an unjudged place stays a place; it is never demoted on
// silence).
type Auditor interface {
	Judged
	Audit(ctx context.Context, t *Tree, corpus *Corpus, vectors [][]float32, judge Judge) ([]Op, error)
}

// Parker places every chat by hierarchical Chow: descend only while a child
// beats its parent by Margin; below Floor the chat goes to Now. Parked chats
// are citizens: each node's centroid is recomputed over members and parked
// chats. The thresholds are calibrated per space from the quantiles of the
// kNN similarities, never copied between spaces.
type Parker interface {
	Park(ctx context.Context, t *Tree, corpus *Corpus, vectors [][]float32, space SpaceID) ([]Op, error)
}

// Namer names every place: c-TF-IDF candidates (three or more letters,
// supported by two or more members, informative by IDF), a charter line for
// each top-level place, then a Judge pick held to one or two words and
// tree-wide uniqueness. Fallback: the first candidate not yet taken.
type Namer interface {
	Judged
	Name(ctx context.Context, t *Tree, corpus *Corpus, judge Judge) ([]Op, error)
}

// Reviewer reads the whole outline once as a picky librarian. Renames that
// pass the name rules become OpRename; merges and misplacements become
// proposals only. Fallback: no change.
type Reviewer interface {
	Judged
	Review(ctx context.Context, t *Tree, corpus *Corpus, judge Judge) ([]Op, error)
}

// Linker adds shortcuts for the multi-home minority (a second place within
// Delta of the best and above Floor, uncapped, one per branch) and lenses: demoted
// groups, by-month on any place with enough chats, and steering axes.
type Linker interface {
	Link(ctx context.Context, t *Tree, corpus *Corpus, vectors [][]float32) ([]Op, error)
}

// Stages is the registry: one field per stage, filled by the engine's
// wiring, read by the Runner. A nil field is a build error the Runner
// reports before it starts, not a path it silently skips.
type Stages struct {
	Dedup       Deduper
	Kind        Kinder
	Anchors     Anchorer
	Vectors     Vectorizer
	Neighbours  Neighbours
	Communities Communities
	Nest        Nester
	Audit       Auditor
	Park        Parker
	Name        Namer
	Review      Reviewer
	Link        Linker
	Stream      Filer
	Steer       Steerer
	Correct     Corrector
}
