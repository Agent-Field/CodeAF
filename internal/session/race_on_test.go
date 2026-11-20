//go:build race

package session

// raceInstrumented reports that this test binary was built with -race. The
// detector slows memory-heavy work several times over, so a test asserting a
// product's wall-clock contract says which build it is measuring.
const raceInstrumented = true
