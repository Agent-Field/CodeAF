package raftcore

// splitMix64 is specified here rather than delegated to math/rand, whose
// implementation is not our wire-version contract. Addition and products wrap
// modulo 2^64. Rejection sampling avoids biased election choices. This generator
// is NEVER used for keys, certificates, request IDs or other cryptography.
type splitMix64 struct{ state, draws uint64 }

func (r *splitMix64) next() uint64 {
	r.state += 0x9e3779b97f4a7c15
	r.draws++
	z := r.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func (r *splitMix64) bounded(n int) int {
	if n <= 0 {
		panic("raftcore: nonpositive random bound")
	}
	b := uint64(n)
	threshold := -b % b
	for {
		v := r.next()
		if v >= threshold {
			return int(v % b)
		}
	}
}
