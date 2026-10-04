# atlas — an interactive architecture map in the terminal

A terminal app built with [OpenTUI](https://opentui.com) (`@opentui/core`). It shows how
codeaf's two-computer work (pairing, devices, cells, sync/relay, the furrow engine) fits together.

## Run

Needs [Bun](https://bun.sh) 1.3 or newer (written against 1.4.0).

```sh
cd tools/atlas
bun install
bun start        # the explorer; looks best at 120x35 or larger, still works at 80x24
bun test         # frame tests (OpenTUI test renderer)
bun run typecheck
```

## Use

| Input | What it does |
| --- | --- |
| drag a box | moves it; the arrows follow |
| click a box, or `tab` then `enter` | opens the detail pane (what it does, files, key symbols, what it talks to) |
| `1`..`n`, `f` | opens a step-through flow / cycles through flows |
| `←` `→` (`h` `l`), or click `◀ prev` / `next ▶` | steps the flow; the active arrow and message turn orange |
| `p` / `space` | plays or pauses the flow |
| `0` / `esc` | back to the overview / closes a pane |
| `?` | help and colour legend |
| `q` / `ctrl+c` | quits and restores the terminal |

## Layout of the code

- `src/types.ts`: the schema (`AtlasData`: nodes, edges, flows, file refs).
- `src/data.ts`: **all diagram content**. Update this when the code changes. Every `files[].path`
  is relative to the repo root, and `test/data.test.ts` fails if one doesn't exist.
- `src/app.ts`: rendering and interaction. `createApp(renderer, data)` works with a real renderer
  or with `createTestRenderer()` in tests.
- `src/theme.ts`: the colour scheme (one colour per node kind, orange for the active flow step).
- `src/index.ts`: the entry point.
