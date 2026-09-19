# internal/connect fixed-port investigation

## Task

Establish every internal/connect location that fixes port 8765, every path that binds it, whether non-test product code pins that port, and the smallest collision-proof way to preserve the busy-first-door fallback proof.

## Settled facts

- Tests in `internal/connect` bind `127.0.0.1:8765`.
- The anchor is `TestSlackBeginAuthUsesTheSecondDoorWhenTheFirstIsBusy` and its neighboring tests.
- Unrelated processes can hold port 8765, causing the internal/connect gate to fail or wait.

## Ruled out so far

- The heavy-suite lock is not a solution because it serializes project suites, not unrelated machine processes.
- Manufacturing an external collision or interfering with another process is not an acceptable test mechanism.

## Base-tree evidence

### Fixed selection and bind sites

- `internal/connect/auth.go:26` is the sole non-test selection: `localServerAddresses` orders `127.0.0.1:8765`, `127.0.0.1:18765`, then `127.0.0.1:0`.
- `internal/connect/auth.go:153` copies that selection into `oauth2cli.Config.LocalServerBindAddress` for ordinary browser authentication.
- For a doored plug such as Slack, `internal/connect/auth.go:162-174` restricts selection to the first two fixed addresses. `net.Listen` at `internal/connect/auth.go:163` probes each address, closes the successful probe at line 167, and selects that single address at line 174.
- `internal/connect/auth.go:199-200` starts `oauth2cli.GetToken` with the selected config. The dependency then creates the serving listener from `LocalServerBindAddress`. Thus the doored path binds twice in sequence: the product probe in this repository, then the dependency listener that serves the callback. The close between them also leaves a probe-to-bind race.
- The anchor test directly binds 8765 at `internal/connect/slack_test.go:366`, then calls the production `Manager.BeginAuth` path at line 376. Production probes and ultimately serves on 18765, asserted at lines 380 and 398.
- Two neighboring tests directly bind both fixed addresses: `internal/connect/slack_test.go:419,424` and `internal/connect/slack_test.go:447,452`. These are additional test-owned collision sites.
- `internal/connect/slack_test.go:352-353` mentions 8765 only to check URL formatting. `internal/connect/mcp_auth_test.go:354-358` mentions the registered fixed loopbacks but does not bind them.

### Reachability and product-versus-test conclusion

The fixed choice is reachable product code, not merely a test convenience. Slack registers as a product plug at `internal/connect/slack.go:34`, implements its registered door at `internal/connect/slack.go:81`, and the live chat constructs the connect manager at `cmd/codeaf/chatv3.go:1630`. A real `Manager.BeginAuth` reaches `internal/connect/auth.go:120`, selects from the product list, probes at line 163 for Slack, and launches the serving listener through `oauth2cli.GetToken` at line 200.

The product pins 8765 as its preferred fixed candidate, but it does not require that exact port. Slack can fall through to the independently registered 18765 address. Non-doored browser services can also fall through to 18765 and then an operating-system-selected port through `127.0.0.1:0`. Slack does require one of its two registered fixed ports because its vendor callback must match a pre-registered door.

### Smallest safe test direction

Changing only the anchor test to acquire real 8765 cannot make it immune to an unrelated holder. The smallest robust direction is a test-only seam for the ordered addresses and busy-first-door decision: use test-owned dynamic loopback addresses, force the first selection attempt to report busy in-process, and let the production path bind the second dynamic address. That preserves the assertion that begin-auth chooses the second door without competing for a machine-global fixed port or touching another process. The two neighboring tests that directly bind both fixed ports need the same seam to remove every internal/connect collision site.

## Ruled out by evidence

- 8765 is not test-only. `internal/connect/auth.go:26` is non-test product code.
- The product does not require 8765 specifically. It has a registered 18765 fallback, and non-doored services also have a dynamic fallback.
- A heavy-suite lock cannot protect these binds from unrelated processes.
- Probe-then-close does not reserve the address for the later serving bind.

## Implementation step

The test change will keep the production address order and binding path intact while replacing the package address list during each fixed-port collision test with test-owned loopback ports. The anchor will hold its first test address open and let `BeginAuth` choose and serve on the second test address, so the busy-first-door condition remains forced in-process. The assertions will derive the expected loopback and registered door from the second test port. The two both-busy tests will hold both test addresses. No test will bind 8765 or 18765, so an external holder of either product port cannot affect these proofs.
