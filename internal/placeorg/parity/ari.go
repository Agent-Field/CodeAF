package parity

// AdjustedRand is the adjusted Rand index of two labellings of the same
// items, in the same order. One means the partitions agree up to renaming.
// The chance correction is what makes two unrelated partitions score near
// zero instead of looking alike merely because both have a large group.
func AdjustedRand(a, b []string) float64 {
	n := len(a)
	if n != len(b) || n < 2 {
		return 0
	}
	pair := map[[2]string]int{}
	rows, cols := map[string]int{}, map[string]int{}
	for i := range a {
		pair[[2]string{a[i], b[i]}]++
		rows[a[i]]++
		cols[b[i]]++
	}
	comb := func(x int) float64 { return float64(x*(x-1)) / 2 }
	var sumPair, sumRow, sumCol float64
	for _, v := range pair {
		sumPair += comb(v)
	}
	for _, v := range rows {
		sumRow += comb(v)
	}
	for _, v := range cols {
		sumCol += comb(v)
	}
	total := comb(n)
	if total == 0 {
		return 1
	}
	expected := sumRow * sumCol / total
	max := (sumRow + sumCol) / 2
	if max == expected {
		return 1
	}
	return (sumPair - expected) / (max - expected)
}
