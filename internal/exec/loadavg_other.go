//go:build !darwin && !linux

package exec

// A platform with no cheap load reading reports nothing rather than guessing.
// The governor treats silence as "no evidence of pressure" and never gates, so
// an unsupported host behaves exactly as it did before the governor existed.
func loadAverageOne() (float64, bool) { return 0, false }
