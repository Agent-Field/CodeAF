# Desktop development

The React + Tauri desktop scaffold lives in `desktop/` alongside the real
codeaf engine. The integration branch is `work/8f3c2a9d`, based on public
`dev`. It is public, and its opaque name does not provide privacy. This
branch is intentionally not a pull request and must not be merged into
`dev` without a later explicit decision.

## Start and check

```sh
cd desktop
npm ci
npx playwright install chromium webkit
npm run dev
npm run check
npm run test:ui
```

Run `npm run desktop:dev` for the native shell or `npm run desktop:build`
for a platform build. Each platform needs its own Tauri prerequisites.
The native minimum remains 800 x 560; browser previews must also reflow
down to 320px without clipped controls or horizontal page scrolling.

## Keep connected to dev

```sh
git remote add upstream git@github.com:Agent-Field/CodeAF.git # once, if absent
npm --prefix desktop run upstream:sync
git push
```

The sync command requires a clean integration branch, fetches and merges
`upstream/dev`, and runs desktop checks, both browser engines and the root
`pr-ready` gate. It never pushes, rebases or force pushes. A failed merge
leaves conflicts for review; resolve them before rerunning checks. Install
browser prerequisites before the first sync. Both remotes may point to the
same public repository: origin publishes this branch, upstream tracks dev.

## One execution engine

The real engine remains in the root repository: existing coding loops,
task splitting, prompts, model routing, storage and program execution.
The nested desktop `engine/` is a health-check placeholder only; no chat
execution or shared-session connection is implemented in this scaffold.
Future packaging stages the canonical root `make build` result rather than
creating a duplicate loop or bypassing the packed manuals and furrow build.

The intended clients attach to the same persistent workspace host. Closing
a desktop tab detaches its view; Stop cancels work explicitly. Interface
layout state may differ, while conversations and tasks remain engine-owned.

Read `desktop/AGENTS.md` and `desktop/docs/DESIGN.md` before UI changes.
Root Go checks omit the nested health module; its tests are included in
`npm --prefix desktop run check`. The desktop workflow lives in root
`.github/workflows/desktop.yml`; workflow files inside desktop would not run.
