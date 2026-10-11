package localembed

import (
	"context"
	"hash/fnv"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// Hashed is the floor under the floor: words and character trigrams hashed
// into the 256 dimensions with a sign, so no asset is read and no run can
// block. It finds shared vocabulary, not meaning, which is why a tree built
// in it is provisional.
type Hashed struct{}

// NewHashed returns the stateless embedder.
func NewHashed() Hashed { return Hashed{} }

// Space names the hashed space.
func (Hashed) Space() placeorg.SpaceID { return placeorg.SpaceHashed }

// Dims is the shared local width.
func (Hashed) Dims() int { return placeorg.PrimaryDims }

// Embed never fails and ignores the context.
func (h Hashed) Embed(_ context.Context, texts []string) ([][]float32, error) {
	return parallel(texts, h.one), nil
}

// trigramWeight keeps a word's own hash louder than any one of its trigrams,
// so two titles sharing a whole word are nearer than two sharing a fragment.
const trigramWeight = 0.5

func (Hashed) one(text string) []float32 {
	v := make([]float32, placeorg.PrimaryDims)
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	for _, w := range words {
		bump(v, "w:"+w, 1)
		padded := []rune("^" + w + "$")
		for i := 0; i+3 <= len(padded); i++ {
			bump(v, "t:"+string(padded[i:i+3]), trigramWeight)
		}
	}
	normalizeInPlace(v)
	return v
}

// bump adds weight to the bucket of key; one hash gives both the bucket and
// the sign, so collisions cancel as often as they add.
func bump(v []float32, key string, weight float32) {
	h := fnv.New64a()
	h.Write([]byte(key))
	sum := h.Sum64()
	if sum>>63 == 1 {
		weight = -weight
	}
	v[sum%uint64(len(v))] += weight
}
