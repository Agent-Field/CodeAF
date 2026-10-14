# H1 Builder files object selection

Builder now prefers a valid object under `files`, then the first object whose
keys all satisfy `ValidPath`. Unrelated objects are skipped. If no object
qualifies, `FilesObjectError` gives the model the observed keys and the
path-to-content instruction through the existing one-retry conversation.
Content syntax validation and valid response bytes are unchanged.

Deterministic tests cover leading inline app definitions and plan fragments,
mixed valid/invalid keys, wrappers and wrapper precedence, first-map selection,
typed garbage corrections, content validation, and the WRITE step's repair
messages, retry ceiling and billing. The prompt README and compiled chat manual
describe selection and repair.

## Local gates

- `make build`: PASS.
- `go build ./...`: PASS.
- `go vet ./internal/apps/...`: PASS.
- `go test ./internal/apps/... ./internal/config/...`: PASS.
- Focused parser, WRITE repair and few-shot tests: PASS.
- Chat manual retrieval and vocabulary tests: PASS.
- `git diff --check`: PASS.

## Original evidence and missing fixture

SSH to `ron@mac-studio` was refused. The same r8 home is available locally at
`/Users/ron/uxtest/r8/home/apps/inventory-coffee-roastery`. Its saved request is:

> Inventory for our coffee roastery. Items have a name, SKU, category (beans, equipment, packaging), quantity on hand, reorder level and a supplier. Suppliers have a name, email and lead time in days. Show items that are below their reorder level at the top. The agent drafts a purchase order when an item drops below its reorder level; I approve every order before it is sent.

That differs from the green-coffee-lots paragraph in the task.
`~/af-w2/ux/LEDGER.md` is absent on this machine. Both paragraphs have dedicated
live tests in the existing real-binary bridge harness.

The failing home's definition contains only `plan.json`. Its logs are under
`/Users/ron/uxtest/r8/home/logs/`. `calls.jsonl` records two WRITE replies:

| Call | Messages | Completion tokens | Cost |
| --- | ---: | ---: | ---: |
| `fb677be8` | 1 | 833 | $0.0030192 |
| `0c27477e` | 3 | 833 | $0.0032778 |

Those logs contain metadata only. There is no Builder transcript or trace body
in that home, and its chat transcript contains no Builder output. The original
raw model response cannot be quoted or added as a fixture from these artifacts.
It has been requested; no synthetic response is presented as the original.

## Live verification

Both runs use OpenRouter `deepseek/deepseek-v4.1-flash` for Builder and the app
agent, via `bin/codeaf app serve` in a disposable home, the real bridge, Check,
sample generation, and Keep/get. The UX launcher credential was supplied to the
test environment without printing it. The initial no-key SKIP is not acceptance.

Task paragraph, `TestLiveServeInventoryRoastery`: **FAIL**, Check rounds **1**,
cost **$0.027535584**, elapsed **39.09 seconds**. Records and all files were
written; Check passed schema and kit, then sample/render failed on:

```text
invalid: this_week: ERROR: <input>:1:22: Syntax error: extraneous input 'd' expecting <EOF>
 | roast_date >= now - 7d
 | .....................^
```

This is a separate generated computed-expression failure. Repairing sample or
Check handling is outside this lane's package ownership. This live run does not
meet the manager's landing requirement.

Saved r8 paragraph, `TestLiveServeInventoryPurchaseOrders`: **PASS**, Check
rounds **3** (plus the reduced-definition check at round 3), cost **$0.033661**,
elapsed **87.66 seconds**. Preview and Keep succeeded, state `kept`, version
`1`, five files. Its purchase-order automation failed behaviour Check and was
left out before the reduced definition passed. This is a Preview/Keep pass with
an omitted automation, not evidence that purchase orders work.

Logs: `.tmp/live-inventory.log` and `.tmp/live-inventory-purchase-orders.log` in
this worktree. The local branch is `apps-hold/h1-builder-files`. No branch was
pushed and no pull request was opened. Do not land this lane until the task
paragraph passes live Check and the missing original raw-response fixture is
supplied.
