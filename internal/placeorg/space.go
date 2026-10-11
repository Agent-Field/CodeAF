package placeorg

import "context"

// SpaceID names an embedding space. Vectors from two spaces are never compared,
// averaged or indexed together: a tree is built in one space, and a fallback
// space is a different tree, not a patch on the first.
type SpaceID string

const (
	// SpacePrimary is the engine's embedding model through the provider,
	// requested at PrimaryDims so ten thousand chats fit in ten megabytes.
	SpacePrimary SpaceID = "openai/text-embedding-3-small@256"
	// SpaceLocal is the static table that ships inside the binary: a
	// Model2Vec distillation (potion-base-8M, MIT) quantised to int8, with a
	// pure-Go WordPiece tokeniser. It answers in microseconds on a CPU and
	// is what the live guess and the outage path use.
	SpaceLocal SpaceID = "local/potion-base-8M@256"
	// SpaceHashed is the floor under the floor: hashed word and trigram
	// counts, no asset at all. It exists so the pipeline can never block,
	// and a tree built in it is marked provisional.
	SpaceHashed SpaceID = "local/hashed@256"
)

// PrimaryDims is the dimension asked of the provider model (its Matryoshka
// truncation) and the width of both local spaces.
const PrimaryDims = 256

// Embedder turns texts into unit vectors in one space. The provider-backed
// one is supplied by the engine, like placegraph.Asker; the local ones live in
// placeorg/localembed. An Embedder reports failure as an error and never
// substitutes another space's vectors: that choice belongs to the Vectors
// stage, which journals it.
type Embedder interface {
	Space() SpaceID
	Dims() int
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Vectors is the per-space cache of chat vectors keyed by the hash of the text
// that was embedded, so a retitled chat is embedded again and an unchanged one
// never is. It is append-only on disk and loaded whole; at ten thousand chats
// and PrimaryDims it is ten megabytes.
type Vectors interface {
	Space() SpaceID
	// Get returns the vector for a chat when its text hash matches.
	Get(chatID string, textHash string) ([]float32, bool)
	Put(chatID string, textHash string, v []float32) error
	// All returns every cached vector in Corpus order for the given ids,
	// with false for any id not cached.
	All(ids []string, hashes []string) ([][]float32, []bool)
}

// Centroid is a place's position in one space: the running sum of its
// members' vectors and the count, so adding or removing a member is O(dims)
// and a correction exemplar can weigh more than one member.
type Centroid struct {
	Sum   []float32
	Count float64
}
