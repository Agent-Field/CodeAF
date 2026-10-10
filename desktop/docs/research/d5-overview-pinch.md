# Overview pinch research

Task: `t-d5-sh-overview-pinch` · SH-190 · 2026-10-10.
Dependency: `t-s1-overview`. Design references: Shell 2h, S-3h-13,
L-overview OV2 and Interactions Shortcuts. Iteration 2 does not replace the
pinch-out instruction.

## Decision: no additional wiring

**No:** do not add a cross-platform `useOverviewPinch` or broaden the existing
listener to the whole window. Physical pinch cannot be reliably distinguished
from ordinary zoom intent using the renderer fields currently inspected.
Keep the existing overview button and keyboard doors (⌘⇧\\ on macOS,
Ctrl Shift A on Linux). Native event delivery remains an acceptance gap, rather
than a reason to claim either platform has no pinch events.

This is a research decision, not an implementation rollback. The checkout
already has `useOverviewGesture(root: RefObject<HTMLElement | null>, enabled:
boolean, open: () => void): void`, wired in `Workspace.tsx`. It listens only over
`.workspace-tabstrip`, ignores editing fields and modal dialogs, opens at
`scale >= 1.12`, and accumulates negative Ctrl pixel-wheel deltas across gaps
of at most 200ms. It prevents negative eligible wheel events before reaching
the threshold. It does **not** distinguish a physical pinch from Ctrl+wheel
on a precision mouse or touchpad. Existing synthetic tests prove that scoped
behavior, not native gesture support. No replacement hook is proposed.

## Platform evidence and observations

| Platform | Events supported by source evidence | Events observed in this lane | Zoom distinction |
| --- | --- | --- | --- |
| macOS / Tauri WKWebView | WebKit's macOS gesture path supports `gesturestart`, `gesturechange`, `gestureend` with scale/rotation, and conversion to pixel `wheel` with `ctrlKey=true`. Delivery/order depends on the installed WebKit and event handling; do not assume both paths arrive independently. | No native observation: runner is Linux aarch64 without a macOS host. Browser tests dispatch all three gesture names and Ctrl+wheel artificially. | Gesture scale identifies magnification, not whether the person intended overview rather than page zoom. Ctrl+wheel alone has no physical-pinch identity. |
| Linux / Tauri WebKitGTK | GTK `GtkGestureZoom` begin/change/end feed native magnification. The inspected zoom-begin path also synthesizes a zero-delta wheel; this does not establish a negative Ctrl-wheel stream or DOM GestureEvent delivery for a trackpad. | No native observation: neither DISPLAY nor WAYLAND_DISPLAY is set and no physical trackpad is available. Playwright WebKit is not the packaged Tauri WebKitGTK view. | A GTK/native gesture bridge could expose origin/phase, but that is not evidence that current renderer listeners receive it. Ctrl+wheel, if delivered, remains ambiguous. |

The macOS wheel conversion is confirmed by [WebKit's implementation record
225788](https://bugs.webkit.org/show_bug.cgi?id=225788) and the developer's
[macOS-only clarification in 230545](https://bugs.webkit.org/show_bug.cgi?id=230545#c6).
Its [PlatformWheelEvent implementation](https://github.com/WebKit/WebKit/blob/main/Source/WebCore/platform/PlatformWheelEvent.cpp)
sets Control and pixel granularity for converted gestures.
The [GTK view](https://github.com/WebKit/WebKit/blob/main/Source/WebKit/UIProcess/API/gtk/WebKitWebViewBase.cpp)
connects `webkitWebViewBaseZoomBegin`, `ZoomChanged`, and `ZoomEnd` to the GTK
recognizer. [ViewGestureControllerGtk](https://github.com/WebKit/WebKit/blob/main/Source/WebKit/UIProcess/gtk/ViewGestureControllerGtk.cpp)
applies magnification. These are upstream source findings inspected on the
research date, not a trace of a particular installed WebKitGTK release.

[WebKit report 233141](https://bugs.webkit.org/show_bug.cgi?id=233141) records
macOS Safari native overview occurring despite cancelled gesture events.
That report concerns Safari, not proof of a current Tauri failure; it establishes
why cancellation must be checked in a real WKWebView. The existing listener
cancels gesturechange, but not gesturestart. `preventDefault()` and a
non-passive listener alone are not a demonstrated guarantee against native zoom.

The locked Wry version is 0.57.0. Its local macOS and GTK constructors expose
back/forward gesture configuration but do not establish DOM pinch delivery.
Tauri's [zoomHotkeysEnabled configuration](https://v2.tauri.app/reference/config/#zoomhotkeysenabled)
controls WebView2 zoom on Windows and injects a permission-gated keyboard zoom
polyfill on macOS/Linux. It does not identify physical pinch input.

## Minimal renderer probe

Paste into the **main Tauri window** devtools, not a child web page. The probe
records only event and viewport numbers in memory; it captures no document
text, URLs, keys, or renderer storage. Run once without cancellation, then
repeat with cancellation enabled. `visualViewport.scale`, viewport width and
DPR are diagnostics, not a portable page-zoom detector.

```js
(() => {
  const rows = [];
  let cancel = false;
  const names = ['gesturestart', 'gesturechange', 'gestureend', 'wheel'];
  const options = { capture: true, passive: false };
  const record = (e) => {
    if (cancel && e.cancelable &&
        (e.type.startsWith('gesture') || (e.type === 'wheel' && e.ctrlKey))) {
      e.preventDefault();
    }
    rows.push({
      type: e.type, trusted: e.isTrusted, time: e.timeStamp,
      scale: e.scale, rotation: e.rotation,
      ctrl: e.ctrlKey, meta: e.metaKey, alt: e.altKey, shift: e.shiftKey,
      dx: e.deltaX, dy: e.deltaY, mode: e.deltaMode,
      cancelable: e.cancelable, prevented: e.defaultPrevented,
      viewportScale: visualViewport?.scale,
      width: innerWidth, dpr: devicePixelRatio,
    });
    if (rows.length > 300) rows.shift();
  };
  names.forEach(name => window.addEventListener(name, record, options));
  window.overviewPinchProbe = {
    rows,
    setCancel(value) { cancel = Boolean(value); },
    clear() { rows.length = 0; },
    stop() {
      names.forEach(name => window.removeEventListener(name, record, options));
      delete window.overviewPinchProbe;
    },
  };
})();
```

1. Record OS, Tauri/Wry and installed WebKit versions, input device, and Linux Wayland/X11 session. Use the real desktop build with existing gesture handling noted.
2. Over the tab strip, test a finger spread, inward pinch, ordinary two-finger scroll and physical Ctrl+wheel separately; inspect `overviewPinchProbe.rows` and visible zoom/overview effects after each.
3. Repeat over conversation text, the composer, a terminal and a native web pane. A child webview is a separate document; this main-window probe does not observe its events.
4. Repeat with `overviewPinchProbe.setCancel(true)`. Check defaultPrevented, visible native zoom and whether one gesture fires both wheel and gesture paths. OS accessibility zoom may intercept input before the app.
5. Copy the numeric trace into this report with its platform/version label; call `overviewPinchProbe.stop()`. Test both directions rather than inferring finger direction from Safari's own overview convention.

The design says “pinch out” but does not specify the target surface, threshold,
zoom ownership, unsupported-platform fallback, or gesture precedence. Here it
means a finger spread, consistent with the current scale-increase handler;
that interpretation is an assumption, not a native observation.

## Exact Open-table row

| # | Question | Assumption the app ships now |
|---|---|---|
| OV2 | Shell 2h says pinch out opens overview. Which surface owns it, and may it replace page zoom when WKWebView/WebKitGTK cannot identify physical pinch separately from Ctrl+wheel? | Existing tab-strip-only useOverviewGesture handles scale and Ctrl pixel-wheel events; synthetic tests pass, but native delivery and zoom cancellation are unconfirmed. No additional or window-wide wiring is recommended. Keep the overview button and platform keys; do not claim pinch support on either native platform until the probe in research/d5-overview-pinch.md passes. |

The separate `OV-PINCH-286` Open row records the design-silent direction,
surface and zoom-precedence assumptions.

## Verification and remaining acceptance

The v-Shell design was opened locally through desktop's Playwright in Chromium
and WebKit. Its Overview instruction was measured with getBoundingClientRect
and getComputedStyle: both browsers measured 572 × 57.5625px with 12px
type and approximately 19.2px line height at a 1400 × 900px viewport. No UI
geometry changes are part of this lane.
The implementation tests deliberately synthesize event properties, including
`isTrusted=false`; passing them cannot fill the native observation column.

Research decision, source review, reproducible probe and ledger correction are
complete. Physical input traces on macOS WKWebView and Linux WebKitGTK remain
unavailable on this headless Linux runner. SH-190 native gesture acceptance
must remain open. This work adds no user-visible feature and changes no engine
or renderer behavior, so it needs no new manual page or backend seam.

Checks: `npx tsc --noEmit -p .` and `npm run design:check` passed;
`node --test src/features/tabs/overviewGesture.test.ts` passed 4/4;
`overview-gesture.spec.ts` passed 16/16 across Chromium and WebKit, light/dark,
on isolated port 1786 in 19.3 seconds. The temporary config disabled server
reuse and used `--configLoader runner` because the default Vite bundler tried
to write into shared read-only node_modules. The temporary config was removed.
The embedded probe was also evaluated in both browsers: cancellation toggle,
300-row buffer cap, clear and listener cleanup passed. Go/native checks are
not applicable to these documentation-only changes.
