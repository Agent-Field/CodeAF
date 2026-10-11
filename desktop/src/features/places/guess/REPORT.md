# Desktop draft Places lane evidence

The composer requests a read-only guess after 300 ms, hides its previous answer
on every edit, aborts at 20 ms, and rejects late or obsolete replies even when a
transport ignores cancellation. The shared place picker keeps a hand choice local
until the first settled answer, then replaces the primary membership through the
shell's undoable write path. Home reads organiser views, filters real loaded chat
ids, labels shortcut memberships, and posts scoped steer sentences.

## Acceptance

- Done: debounce, valid/absent/failed pill states, stale suppression, 20 ms breach,
  and first-answer filing eligibility; seven deterministic node tests pass.
- Done: fixture controls render and operate in Chromium and WebKit, light and
  dark, at 320, 480, 600, 800 and 1200 px; 20 browser tests pass (15.2 seconds).
- Done: TypeScript, design policy, all 1,289 desktop unit tests, desktop bridge
  tests, manual retrieval (new probes plus existing table), `go vet
  ./internal/manual`, `make build`, `go build ./...`, and `go test ./internal/placeorg/...` pass.
- Gap: `npm run check` and `make test-laws` are not green on clean HEAD. The
  desktop contracts lack the native `web_find` handler and still expect a literal
  scrim value. The Go role law rejects the six organiser role words declared in
  `internal/placeorg/judge.go`. A clean archive of HEAD reproduces these failures.
- Gap: the organiser bridge lane owns `/places/guess`, `/places/{id}/lenses`,
  `/places/steer` and the shortcut wire data. They are absent on this branch;
  this lane uses their frozen wire contract and an isolated fixture transport.
  No live organiser request is claimed as proved.

## Design and environment

Read-only React design renders were opened locally with Playwright. Places 8a
measures the model label at 12px and the Chats label at 11px. Guess-specific
controls are not drawn there; PO-4 through PO-7 record the shipped assumptions.
Browser measurements assert the pill's 24px height, 12px text, 6px radius, and no
page overflow at every tested width. All visual values are named tokens.

The default Vite config loader writes into this worktree's read-only shared
`node_modules`. Browser tests used `--configLoader runner`; the full gate used
that flag temporarily and `package.json` was restored. Its frontend build passed.
The temporary Playwright config was removed after testing. Git staging and fetch
fail with EROFS in this worktree's Git metadata; no commit or push was possible.
