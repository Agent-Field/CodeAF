# codeaf visual standard

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

The app uses Button (quiet/secondary/primary/loading/disabled), IconButton, NavigationItem, SidebarAction, TextInput, TextArea, Select, ThemeSelect, ContextMenu, DropdownMenu, HoverPreview, Typography, Surface, Separator, KeyboardShortcut, BrandMark and CommandPalette. The Design system screen presents live colors, fonts, monospaced text, icon choices, button states and spacing. Add new reusable primitives there before using them widely.

## Brand

The codeaf C is one vector arc, not a text glyph. The same geometry produces the monochrome UI mark, SVG favicon, Safari mask icon and desktop app icons. Brand colors are graphite and off-white, independent of the warm sidebar material; no remaining violet branding. It is decorative inside the labeled codeaf control and does not substitute for an accessible name.

## Enforcement and limits

`npm run design:check` runs before every production build and checks generated outputs, known CSS tokens, literal CSS style values, typography/opacity literals, breakpoints, icon import boundaries, raw screen controls, inline styles and per-screen SVGs. Regression tests prove common violations fail. `AGENTS.md`, `CLAUDE.md` and `CONTRIBUTING.md` require all contributors to use that gate. This is a concrete baseline; it does not automatically judge every visual design decision. Human review still checks coherence, readable contrast and native behavior. No branch protection or successful remote CI run is implied.

Sources: [AnimateIcons](https://github.com/Avijit07x/animateicons), [Lucide](https://lucide.dev/), and the [alternative family browser](https://icons.lndev.me/?lib=heroicons).

## Motion and interaction states

All timings, curves, travel and scale originate in tokens. Control colors settle over 220ms. Sidebar layout changes over 320ms with a gentle decelerating curve; hidden controls become inert. Shared overlay entry is 240ms, moving only 3px and scaling from 0.985; exit fades over 220ms. Shared keyframes live in ui.css, never in feature styles. Reduced motion sets durations and travel to zero while keeping state changes immediate.

Hover is quiet neutral feedback; pressed is slightly stronger. Favorite tiles indicate the active view with aria-pressed; navigation uses aria-current. Secondary buttons have hover, press, visible keyboard focus and disabled states. Selection is meaningful state, not a permanent highlight on every control.

App-owned choices use shared Select; action menus use shared ContextMenu and DropdownMenu with token-colored popups, highlights and checkmarks; the background is inert while open. Keyboard focus uses a thin ring; the palette search uses a subtle underline instead of a large box. Native OS dialogs retain their platform appearance.

## Browser regression gate

Run npm run test:ui for Chromium and WebKit. The reusable contracts check themed surfaces, absence of visible native selects/unwrapped controls, WCAG accessibility, theme persistence and system changes, menu keyboard/dismissal/focus behavior, hover/press/selection/disabled states, intentional icon motion and reduced motion. New controls must extend these contracts. These checks catch covered regressions; they cannot universally judge aesthetics or prove native desktop materials.

## Responsive layout contract

The native window keeps its 800×560 minimum, while browser layouts remain usable down to 320px width. At or below the central small breakpoint (600px), navigation becomes a modal drawer and content uses the full available width. Opening it traps focus; Escape, an outside click or choosing a page dismisses it. Theme menus stay in the drawer's native modal layer. Resizing preserves the desktop sidebar preference rather than turning a narrow-screen temporary choice into a desktop setting.

Content panes scroll independently, specimen collections wrap, and overlays fit both viewport width and height. Never hide overflow to mask inaccessible controls or horizontal layout failures. Future chat tabs, graphs and split panes must collapse or scroll deliberately at narrow widths without shrinking the primary content into a sidebar-sized column. New screens must pass the responsive browser matrix at 320, 480, 600, 800 and 1200px, including a short viewport, both themes, keyboard dismissal and reduced motion. Geometry, breakpoints and animation continue to come from the shared tokens.

## Horizontal conversation tabs

Conversation tabs sit above the workspace content. This deliberately adapts
Arc's quiet organization to a horizontal strip, leaving the sidebar as
navigation. Selected tabs have a restrained neutral surface; inactive tabs
remain quiet until hover or keyboard focus. Pinned tabs lead the strip and
groups remain contiguous with a compact disclosure label and count. A
collapsed group retains its active tab so the visible selection is honest.

Tabs support named right-click actions and the same shared themed overflow
menu. Arrow keys, Home and End navigate the strip; keyboard context menus
must be usable without a pointer. New tab and All tabs stay outside the
scrolling region so they remain reachable on narrow screens. Reordering
needs a menu alternative to dragging. Avoid idle animation and content
flourishes: group disclosure communicates state and shared surface motion
communicates menu entry. Respect reduced motion throughout.

The current workspace stores local drafts as a UI preview. Switching tabs
preserves drafts; closing a view does not mean cancelling a task. Future
engine integration must keep that distinction. No preview may fabricate AI
messages, progress or task execution. Document local storage as local UI
state, not cross-device synchronization.

Organization references: Arc's [pinned tabs](https://resources.arc.net/hc/en-us/articles/19231060187159-Pinned-Tabs-Tabs-you-want-to-stick-around)
and [folders](https://resources.arc.net/hc/en-us/articles/19228419623447-Folders-Stash-Similar-Tabs-Together).

## Chrome hierarchy and many-tab behavior

Workspace has one top row: the tab strip and fixed workspace actions. Do not
add a second Workspace title/search row. The sidebar already names the view;
command search remains available through its address action and Cmd/Ctrl+K.
The sidebar visibility control joins the strip only when navigation is hidden.
Keep unused native chrome draggable and actual controls outside drag regions.

Keep tabs readable within the central minimum/maximum widths and scroll only
the strip as capacity grows. Never squeeze every tab to fit or hide overflow
behind clipped content. Reveal selection automatically; keep New tab and All
tabs reachable independently. Compact pinned tabs, group disclosure and a
scrollable overview provide complementary ways to manage many tabs.

Hover previews are delayed and noninteractive; they never steal focus or
block selection. Preview content reflects the local draft, not fabricated
conversation output. Ctrl+Tab traverses recently used tabs with an explicit
selection preview; modifier release commits and Escape cancels. The overview
uses the same theme, shared menu controls and reduced-motion behavior as the
rest of the interface. All essential actions remain possible without hover.

## Quiet overflow and overview

The tab strip has no exposed scrollbar. Native horizontal trackpad movement
remains available, with small direction controls appearing only when more tabs
exist offscreen. Do not intercept vertical wheel movement or animate tab
selection into a long travel. Active tabs reveal themselves immediately.
Readable widths take precedence over fitting every tab at once.

The overview is one compact toolbar and one flat preview grid. Show each title
once, beneath the preview; keep group metadata subdued. Remove repeated group
headings, status captions, faux window chrome and decorative colored cards.
Search uses an understated focus underline. Only selection earns a persistent
preview border. Organization actions appear on hover/focus and stay visible
for coarse pointers. The preview contains actual draft text or a quiet empty
state, never fabricated conversation content.
