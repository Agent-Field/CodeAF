package wsdiscover

import (
	"encoding/binary"
	"math"
)

func packVector(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	out := make([]byte, len(v)*4)
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(x))
	}
	return out
}

func unpackVector(b []byte) []float32 {
	if len(b) < 4 || len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

// cosine is brute-force similarity. Matching model/version/dimension is the
// caller's law; this only scores two equal-length vectors.
func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func sameEmbedding(p Passage, model, version string, dim int) bool {
	return p.Model != "" && p.Model == model && p.Version == version && p.Dimension == dim && dim == len(p.Vector)
}

func usableVectors(vecs [][]float32, n, dim int) bool {
	if n == 0 || dim < 1 || len(vecs) != n {
		return false
	}
	for _, v := range vecs {
		if len(v) != dim {
			return false
		}
	}
	return true
}
