[x] shell.Run skips an empty command list instead of panicking on command[0]
[x] shell.Run returns no error for an empty command list
[x] A non-empty command that fails still returns its error unchanged
[x] Environment and output handling are untouched
[x] Regression test for the empty command case registered as a subtest of TestRunCommand
[x] go build ./... succeeds
[x] go test ./internal/shell/ passes
