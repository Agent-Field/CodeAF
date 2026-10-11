// Package dedup is stage 1 of the Places organiser: it folds near-identical
// chats (a retried brief, a pasted preamble sent again) into one group and
// names the earliest chat as the group's representative.
//
// The method is the reference harness's: MinHash signatures over word
// shingles, banded locality-sensitive hashing tuned to a Jaccard threshold,
// and union-find over every pair that shares a band. Like the reference, a
// shared band is taken as proof of similarity; no pair is re-verified, because
// the bands are already tuned so a share means "very likely above threshold".
//
// This stage is a rule stage: it takes no Judge and opens no door.
package dedup

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"math"
	"math/bits"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

const (
	// Permutations is the signature length. Fixed by the reference algorithm.
	Permutations = 128
	// ShingleWords is how many consecutive words make one shingle.
	ShingleWords = 5
	// Threshold is the Jaccard similarity above which two chats fold.
	Threshold = 0.82
	// DefaultSeed makes a run reproducible; the reference harness also fixes its seed.
	DefaultSeed = 42
)

// prime is the Mersenne prime 2^61-1 the permutations work modulo.
const prime = (1 << 61) - 1

// stoplist is the reference harness's function words. They carry no subject,
// so two briefs that differ only in them are still the same brief.
var stoplist = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields("the a an and or of to in for on with is are was were be been it its this that these those you your my me i we our") {
		m[w] = true
	}
	return m
}()

// Deduper implements placeorg.Deduper.
type Deduper struct {
	a, b        [Permutations]uint64
	bands, rows int
}

var _ placeorg.Deduper = (*Deduper)(nil)

// New builds a Deduper whose permutations derive from seed alone.
func New(seed uint64) *Deduper {
	d := &Deduper{}
	state := seed
	for i := range d.a {
		d.a[i] = splitmix(&state)%(prime-1) + 1 // a must be non-zero
		d.b[i] = splitmix(&state) % prime
	}
	d.bands, d.rows = optimalBands(Threshold, Permutations)
	return d
}

// Dedup returns, for every chat that folds into an earlier one, the ID of the
// group's representative: the chat with the earliest Created, ties broken by
// input order. Representatives and unique chats do not appear in the map.
func (d *Deduper) Dedup(ctx context.Context, chats []placeorg.Chat) (map[string]string, error) {
	parent := make([]int, len(chats))
	for i := range parent {
		parent[i] = i
	}
	find := func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}

	// buckets maps one band's bytes to the first chat seen with them, so each
	// later chat unions with it: the same components as unioning every pair.
	buckets := make(map[string]int, len(chats)*d.bands)
	key := make([]byte, 0, 2+d.rows*4)
	for i, c := range chats {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		sig := d.signature(shingles(c.Text))
		for band := 0; band < d.bands; band++ {
			key = append(key[:0], byte(band), byte(band>>8))
			for _, v := range sig[band*d.rows : (band+1)*d.rows] {
				key = binary.LittleEndian.AppendUint32(key, v)
			}
			if first, ok := buckets[string(key)]; ok {
				parent[find(i)] = find(first)
			} else {
				buckets[string(key)] = i
			}
		}
	}

	earliest := map[int]int{}
	for i := range chats {
		root := find(i)
		if e, ok := earliest[root]; !ok || chats[i].Created.Before(chats[e].Created) {
			earliest[root] = i
		}
	}
	out := map[string]string{}
	for i, c := range chats {
		if rep := earliest[find(i)]; rep != i {
			out[c.ID] = chats[rep].ID
		}
	}
	return out, nil
}

// signature is the MinHash of a shingle set: per permutation, the smallest
// permuted 32-bit hash over the set.
func (d *Deduper) signature(set map[string]struct{}) [Permutations]uint32 {
	var sig [Permutations]uint32
	for i := range sig {
		sig[i] = math.MaxUint32
	}
	for s := range set {
		sum := sha1.Sum([]byte(s))
		h := uint64(binary.LittleEndian.Uint32(sum[:4]))
		for i := range sig {
			if v := uint32(permute(d.a[i], h, d.b[i])); v < sig[i] {
				sig[i] = v
			}
		}
	}
	return sig
}

// shingles lower-cases text, keeps [a-z0-9#] runs minus the stoplist, and
// returns every window of ShingleWords words. A text shorter than one window
// yields its single joined shingle, and an empty text yields {""}, as in the
// reference, so empty chats fold with each other.
func shingles(text string) map[string]struct{} {
	var words []string
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '#')
	}) {
		if !stoplist[w] {
			words = append(words, w)
		}
	}
	n := max(len(words)-ShingleWords+1, 1)
	out := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		end := min(i+ShingleWords, len(words))
		out[strings.Join(words[i:end], " ")] = struct{}{}
	}
	return out
}

// permute returns (a*x + b) mod 2^61-1 without overflow, reducing the 128-bit
// product with the Mersenne identity 2^61 = 1.
func permute(a, x, b uint64) uint64 {
	hi, lo := bits.Mul64(a, x)
	r := (lo & prime) + (lo >> 61) + (hi << 3)
	r = (r & prime) + (r >> 61)
	r += b
	r = (r & prime) + (r >> 61)
	if r >= prime {
		r -= prime
	}
	return r
}

// optimalBands picks (bands, rows) with bands*rows <= perms that minimise the
// summed false-positive and false-negative probability mass around the
// threshold, weighting both equally: the choice datasketch makes for the
// reference run.
func optimalBands(threshold float64, perms int) (bands, rows int) {
	best := math.MaxFloat64
	for b := 1; b <= perms; b++ {
		for r := 1; r <= perms/b; r++ {
			hit := func(s float64) float64 { return 1 - math.Pow(1-math.Pow(s, float64(r)), float64(b)) }
			miss := func(s float64) float64 { return 1 - hit(s) }
			if e := 0.5*integrate(hit, 0, threshold) + 0.5*integrate(miss, threshold, 1); e < best {
				best, bands, rows = e, b, r
			}
		}
	}
	return bands, rows
}

// integrate is a midpoint rule with fixed resolution; accuracy beyond that is
// not needed to rank band shapes.
func integrate(f func(float64) float64, lo, hi float64) float64 {
	const steps = 200
	w := (hi - lo) / steps
	var sum float64
	for i := 0; i < steps; i++ {
		sum += f(lo+(float64(i)+0.5)*w) * w
	}
	return sum
}

// splitmix is a small seeded generator, so permutations never depend on a
// library's version.
func splitmix(s *uint64) uint64 {
	*s += 0x9e3779b97f4a7c15
	z := *s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}
