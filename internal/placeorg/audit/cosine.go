package audit

import "math"

// cosine is the similarity of two vectors. A missing or zero vector is -1,
// so it never clears splitFloor and never pulls two places together.
// Vectors are not assumed to be unit length: the reference embeddings are,
// and a test fixture often is not.
func cosine(a, b []float32) float32 {
	if len(a) == 0 || len(b) == 0 {
		return -1
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return -1
	}
	return float32(dot / math.Sqrt(na*nb))
}

// centroid is the mean of the vectors it is given, zeros and gaps skipped.
// cosine normalises, so the mean is not normalised here.
func centroid(vecs [][]float32) []float32 {
	var sum []float32
	n := 0
	for _, v := range vecs {
		if len(v) == 0 {
			continue
		}
		if sum == nil {
			sum = make([]float32, len(v))
		}
		m := len(sum)
		if len(v) < m {
			m = len(v)
		}
		for i := 0; i < m; i++ {
			sum[i] += v[i]
		}
		n++
	}
	if n == 0 {
		return nil
	}
	scale := float32(n)
	for i := range sum {
		sum[i] /= scale
	}
	return sum
}
