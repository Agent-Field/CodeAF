# CodeAF visual standard

Match the classic Arc desktop reference: a slim tinted sidebar and a single inset content pane. Minimize visible chrome. The content belongs to the user, not a dashboard of promotional cards.

## Geometry

Sidebar 216px (190px for smaller windows), 6px outer frame, 6px content corners. Navigation rows 34px; top controls 28px. No full-width page header divider, profile card, uppercase section labels, hero banners, or default large filled CTA. Sidebar selections use a softly translucent neutral surface, never a saturated accent block.

## Typography and colors

System sans only: SF Pro on macOS, system-ui on Linux. Interface 12px, supporting labels 10–11px, page heading 25px with medium weight. System monospace for code. Warm sand/peach chrome in light mode; warm charcoal in dark mode. White/off-white content is opaque. The primary accent is graphite: reserve stronger color for meaningful status or actual project identities. All visual values originate in `src/design/tokens.json`; `src/styles/tokens.css` is generated. Screens consume semantic variables, not literal values.

## Native materials

macOS uses a real NSVisualEffect sidebar material through Tauri windowEffects, a transparent WebView, native traffic lights and overlay titlebar. CSS paints only a light tint over the native material. Linux uses native decorations and a solid soft tint; Tauri does not support Linux backdrop effects. Never draw fake macOS traffic lights.

Browser preview renders a quiet sand-to-peach background as an approximation of tinted chrome. It cannot blur the desktop behind the browser. Do not present browser screenshots as proof of native translucency.

## Interaction

Cmd/Ctrl+K opens the command palette; Cmd/Ctrl+B toggles the sidebar. Theme defaults to System, persists locally, and synchronizes the native window appearance. Keep visible focus, semantic controls, modal focus restoration, and reduced motion. Use the top chrome as a native drag region. Sidebar collapse removes hidden controls from keyboard navigation.

## Distribution note

The current macOS transparent WebView uses Tauri's macOSPrivateApi mode, which is not compatible with Mac App Store acceptance. Direct signed/notarized distribution remains the intended option for this scaffold. If App Store distribution becomes a requirement, reassess the transparent WebView approach rather than silently retaining private API use.

## Central source and change workflow

| Concern | Authoritative source |
| --- | --- |
| Color, spacing, geometry, typography, effects | `src/design/tokens.json` |
| Semantic icon names and upstream implementation | `src/components/ui/Icon.tsx` |
| Controls and their CSS | `src/components/ui/` and `src/styles/ui.css` |
| Theme and reduced motion behavior | `src/design/ThemeProvider.tsx` |
| Monogram geometry, favicon and app identity | `brand` in `src/design/tokens.json` |

Edit the source, run `npm run design:generate`, then `npm run check`. Run `npm run brand:icons` when brand geometry or brand colors change. Native raster/ICNS/ICO outputs are fingerprinted so a stale or manually modified icon fails validation. Light, Dark and System-dark derive from the same semantic color maps; never maintain duplicate theme values in feature CSS.

## Typography roles

| Role | Size | Weight | Leading |
| --- | --- | --- | --- |
| Caption / shortcut | 10px | 400 | inherited |
| Label / toolbar / action | 11px | 400 | inherited |
| Body / code | 12px | 400 | 1.6 |
| Section heading | 12px | 550 | 1.6 |
| Page heading | 25px (21px narrow) | 500 | 1.3 |

System sans is used everywhere except real code/terminal content, which uses system monospace. Do not bundle SF Pro or web fonts. Do not uppercase labels or add tracking to navigation. Page headings alone use slight negative tracking. Use the shared Typography components so future type adjustments apply everywhere.

## Icon decision

**Lucide outline is the final family.** This is a design choice for neutral geometric tool icons that remain coherent in our compact sidebar, rather than a claim that Arc uses Lucide. The other candidates (Heroicons, Tabler, Phosphor and Hugeicons) are viable in isolation, but may not be mixed into this app. Use AnimateIcons' Lucide implementation through our semantic registry only. No extra icon runtime or separate animation library is needed.

| Semantic name | Upstream glyph | Use |
| --- | --- | --- |
| sidebar | PanelLeft | Sidebar visibility |
| code | CodeXml | Workspace / code |
| activity | Activity | Activity and engine status |
| grid | LayoutGrid | Design system / layout |
| search / plus / settings / arrow | Search / Plus / Settings / ArrowRight | Shared actions |

Sizes: xs 13px for trailing hints, sm 14px for navigation, md 16px for controls, lg 17px for favorites. The central stroke is 1.6 in a 24-unit viewBox, with rounded caps/joins. Use currentColor only. Navigation and utility glyphs stay still. Explicit directional action arrows translate 2px over 280ms; disclosure chevrons rotate only with open state. No upstream default glyph animation. Do not introduce idle animations or increase icon size to compensate for a poor glyph. Icon controls are 28px; compact sidebar actions are 32–34px high. This is desktop density, not a touch-optimized interface.

## Components and specimens

The app uses Button (quiet/secondary/primary/loading/disabled), IconButton, NavigationItem, SidebarAction, TextInput, Select, ThemeSelect, Typography, Surface, Separator, KeyboardShortcut, BrandMark and CommandPalette. The Design system screen presents live colors, fonts, monospaced text, icon choices, button states and spacing. Add new reusable primitives there before using them widely.

## Brand

The CodeAF C is one vector arc, not a text glyph. The same geometry produces the monochrome UI mark, SVG favicon, Safari mask icon and desktop app icons. Brand colors are graphite and off-white, independent of the warm sidebar material; no remaining violet branding. It is decorative inside the labeled CodeAF control and does not substitute for an accessible name.

## Enforcement and limits

`npm run design:check` runs before every production build and checks generated outputs, known CSS tokens, literal CSS style values, typography/opacity literals, breakpoints, icon import boundaries, raw screen controls, inline styles and per-screen SVGs. Regression tests prove common violations fail. `AGENTS.md`, `CLAUDE.md` and `CONTRIBUTING.md` require all contributors to use that gate. This is a concrete baseline; it does not automatically judge every visual design decision. Human review still checks coherence, readable contrast and native behavior. No branch protection or successful remote CI run is implied.

Sources: [AnimateIcons](https://github.com/Avijit07x/animateicons), [Lucide](https://lucide.dev/), and the [alternative family browser](https://icons.lndev.me/?lib=heroicons).

## Motion and interaction states

All timings, curves, travel and scale originate in tokens. Control colors settle over 220ms. Sidebar layout changes over 320ms with a gentle decelerating curve; hidden controls become inert. Shared overlay entry is 240ms, moving only 3px and scaling from 0.985; exit fades over 220ms. Shared keyframes live in ui.css, never in feature styles. Reduced motion sets durations and travel to zero while keeping state changes immediate.

Hover is quiet neutral feedback; pressed is slightly stronger. Favorite tiles indicate the active view with aria-pressed; navigation uses aria-current. Secondary buttons have hover, press, visible keyboard focus and disabled states. Selection is meaningful state, not a permanent highlight on every control.

App-owned dropdowns use shared Select with token-colored popup, highlight and checkmark; the background is inert while open. Keyboard focus uses a thin ring; the palette search uses a subtle underline instead of a large box. Native OS dialogs retain their platform appearance.

## Browser regression gate

Run npm run test:ui for Chromium and WebKit. The reusable contracts check themed surfaces, absence of visible native selects/unwrapped controls, WCAG accessibility, theme persistence and system changes, menu keyboard/dismissal/focus behavior, hover/press/selection/disabled states, intentional icon motion and reduced motion. New controls must extend these contracts. These checks catch covered regressions; they cannot universally judge aesthetics or prove native desktop materials.
