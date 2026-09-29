[x] Empty pipe command list is skipped without panicking and without returning an error in internal/shell.Run
[x] Non-empty command that fails still produces its error
[x] Environment and output handling untouched
[x] Regression test for the empty command case added following existing table/subtest conventions
[x] `go build ./...` succeeds
[x] `go test ./internal/shell/` passes
[x] Diff is lint/fmt clean and focused on internal/shell
