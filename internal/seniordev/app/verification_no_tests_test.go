//go:build !windows

package app

import "testing"

// The discovery can select these runners directly or through a project
// command. Their empty-suite reports never count as a verified test run.
func TestEveryDiscoveredRunnerReportsAnEmptySuite(t *testing.T) {
	cases := []struct {
		name, output string
		empty        bool
	}{
		{"unittest", "Ran 0 tests in 0.001s\nOK", true},
		{"pytest", "no tests ran in 0.01s", true},
		{"go", "?   example.test/pkg [no test files]\n", true},
		{"vitest", "No test files found, exiting with code 1", true},
		{"jest", "No tests found, exiting with code 1", true},
		{"cargo", "running 0 tests\ntest result: ok. 0 passed", true},
		{"maven", "Tests run: 0, Failures: 0", true},
		{"maven no tests", "No tests to run.", true},
		{"gradle", "> Task :test NO-SOURCE", true},
		{"ctest", "No tests were found!!!", true},
		{"dotnet", "No test is available in assembly", true},
		{"go mixed", "?   example.test/empty [no test files]\nok  example.test/tested 0.01s", false},
		{"unittest folder missed", "Ran 0 tests in 0.001s\nRan 1 test in 0.001s", true},
		{"cargo mixed", "running 0 tests\nrunning 2 tests", false},
		{"maven mixed", "Tests run: 0\nTests run: 3", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := noTestsReported(tc.output); got != tc.empty {
				t.Fatalf("noTestsReported(%q) = %v, want %v", tc.output, got, tc.empty)
			}
		})
	}
}
