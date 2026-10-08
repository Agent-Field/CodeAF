# CodeAF desktop

Private foundation for the next CodeAF desktop app: Tauri 2 + React/TypeScript + Go. Arc-inspired restraint with native OS window controls, system fonts, semantic themes, and keyboard-first navigation. The macOS shell uses native sidebar vibrancy and real traffic lights; Linux retains native decorations with a tinted opaque fallback.

## Start

Requires Node 24, Go 1.26, Rust stable, and [Tauri platform prerequisites](https://v2.tauri.app/start/prerequisites/). macOS requires Xcode command line tools. Ubuntu/Debian requires WebKitGTK 4.1 and the GTK/build dependencies documented there.

```sh
npm ci
npm run dev          # browser preview at http://localhost:1420
npm run desktop:dev  # real native app + bundled Go engine
npm run check        # TypeScript/build + Go contract tests
npm run desktop:build
```

Browser preview supports navigation, appearance, and the command palette; the engine check explicitly requires the desktop app. No agent execution or project persistence is implemented yet.

## Architecture

`src/` is React, `src/styles/tokens.css` owns themes, `src/lib/engine.ts` owns the typed renderer contract, `src-tauri/` is the native shell, and `engine/` is the Go engine. The shell invokes the bundled Go executable for a JSON health response. There is no listener, port, or arbitrary process API exposed to the renderer. The initial command starts and exits; long-running engine supervision and streaming are future work.

The build script generates a sidecar matching the Rust target triple. Set `CARGO_BUILD_TARGET` to build the corresponding Go target; native shell builds still require the platform toolchain. Go binaries alone can be cross-compiled; Linux builds do not produce a validated macOS application.

## Design

The final icon family is AnimateIcons Lucide outline. All theme/type/spacing/brand/window geometry values live in `src/design/tokens.json`. Shared controls live in `src/components/ui/`, and `npm run design:check` is a required pre-build gate. Read [the contributor contract](AGENTS.md) before editing. Claude contributors follow the same contract through `CLAUDE.md`.

Read [the design standard](docs/DESIGN.md). System fonts, warm neutral light theme, charcoal dark theme, graphite accent, spacing/radius/motion tokens, native window controls, visible focus, and reduced motion are the defaults. No custom fonts or external asset requests.

## Checks

CI verifies frontend/Go contracts and native compilation on Ubuntu and macOS. Signing, notarization, auto-update, Windows validation, and production distribution are not configured.

See [development across machines](docs/DEVELOPMENT.md).
