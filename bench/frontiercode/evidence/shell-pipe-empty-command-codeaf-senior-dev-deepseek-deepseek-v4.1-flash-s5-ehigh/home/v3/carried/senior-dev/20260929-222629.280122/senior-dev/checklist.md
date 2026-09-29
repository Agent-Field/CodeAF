[x] shell.Run returns nil (no error, no panic) when the command slice is empty
[x] every other shell.Run behaviour is unchanged (present-but-failing command still errors, env/output handling untouched)
[x] regression test for the empty command case registered as a subtest in shell_test.go
[x] go build ./... succeeds
[x] go test ./internal/shell/ passes
[x] diff is lint-clean and limited to internal/shell and its test
