# codeaf-probe — coding agent guide

One page. Verbs, one example journey, error codes, fixture reset. Every
response is compact JSON on stdout.

## Verbs

| Verb | What it does |
| --- | --- |
| `codeaf-probe prepare [--bin PATH]` | Build (or reuse) the CodeAF binary; pins immutable build identity (sha, dirty flag, go version, flags). |
| `codeaf-probe start --fixture NAME [--session ID]` | Launch an isolated persistent terminal session on a fixture. tmux renders the screen; sessions survive across CLI calls. |
| `codeaf-probe observe --session ID [--diff-from REV]` | Rendered screen snapshot with cursor + process status, and a revision counter `rev`. |
| `codeaf-probe act --session ID (--text '…' \| --keys 'Enter' \| --resize 40x120) [--wait-for-rev N --wait 10s] [--expect-rev N]` | Real keyboard input only. Atomic act+wait+observe in one call. |
| `codeaf-probe finish --session ID` | End the session; clean up only resources the probe owns. |
| `codeaf-probe fixture list\|reset\|verify --fixture NAME` | Reproducible product state: list, reset to identity, verify. |
| `codeaf-probe record --session ID` | Step-indexed action/observation recording. |
| `codeaf-probe --json-contract` | Machine-readable contract: verbs, flags, error codes. |

## Example journey (paste-ready)

```sh
codeaf-probe prepare --bin bin/codeaf
codeaf-probe fixture reset --fixture clean-repo
codeaf-probe start --fixture clean-repo --session t1
codeaf-probe act --session t1 --keys Enter --wait-for-rev any --wait 5s
codeaf-probe observe --session t1
codeaf-probe act --session t1 --text 'add a README section on fixtures' --wait-for-rev 3 --wait 30s --expect-rev 2
codeaf-probe observe --session t1
codeaf-probe record --session t1
codeaf-probe fixture verify --fixture clean-repo
codeaf-probe finish --session t1
```

## Error codes

| Code | Meaning | What to do |
| --- | --- | --- |
| `E_STALE_REV` | Session moved since the revision you acted against | Re-`observe`, then retry with the new `rev`. |
| `E_WAIT_TIMEOUT` | Bounded wait expired without the revision advancing | The screen is not necessarily done (quiet ≠ complete); raise the bound or read product state. |
| `E_NO_SESSION` | Unknown or already-finished session | `start` a new one; sessions do not resurrect. |
| `E_BUILD_MISMATCH` | Requested build differs from the session's pinned build | Finish the session first; targets are never swapped mid-session. |
| `E_FIXTURE_UNKNOWN` | Fixture name not found | `fixture list` for names. |
| `E_BOUND` | A session/length/concurrency bound was hit | Truthful, not an error to hide — shorten the journey or raise the bound explicitly. |

## Fixture reset

`fixture reset --fixture NAME` restores the fixture's recorded identity
(schema/version aware) into a fresh isolated home — never the real user's data,
never raw-DB mutation. `clean-repo` is a clean checkout; returning-user
fixtures come from supported product seeding. Reset is deterministic, so the
same journey on the same fixture is repeatable in CI; live-model campaigns are
nondeterministic and are reported as campaigns, not checks.
