//go:build !race

package session

// raceInstrumented reports that this test binary was built with -race; see
// race_on_test.go.
const raceInstrumented = false
