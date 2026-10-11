// Package localembed holds the two embedders that need no network: Static, a
// Model2Vec table inside the binary, and Hashed, which needs nothing at all.
// Both answer in the same 256 dimensions as the provider space, and both are
// deterministic, so the same text is the same vector on every machine.
package localembed

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// potionBin is potion-base-8M (MIT) distilled by Minish Lab: an int8 table
// with one scale per row, then the vocabulary. testdata/make_assets.py
// documents the layout and rebuilds it. The size law pins it at 10 MB.
//
//go:embed potion.bin
var potionBin []byte

// maxTokens is model2vec's own default cut: longer texts embed their first
// maxTokens tokens only.
const maxTokens = 512

// Static embeds with the Model2Vec table: tokenise, average the rows of the
// tokens, L2-normalise. An empty or entirely unknown text is the zero vector.
type Static struct {
	rows, dims int
	table      []int8
	scale      []float32
	tok        *wordPiece
}

var (
	staticOnce sync.Once
	staticVal  *Static
	staticErr  error
)

// NewStatic decodes the embedded asset once and shares it, because the table
// is read-only and eight megabytes.
func NewStatic() (*Static, error) {
	staticOnce.Do(func() { staticVal, staticErr = decode(potionBin) })
	return staticVal, staticErr
}

func decode(b []byte) (*Static, error) {
	if len(b) < 12 || string(b[:4]) != "PTN1" {
		return nil, fmt.Errorf("localembed: asset is not a potion table")
	}
	rows := int(binary.LittleEndian.Uint32(b[4:]))
	dims := int(binary.LittleEndian.Uint32(b[8:]))
	tableEnd := 12 + rows*dims
	scaleEnd := tableEnd + rows*4
	if dims != placeorg.PrimaryDims || scaleEnd > len(b) {
		return nil, fmt.Errorf("localembed: asset has %d rows of %d dims in %d bytes", rows, dims, len(b))
	}
	table := make([]int8, rows*dims)
	for i, v := range b[12:tableEnd] {
		table[i] = int8(v)
	}
	scale := make([]float32, rows)
	for i := range scale {
		scale[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[tableEnd+4*i:]))
	}
	tokens := strings.Split(string(bytes.TrimSpace(b[scaleEnd:])), "\n")
	if len(tokens) != rows {
		return nil, fmt.Errorf("localembed: %d tokens for %d rows", len(tokens), rows)
	}
	return &Static{rows: rows, dims: dims, table: table, scale: scale, tok: newWordPiece(tokens)}, nil
}

// Space names the local space.
func (s *Static) Space() placeorg.SpaceID { return placeorg.SpaceLocal }

// Dims is the table width.
func (s *Static) Dims() int { return s.dims }

// Embed never fails and ignores the context: a batch is microseconds of CPU.
func (s *Static) Embed(_ context.Context, texts []string) ([][]float32, error) {
	return parallel(texts, s.one), nil
}

// Tokens exposes the ids for the parity test against Python.
func (s *Static) Tokens(text string) []int32 { return s.tok.Encode(text) }

func (s *Static) one(text string) []float32 {
	ids := s.tok.Encode(text)
	if len(ids) > maxTokens {
		ids = ids[:maxTokens]
	}
	v := make([]float32, s.dims)
	for _, id := range ids {
		row, sc := s.table[int(id)*s.dims:int(id+1)*s.dims], s.scale[id]
		for d, q := range row {
			v[d] += float32(q) * sc
		}
	}
	normalizeInPlace(v)
	return v
}

// normalizeInPlace scales v to unit length; a zero vector stays zero.
func normalizeInPlace(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}

// parallel maps one over texts on every core, because the work is pure CPU on
// independent texts and a large first run should not wait on a single thread.
func parallel(texts []string, one func(string) []float32) [][]float32 {
	out := make([][]float32, len(texts))
	workers := runtime.GOMAXPROCS(0)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := w; i < len(texts); i += workers {
				out[i] = one(texts[i])
			}
		}()
	}
	wg.Wait()
	return out
}
