# codeaf-probe — coding agent guide

One page, matched to the implemented CLI (verify any doubt with
`codeaf-probe contract`, the machine-readable schema of every verb). Every
invocation prints exactly one compact JSON object on stdout (`{"ok":true,…}`
or `{"ok":false,"error":{"code":…,"message":…}}`); human logs go to stderr;
errors exit non-zero.

## Verbs (as implemented)

| Verb | Flags | Response data |
| --- | --- | --- |
| `prepare` | `--bin PATH` **or** `--source DIR` (exclusive; exactly one required) | `{build:{sha,dirty,go_version,flags,binary}, reused}` — build identity is pinned and reused |
| `start` | `--session ID --profile NAME --bin PATH` (all three required), optional `--arg s` (repeatable, passed to `codeaf chat`) | `{session_id, socket, profile, dims:{width,height}}` — persistent tmux session, survives across CLI calls |
| `observe` | `--session ID [--diff]` | `{revision, snapshot, cursor:{x,y}, processes, ts}` — snapshot is the real rendered pane; `--diff` diffs against your previous observe |
| `act` | `--session ID`, then `--text s` / `--keys ks` / `--resize WxH` (any mix), `--wait quietMs,timeoutMs`, `--expect-revision N` | `{accepted, revision_before, revision_after, stale, observation}` — atomic act+wait+observe; `--expect-revision` mismatches are refused with `STALE_REVISION` and **nothing is sent** |
| `wait` | `--session ID [--quiet ms] [--timeout ms]` | `{settled, reason:"quiet"\|"timeout", revision}` — truthful, not an error |
| `finish` | `--session ID` | `{recorded, removed}` — kills the probe-owned tmux session and removes its home; writes the terminal record |
| `fixture-prepare` | `--scenario clean\|returning` | `{scenario, home, seeded}` — idempotent; a verified fixture of the same version is returned as-is |
| `fixture-reset` | `--scenario clean\|returning` | `{scenario, home, seeded}` — replays the recipe from an empty directory after verifying the old tree; local file work only, no model calls |
| `contract` | — | the JSON schema of every verb, flag, request and response |
| `record-outcome` | `--session ID --outcome ok\|failed\|error [--reason s]` | `{recorded}` — appends the journey's terminal record to the session evidence file |

Unknown verbs and flags print `usage` on stderr and return `BAD_REQUEST`.

**Not implemented today (deferred):** `start` does **not** accept `--fixture`;
it always creates its own isolated home under the probe root. Fixture
`fixture-prepare`/`fixture-reset` produce verified, repeatable fixture homes
(`data.home`), but no CLI verb yet wires a fixture home into `start`, and there
is no `fixture verify` or `record`/playback verb. Live-model campaigns and the
Spark SSH cluster path are likewise deferred — see DESIGN.md §9.

## Example journey (every line below was executed against the real binary; outputs saved under `probe-evidence/docs/`)

```sh
P=./bin/codeaf-probe
B=$PWD/bin/codeaf
export CODEAF_PROBE_BASE=/tmp/prb-docs.XXXXXX   # short path: tmux -S sockets cap ~104 bytes on macOS

$P contract                                     # read the interface
$P prepare --bin "$B"                           # pin build identity
$P fixture-prepare --scenario clean             # verified fixture state (seeded:false)
$P start --session doc1 --profile reviewer --bin "$B"
sleep 2                                         # let the TUI paint
$P observe --session doc1                       # rendered screen, rev 0
$P act --session doc1 --keys Enter              # rev advances; atomic observation back
$P act --session doc1 --resize 100x40           # real resize
$P wait --session doc1 --quiet 500 --timeout 2000
$P observe --session doc1 --diff                # diff vs previous observation
$P act --session doc1 --expect-revision 999 --keys Enter   # -> STALE_REVISION, nothing sent
$P observe --session nosuch                     # -> NO_SESSION
$P record-outcome --session doc1 --outcome ok --reason "docs verification journey"
$P finish --session doc1                        # owned cleanup: {recorded:true,removed:true}
```

Do **not** type free text ending in an unfinished model turn unless you intend
a paid model call: keys and resize are deterministic.

## Error codes (as implemented, `internal/probe/contract.go`)

| Code | Meaning | What to do |
| --- | --- | --- |
| `STALE_REVISION` | `--expect-revision` did not match the session's current revision; nothing was sent | Re-`observe`, retry with the current revision. |
| `NO_SESSION` | Unknown or already-finished session | `start` a new one; sessions do not resurrect. |
| `TIMEOUT` | A bounded wait did not reach its condition | Quiet ≠ complete; raise the bound or check product state. (`wait` itself returns `settled:false, reason:"timeout"` as a *success* envelope — the TIMEOUT error code marks a bounded wait that was part of another operation.) |
| `NOT_OWNED` | Cleanup refused to touch a resource the probe does not own | Finish only probe sessions. |
| `BAD_REQUEST` | Unknown verb/flag, missing required flag, malformed value, tmux/fixture failure | Read the message (and `usage` on stderr) and fix the request. |

## Fixtures

Scenarios today: `clean` (fresh HOME, no state, `seeded:false`) and `returning`
(a HOME a returning user left — one project, one past conversation, seeded
through product `session.SaveMeta`, `seeded:true`). Prepare is idempotent and
self-verifying against its manifest; reset verifies the old tree first, reports
drift by rebuilding from an empty directory, and re-verifies. Unknown scenario
names are refused by name. Fixtures never touch the real user's home and never
mutate a database raw.
