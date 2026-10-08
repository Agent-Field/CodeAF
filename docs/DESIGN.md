# CodeAF visual standard

Match the classic Arc desktop reference: a slim tinted sidebar and a single inset content pane. Minimize visible chrome. The content belongs to the user, not a dashboard of promotional cards.

## Geometry

Sidebar 216px (190px for smaller windows), 6px outer frame, 6px content corners. Navigation rows 34px; top controls 28px. No full-width page header divider, profile card, uppercase section labels, hero banners, or default large filled CTA. Sidebar selections use a softly translucent neutral surface, never a saturated accent block.

## Typography and colors

System sans only: SF Pro on macOS, system-ui on Linux. Interface 12px, supporting labels 10–11px, page heading 25px with medium weight. System monospace for code. Warm sand/peach chrome in light mode; warm charcoal in dark mode. White/off-white content is opaque. The primary accent is graphite: reserve stronger color for meaningful status or actual project identities. All colors and geometry primitives originate in `src/styles/tokens.css`.

## Native materials

macOS uses a real NSVisualEffect sidebar material through Tauri windowEffects, a transparent WebView, native traffic lights and overlay titlebar. CSS paints only a light tint over the native material. Linux uses native decorations and a solid soft tint; Tauri does not support Linux backdrop effects. Never draw fake macOS traffic lights.

Browser preview renders a quiet sand-to-peach background as an approximation of tinted chrome. It cannot blur the desktop behind the browser. Do not present browser screenshots as proof of native translucency.

## Interaction

Cmd/Ctrl+K opens the command palette; Cmd/Ctrl+B toggles the sidebar. Theme defaults to System, persists locally, and synchronizes the native window appearance. Keep visible focus, semantic controls, modal focus restoration, and reduced motion. Use the top chrome as a native drag region. Sidebar collapse removes hidden controls from keyboard navigation.

## Distribution note

The current macOS transparent WebView uses Tauri's macOSPrivateApi mode, which is not compatible with Mac App Store acceptance. Direct signed/notarized distribution remains the intended option for this scaffold. If App Store distribution becomes a requirement, reassess the transparent WebView approach rather than silently retaining private API use.
