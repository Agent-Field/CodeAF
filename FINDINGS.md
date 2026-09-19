# Findings

This task investigates why a reconnect sometimes introduces codeaf twice when only one introduction is wanted.

## Established from the base tree

There is one product call site that issues an introduction. `Manager.BeginAuth` selects the tool-server path at `internal/connect/auth.go:120-130`. Each call starts `connectToolServer` in a goroutine at `internal/connect/mcp_auth.go:325-333`. That goroutine reads the saved registration at `internal/connect/mcp_auth.go:368` and calls `introduce` when none is held or it does not fit at `internal/connect/mcp_auth.go:369-370`. The actual dynamic registration request is issued by `oauthex.RegisterClient` at `internal/connect/mcp_auth.go:470`. Therefore the two introduction paths are two executions of this same call chain, one from the initial `BeginAuth` and one from the reconnecting `BeginAuth`.

The ordinary settled ordering is:

1. The initial connection finds no registration at `mcp_auth.go:368`, introduces at `mcp_auth.go:370`, and only after the browser and token exchange persists the identity at `mcp_auth.go:414-418`.
2. The test helper waits for that whole flow at `internal/connect/mcp_auth_test.go:373-388`.
3. `Manager.Disconnect` removes only account keys at `internal/connect/connect.go:363-379`. It does not remove the registration.
4. The reconnect reads the registration at `mcp_auth.go:368`. If `mcpRegistration.fits` succeeds at `internal/connect/mcp_registration.go:94-99`, it skips the sole introduction call site.

Two candidate orderings can make both executions introduce:

1. Redirect mismatch on reconnect. Listener candidates are ordered `127.0.0.1:8765`, `127.0.0.1:18765`, then an ephemeral port at `internal/connect/auth.go:17-26`, and `listener.New` chooses from that order at `internal/connect/mcp_auth.go:295`. `loopbacks` records the in-use redirect and fixed candidates, but explicitly skips candidate port `0` at `mcp_auth.go:500-509`. If both fixed candidates are unavailable, the initial flow records ephemeral redirect A. After disconnect, the reconnect can receive ephemeral redirect B. `fits` requires the new redirect to be in the old record at `mcp_registration.go:94-99`, so B rejects the identity and the reconnect calls `introduce` again at `mcp_auth.go:369-370`. This is a product path reachable by a real reconnect, not an extra observation counted by the test. The fake increments only when its `/register` handler receives a request at `internal/connect/mcp_fake_test.go:143-155`. Candidate priority is fixed 8765, fixed 18765, then ephemeral; no fixed-port behavior has been changed.
2. Overlapping flows. Two `BeginAuth` calls can both read no usable registration at `mcp_auth.go:368` before either reaches the delayed persistence at `mcp_auth.go:417`, so both call `introduce`. The store mutex protects each individual `get` and `put` at `internal/connect/mcp_registration.go:184-198`, but does not make the read, introduction, and write one operation. This is also a product ordering, but it is ruled out for the named sequential test because `connectFake` waits for the first `Flow.Wait` before `Disconnect` and the second `BeginAuth` (`mcp_auth_test.go:263-280` and `mcp_auth_test.go:373-388`).

## Ruled out

- There is no second introduction implementation in `internal/connect`; the only production call to `introduce` is `mcp_auth.go:370`, and its only registration request is `mcp_auth.go:470`.
- `Disconnect` does not delete or rewrite the MCP registration (`connect.go:363-379`).
- Opening tools cannot introduce an identity. It only reads and validates the registration at `internal/connect/mcp_tools.go:150-168`.
- The fake counter does not count discovery, authorization, token exchange, or a test-side observation. It increments only in the registration endpoint handler (`mcp_fake_test.go:143-155`).
- A settled reconnect on the same registered redirect does not introduce because `fits` succeeds and bypasses `mcp_auth.go:370`.
- The separate fixed-port bind-error failure is not investigated here, and no port or listener behavior was modified.

## Smallest-change candidates for the next step

For the deterministic redirect-mismatch ordering, force two distinct redirects at the seam and then either avoid issuing the second introduction by reusing an allowed stable redirect, or change the test to wait only if evidence shows unsettled state. The base-tree evidence favors a product fix because the second request is real and `Flow.Wait` already settles persistence. For the overlapping-flow ordering, the product-sized fix would serialize registration acquisition per service and recheck after acquiring the serialization point.

## Forced-ordering regression step

The deterministic seam is the package-level listener candidate list at `internal/connect/auth.go:17-26`, before `listener.New` chooses a redirect in `internal/connect/mcp_auth.go:295`. The regression test will temporarily offer only `127.0.0.1:0`, forcing the initial connection and the sequential reconnect to receive distinct ephemeral redirects. That ordering makes `mcpRegistration.fits` reject the saved identity and drives the same production introduction call at `internal/connect/mcp_auth.go:369-370` a second time. This changes no fixed-port behavior, uses no load or busy loop, and directly distinguishes a product request from a test-side counting artifact because the fake counter advances only in its registration handler.

The forced test now offers only an ephemeral listener candidate, completes and persists the first identity, then holds that first assigned address before reconnecting. The reconnect must therefore receive a different redirect. The focused command `go test ./internal/connect -run '^TestTheIdentityIsUsedAgainAndSurvivesDisconnect$' -count=1` fails deterministically with `codeaf introduced itself 2 times, want once`. This proves the sequential product reconnect issues the second registration request. It rules out an unsettled first flow, an overlapping `BeginAuth`, a test-side extra observation, machine load, and either fixed port.

A five-run check exposed that claiming the first address after `Flow.Wait` races the loopback server's asynchronous shutdown: one run still held the address while four produced the intended exact count. The address-claim barrier is therefore rejected as nondeterministic. The next revision will inject already-open listeners at the existing `listener.New` seam, which controls the two redirects without timing, retries, load, or fixed ports.

The revised hook substitutes `listener.New` only in the test and directs the two sequential `BeginAuth` calls to two preselected ephemeral addresses. Five focused runs all failed at the count assertion with exactly `codeaf introduced itself 2 times, want once`; none failed at listener setup. This is the accepted deterministic regression seam.

## Cause decision and selected fix

The forced ordering proves this is a product path. After the first settled flow persists its registration, a reconnect can bind a different ephemeral redirect. `mcpRegistration.fits` then correctly rejects that redirect because the service was never told about it, and `connectToolServer` issues a real second dynamic registration request. The test counts only that request.

The smallest product change is to make reconnect listener selection try the saved registration's loopback redirect before the ordinary candidates. When that prior address is available, the reconnect uses the redirect already registered and `fits` skips introduction. This is reachable outside the test whenever a prior connection used a fallback ephemeral listener, disconnected, and later reconnects after that listener has closed. The fixed listener candidates and their ordering will remain unchanged as the fallback path.
