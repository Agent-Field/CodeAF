# CodeAF desktop

- Stack: Tauri 2 shell, React + TypeScript UI, Go sidecar. Rust is the narrow native bridge.
- Read docs/DESIGN.md before changing UI. Reuse src/styles/tokens.css; no new ad hoc colors or font families.
- Keep native window decorations. Never simulate macOS traffic lights on Linux.
- Preserve keyboard navigation, system theme and reduced motion. Use semantic controls.
- Expose typed Tauri commands; never grant renderer arbitrary shell access.
- Run npm run check and cargo fmt --check --manifest-path src-tauri/Cargo.toml.
- Native changes require macOS and Linux verification. Browser preview is not native verification.
- Keep screenshots and recordings outside the source tree.
- This scaffold has no agent/model integration. Do not present demo content as live sessions.
