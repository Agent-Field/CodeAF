[x] Run skips an empty command slice without panicking and returns nil
[x] A present-but-failing command still returns its error
[x] Environment and output handling are unchanged
[x] Regression test for the empty case registered as a subtest in internal/shell
[x] `go build ./...` succeeds
[x] `go test ./internal/shell/` passes
[x] Diff is lint-clean and focused on internal/shell and its test
