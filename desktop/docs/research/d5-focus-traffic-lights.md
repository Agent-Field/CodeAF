# Focus mode and the macOS traffic lights (t-d5-sh-focus-native-lights, covers SH-013)

## Recommendation

**Option (b): a native command that hides and shows the three traffic lights.** The renderer calls it when the strip is hidden or revealed. Signature below.

Why: the design (Shell 2d, 2h) puts the lights inside the strip ("the traffic lights and the rail toggle move into the tab strip"). Focus mode "also hides the strip", so the lights go with it, and the top-8px peek brings them back with it. Option (a) leaves three coloured dots floating over the page card's top-left corner with no strip behind them.

## What Tauri offers (checked against the pinned tree: tauri 2.12.1, tao 0.37.1, `src-tauri/Cargo.lock`)

- No stable API hides or shows the lights at runtime. In `tauri-2.12.1/src/window/mod.rs` and `webview/webview_window.rs` the traffic-light surface is `traffic_light_position` on the builder (config key `trafficLightPosition`, already `{x:16,y:18}` in `tauri.macos.conf.json`) and the setters `set_closable`, `set_minimizable`, `set_title_bar_style`. `set_closable` and `set_minimizable` only grey a button out; they do not hide it.
- tao 0.37.1 has `titlebar_buttons_hidden` (`platform_impl/macos/window.rs:273-282`), but only as a builder attribute applied at window creation, not at runtime. `set_traffic_light_inset` moves the lights; it does not hide them.
- I found no maintained Tauri 2 plugin that does this. Not verified online: this lane had no network, so a plugin released after the pinned tree is not ruled out.
- The macOS call that does it is `NSWindow.standardWindowButton(.closeButton | .miniaturizeButton | .zoomButton)?.setHidden(_:)`, the same call tao makes at creation. The repo already depends on `objc2-app-kit` 0.3 and `objc2-foundation` (`src-tauri/Cargo.toml:62-63`). The `NSWindow`, `NSButton` and `NSControl` features need adding to the `objc2-app-kit` feature list.

## Options on macOS (reasoned from the code and AppKit behaviour; no Mac in this lane, so no screenshots)

| | (a) Keep lights, gutter only while the strip shows | (b) Hide lights with the strip |
|---|---|---|
| Focus mode, resting | Lights sit over the card's top-left at (16,18). The card has only the top inset, so they cover its content. | Nothing but the card. This is what the design's "hides the strip" shows. |
| Top-8px peek | Strip slides in; gutter (`--native-controls-inset`, 84px) is already honoured. | Strip slides in and the lights are shown in the same frame. |
| Native code | None. | One command, about 25 lines. |
| Risk | Overlap with page content; the lights ignore the "restrained" language. | AppKit can re-show the buttons on a full-screen transition or `setTitleVisibility`; the writer must re-apply after resize and full-screen events. Close, minimize and zoom remain reachable by ⌘W, ⌘M and the menu bar (`menu.rs`), and the peek restores the buttons. |

## Command for the d5-nat writer

```rust
/// Shows or hides the close, minimize and zoom buttons of the calling window.
/// macOS only; a no-op elsewhere so the renderer need not branch.
#[tauri::command]
fn window_set_traffic_lights(window: tauri::WebviewWindow, visible: bool) -> Result<(), String>
```

- Renderer binding: `invoke('window_set_traffic_lights', { visible })`. Register it in `generate_handler!` in `src-tauri/src/lib.rs` and grant it to `main` and `w-*` windows only, never to `web-*` (see the capability test in `web/tests.rs`).
- Must run on the main thread (`window.run_on_main_thread`), as every AppKit call does.
- Implementation: for each of the three `NSWindowButton` kinds, `window.ns_window()` → `standardWindowButton(kind)?.setHidden(!visible)`.
- Renderer rule: `visible = !(focus && peek !== 'strip')`. Call on every change of `focus` or `peek` in `useShellFrame`, and once at window start, so a restored Focus window starts hidden. Reduced motion needs nothing: the buttons switch with the strip's own transition end, not a separate animation.
- While the strip is revealed the existing mac gutter (`--native-controls-inset`) already applies, so nothing changes in CSS.
- Re-apply after `WindowEvent::Resized` and full-screen enter/exit.
- Also hidden in full screen by macOS itself; the command must not force them visible there.

## Follow-through

- The native writer adds a d5-nat task for the command, the renderer binding and the `useShellFrame` call.
- Until it lands, the shipped behaviour stays option (a); nothing in the renderer references the command yet.
