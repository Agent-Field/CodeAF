# Health

Each dated section is one lane's gate run. This file merges by union.

## 2026-10-10 t-health-2

Tree: `d3-int` at `30c58de75`. Lane branch `lane/t-health-2-317`. The one-line capability-list fix is `c494e1bce` and does not change Go or the UI the Playwright server served.

### Gates

| Gate | Result |
| --- | --- |
| `npx tsc --noEmit -p .` | pass |
| `npm run design:check` | pass |
| `npm run unit:test` | 1137 pass, 1 fail, then fixed; rerun 1138 pass, 0 fail |
| Playwright chromium+webkit, workers 6, port 1817, line reporter | 314 failed, 16 skipped, 1850 passed (40.0m), exit 1 |
| `go build ./...` | pass |
| `make test-laws` | fail: `internal/e2e`, `internal/manual`. `internal/session` failed only because this lane's files changed during the run; a rerun on a stable tree passed |
| `make test-touched BASE=df7b9968f` | fail, exit 2. Session had 7 failures, over the retry cap of 5, so those were not compared to base |

### Fixed here

| Failure | What changed |
| --- | --- |
| `nativeControls.test.ts`: reviewed grants omitted `http:default` | `c494e1bce` adds that identifier. The scoped loopback grant was already in `capabilities/default.json`. Rerun: 19/19 in that file, then 1138/1138 unit tests |

### Go failures filed

| Task | Cause |
| --- | --- |
| `t-fix-completion-load-timeout` | Fix: agreement completion test times out under load |
| `t-fix-desktop-roles-council` | Fix: council and deciding engine roles have no desktop role |
| `t-fix-host-guard-bridge` | Fix: desktop-bridge e2e launch is outside the host guard |
| `t-fix-manual-search-rank` | Fix: chat manual search no longer returns the page that answers |
| `t-fix-place-advice-offered` | Fix: a settled turn records the place advice ask as not offered |
| `t-fix-plandb-cli-nodes` | Fix: plandb CLI session tests cannot see the node they wait on |
| `t-fix-prefix-budget` | Fix: session prompt prefix is over its byte budget |
| `t-fix-repo-root-stray-git` | Fix: repo walk treats a stray .git directory as a repository |

Passing touched packages: `cmd/codeaf-replay`, `internal/config`, `internal/council`, `internal/decide`, `internal/remote`, `internal/roles`.

Session law rerun (`go test` of the law names in `internal/session`) passed in 23s after the tree stopped changing.

### Playwright failures filed

314 failing runs, every one in a task below. Counts are chromium+webkit runs, not unique titles.

| Task | Cause |
| --- | --- |
| `t-fix-pw-Banner-test` | Fix: Banner-test UI spec failures |
| `t-fix-pw-axe-contrast` | Fix: full-suite axe failures (contrast, tray scroll, queue aria) |
| `t-fix-pw-banner-motion` | Fix: reduced-motion next-up banner transition duration is 0s |
| `t-fix-pw-composer-menus` | Fix: composer-menus UI spec failures |
| `t-fix-pw-conversation-background` | Fix: conversation-background UI spec failures |
| `t-fix-pw-conversation-incremental` | Fix: conversation-incremental UI spec failures |
| `t-fix-pw-conversation-questions` | Fix: conversation-questions UI spec failures |
| `t-fix-pw-conversation-scroll-restore` | Fix: conversation-scroll-restore UI spec failures |
| `t-fix-pw-conversation-task-panel-design` | Fix: conversation-task-panel-design UI spec failures |
| `t-fix-pw-conversation-tasks-states` | Fix: conversation-tasks-states UI spec failures |
| `t-fix-pw-council-servers` | Fix: council spec runs in the full UI suite without its servers |
| `t-fix-pw-d5-cv-test-incremental` | Fix: d5-cv-test-incremental UI spec failures |
| `t-fix-pw-d5-sh-strip-menu` | Fix: d5-sh-strip-menu UI spec failures |
| `t-fix-pw-d5-sh-strip-test` | Fix: d5-sh-strip-test UI spec failures |
| `t-fix-pw-d5-sh-tab-state-test` | Fix: d5-sh-tab-state-test UI spec failures |
| `t-fix-pw-decisions` | Fix: decisions UI spec failures |
| `t-fix-pw-empty-tint` | Fix: empty-start tint spec times out on the place tint control |
| `t-fix-pw-files-tabs` | Fix: files-tabs UI spec failures |
| `t-fix-pw-first-place-tabs` | Fix: first-place-tabs UI spec failures |
| `t-fix-pw-focus-ring` | Fix: question focus ring is 2px plus halo, spec expects a 6px shadow |
| `t-fix-pw-folder-place-arrival` | Fix: folder-place-arrival UI spec failures |
| `t-fix-pw-history` | Fix: history UI spec failures |
| `t-fix-pw-ink-token` | Fix: ink token values no longer match tray and Retry specs |
| `t-fix-pw-markdown` | Fix: markdown UI spec failures |
| `t-fix-pw-mini-tray` | Fix: unfocused split pane mini-tray is hidden |
| `t-fix-pw-mock-engine` | Fix: mock-engine UI spec failures |
| `t-fix-pw-newtab-history` | Fix: newtab-history UI spec failures |
| `t-fix-pw-places-shell` | Fix: place home does not show Launch copy or Earlier work |
| `t-fix-pw-shell-menus` | Fix: shell-menus UI spec failures |
| `t-fix-pw-shell-menus-integration` | Fix: shell-menus-integration UI spec failures |
| `t-fix-pw-shortcut-plus` | Fix: shortcut labels render with plus signs |
| `t-fix-pw-stale-places` | Fix: stale-places UI spec failures |
| `t-fix-pw-tab-deep-links` | Fix: tab-deep-links UI spec failures |
| `t-fix-pw-tab-integrity-completion` | Fix: tab-integrity-completion UI spec failures |
| `t-fix-pw-tab-polish` | Fix: tab-polish UI spec failures |
| `t-fix-pw-tab-preview` | Fix: tab preview card does not show Needs you or the close control |
| `t-fix-pw-tab-selection-label` | Fix: multi-selected tab accessible name is selected for grouping |
| `t-fix-pw-tabs` | Fix: tabs UI spec failures |
| `t-fix-pw-tabstrip-fade` | Fix: overflowing tab strip does not set data-fade-end |
| `t-fix-pw-task-row-colour` | Fix: task state colour is neutral ink, spec expects a red oklch |
| `t-fix-pw-theme-motion` | Fix: theme-motion UI spec failures |
| `t-fix-pw-tray-opacity` | Fix: tray scrim opacity is 0.6 where the spec expects 0.4 |

Axe detail lives on `t-fix-pw-axe-contrast`: 32 `scrollable-region-focusable`, 16 `color-contrast`, 6 `aria-allowed-attr`.

### Not a product failure

The first Playwright invocation died because the config lived in `/tmp` and could not resolve `@playwright/test`. A second run deleted that config while workers were still importing it. Both were discarded. The recorded run used `/tmp/t-health-2-317/playwright.config.ts` with a `node_modules` link, `reuseExistingServer: false`, port 1817, output under `/home/santosh/.claude/jobs/8e2ae8c2/tmp/pw-t-health-2-317`.

`TestAgreementDoesNotBuyASecondCompletion` says a one-minute timeout on a loaded box is not evidence the seam never fired. This run shared the box with the Playwright suite. It is still filed (`t-fix-completion-load-timeout`).

Place-graph failures (`t-fix-repo-root-stray-git`) trigger on this box because `/tmp/.git` exists and is not a valid git repository, and `repoRoot` treats any `.git` entry as a repo root.

