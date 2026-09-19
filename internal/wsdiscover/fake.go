package wsdiscover

import (
	"context"
	"hash/fnv"
	"math"
)

// FakeEmbedder is a deterministic Embedder for tests and other lanes' fixtures.
// Open never binds it. Production wiring uses the provider adapter or leaves
// discovery delayed; this type must not become a silent success path.
type FakeEmbedder struct {
	Model   string
	Version string
	Dim     int
	Down    bool
	Err     error
}

func (f *FakeEmbedder) identity() (model, version string, dim int) {
	model, version, dim = f.Model, f.Version, f.Dim
	if model == "" {
		model = "fake"
	}
	if version == "" {
		version = "test"
	}
	if dim < 1 {
		dim = 4
	}
	return model, version, dim
}

func (f *FakeEmbedder) Available(context.Context) (string, bool, error) {
	model, _, _ := f.identity()
	if f.Err != nil {
		return model, false, f.Err
	}
	if f.Down {
		return model, false, nil
	}
	return model, true, nil
}

func (f *FakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, string, string, int, error) {
	model, version, dim := f.identity()
	if f.Err != nil {
		return nil, "", "", 0, f.Err
	}
	if f.Down {
		return nil, model, version, 0, nil
	}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = fakeVector(text, dim)
	}
	return out, model, version, dim, nil
}

func fakeVector(text string, dim int) []float32 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	seed := h.Sum64()
	v := make([]float32, dim)
	var norm float64
	for i := range v {
		seed = seed*6364136223846793005 + 1
		x := float32(int64(seed>>33)%2001-1000) / 1000
		v[i] = x
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		v[0] = 1
		return v
	}
	scale := float32(1 / math.Sqrt(norm))
	for i := range v {
		v[i] *= scale
	}
	return v
}
