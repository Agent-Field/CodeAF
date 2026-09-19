# Findings

This task investigates why a reconnect sometimes introduces codeaf twice when only one introduction is wanted.

Settled facts:

- `internal/connect/mcp_auth_test.go:283`, `TestTheIdentityIsUsedAgainAndSurvivesDisconnect`, failed once under box load with `codeaf introduced itself 2 times, want once`.
- The named test passes when run alone.
- Failures caused by binding fixed port 8765 are a separate investigation and are out of scope here.

Still open:

- The ordering that permits both introductions.
- Whether the behavior is caused by the test or by the product.
- The smallest correct fix.
