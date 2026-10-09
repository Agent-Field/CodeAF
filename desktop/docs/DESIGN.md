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

## Platform tab shortcuts

Mac uses Command+T for New tab, Command+W for Close tab, and
Command+Shift+T for Reopen closed tab. Linux uses the corresponding Control
chords. The recent-tab switcher uses Control+Tab on both platforms and
Control+Shift+Tab to reverse. Do not intercept Command+Tab: macOS owns it for
application switching. Hints originate in src/design/keyboard.ts rather than
showing a combined Command/Control label on every platform.

References: [Apple Safari shortcuts](https://support.apple.com/guide/safari/cpsh003/mac)
and [Arc's recent-tab switcher](https://resources.arc.net/hc/en-us/articles/25619402657303-How-Do-You-Switch-Between-Tabs-Quickly-on-Arc-Desktop).

Mac also uses Command+Shift+[ / ] for adjacent tabs and Command+Shift+\ for
the overview, following Safari's horizontal-tab conventions. Linux uses
Control+PageUp / PageDown for adjacent tabs and Control+Shift+A for overview.

## Work document tabs

The final Tab States wireframe defines a continuous document rather than a stack
of chat bubbles. Adapt its hierarchy to the existing theme; do not copy its raw
colors, fonts, icons, gradients or arbitrary dimensions. The product owns the ask
row, section gaps, folding controls and input hairline. Result content owns its
normal document structure. Shared system typography, Lucide icons, control states
and central geometry apply to both.

A fresh tab is quiet with an immediately usable instruction line, without a hero
heading or promotional empty card. The input rests against one hairline and grows
only as the user writes. Context and model controls appear in the active composer;
model and effort use three tab-local presets: Auto, Fast and Thorough. Cycle the
preset from its label, or open its adjacent picker. Advanced model/effort choices
remain in the shared searchable picker. Presets in this scaffold are interaction
previews, not claims of automatic model routing, availability, prices or latency.

Completed sections fold to ask → digest. Opening the result is independent of
showing the original instruction. Literal user words and all steering amendments
appear behind the shared quotation bar only when explicitly requested. Live steers
are footnotes, and their count remains visible as amended ×n after folding. Do not
invent an AI ask or digest from an arbitrary submitted instruction. A locally staged
instruction stays visibly staged until the canonical engine supplies a result.

The footer presents work state, next-action suggestions or a pending question using
the same themed line. Suggestions and decision answers are separate actions; show
a choice only for the actual pending question, preserve its identity, and acknowledge
local preview selection honestly. Never fabricate progress, time estimates, results
or provider capability. Named Pricing research and Vendor reconcile samples are
explicitly labeled fixtures and do not run AI. New input, queues and sample state
remain local UI previews; unsent drafts retain the existing tab persistence contract.

Enter continues an idle document or steers its current work; Shift+Enter inserts a
newline. Command+Enter on Mac / Control+Enter on Linux starts a new section, replacing
the previous compact-composer Queue proposal. Queue remains a distinct explicit
menu action; Mac Control+Enter can queue. Command/Control+L focuses the instruction
line, Command/Control+Period cycles presets, and Command/Control+Shift+F folds all
sections. Shortcut platform
matching and hints live in keyboard.ts. IME composition never submits. Escape
dismisses overlays before stopping a working preview; Stop remains an explicit
button as well. These desktop
choices require deliberate mapping to canonical TUI operations during integration.

Preserve document reading position, focus and drafts when switching tabs or opening
menus. Disclosure motion uses central shared durations and reduced-motion behavior;
no decorative cursor blinking, gradient text or per-screen keyframes. All actions
remain usable by keyboard and at 320px browser width, including short windows and
long instructions. Test folding, verbatim recovery, steering, pending decisions,
model presets and tab-local isolation alongside shared theme/accessibility contracts.

The final input states are mandatory: resting is a hairline with a ghost suggestion
and Tab cue; focused-empty shows next-step pills without model/attachment controls;
typing reveals the split preset chip, Attach, outlined New section and primary
Continue/Steer. Fresh tabs keep Auto inline. First submission moves the box to the
bottom and thins it using duration-work and ease-layout, without scaling text.
Reduced motion docks immediately. Held shortcut hints are quiet text, not a card.

Tab identity is stable across execution steps. Keep a clear active surface, medium
label weight and a tab joined to the content surface; selection and work status are separate. Show
one still, named state indicator for receiving, working, waiting, completed, stopped
or failure. No decorative spinner or invented percentage. Delayed hover and keyboard
previews reveal the full title, latest section and a relative recorded-activity time.
Do not put timestamps, changing step titles or badges across the tab strip.

Workspace and document consume one persisted state: tab indicators must survive
switching, closing/reopening and reload. Activity timestamps change on meaningful
state events, not scrolling, focus or opening a menu. Samples say sample and marked
time rather than implying a real execution finished. Retry/Stop keep the draft and
partial result; retry samples make no request. A pointer click on the input uses a
quiet border; keyboard text focus uses the composer’s thin lower edge. Avoid a bright
box selection treatment and do not remove keyboard focus. Fresh tabs expose a real
file-picker action; never fabricate attachments, resumable jobs or activity history.

The active tab joins the document: upper corners only, same surface as the pane,
no persistent accent outline or underlined rounded pill. Its label remains medium
weight; keyboard focus remains distinct. Tab changes may fade and settle the document
with shared document-enter motion, without scaling text or resetting the composer.
Hover previews are a compact hierarchy: full title, at most one two-line draft or
named-work summary, then one status/time line. Omit generic staged-result messages,
redundant section labels and empty paragraphs. Hover is a quick orientation aid.

## Task plan direction

Use a per-tab, collapsible right inspector as the task-plan home. Default to a quiet
outline with parent/child connectors: needs-you items first, running work next,
completed branches folded. A small Plan action reopens it; never open on hover or
steal focus when work changes. Keep a consistent width while its data updates.
Narrow windows use an explicit drawer, preserving the document and input.

The inspector outline is containment, not a dependency graph. Show prerequisites
and dependents for the selected task; expand to a dedicated graph view for cross-
branch dependency exploration. Distinguish feeds_into, blocks and suggests rather
than rendering every link alike. Parent completion is not a second completed leaf.

A task row has a title and a still state. Hover/focus reveals its current step,
recorded time and wait reason; click opens an inline task detail in the inspector.
Expose result, artifacts, checks and notes progressively; open a task work view for
its full trajectory. Keep task-directed corrections distinct from chat steering.
Use completed leaf counts as counts, not invented duration/effort percentages.

Future integration must reuse session.PlanAgent and the canonical engine.
PlanTaskRow already supplies parent, waits, live step, timestamps and aggregate
counts. Full dependency types and stopped/paused/waiting flags require a faithful
read-only graph projection from plandb: Waits alone cannot reconstruct all edges.
Keep conversation/chat scoping and task identifiers intact. Team traffic remains a
separate source inside the same inspector when the selected conversation has it.
The right inspector and canonical read-only plan bridge are implemented. Current-run rows are shown; archived runs remain available through the TUI. The full graph remains a follow-up because Waits does not include every typed edge.

## Surface hierarchy and compact tab controls

Use surface for document paper and overlay-surface for every transient menu, preview, picker and dialog. Tab-strip frames the document; the selected tab joins its paper without an accent outline. Keep these roles centralized in tokens.json and visible in the design specimen. Close is inside the tab, revealed on hover or keyboard focus and always available on coarse pointers; it never adds a separate flex slot or changes tab width.

Overview uses document paper inside the raised overlay. Show real tab-local draft or latest section content, with an honest empty state. Selection uses a quiet Current label rather than a focus-like card outline. Keep two-tab overviews compact, sizing by total count so filtering does not resize the dialog. Preserve named keyboard focus separately from selection, theme/accessibility contracts, and reduced motion.

Completion stays quiet: use the shared monochrome check in muted text, without a green fill, badge, circle or animation. Its accessible label and preview say Completed. Reserve warning/error color for conditions needing attention; do not recolor success across navigation.

Tab entry fades without scaling or changing its hit area. When the strip overflows, keep both direction controls mounted and disable unavailable directions; removing controls during scrolling changes the viewport. Preserve stable scroll geometry and verify immediate scroll after creating tabs in WebKit.

## Connected conversations

Unconnected tabs retain the final wireframe as a local preview. Connect engine creates or attaches the canonical persistent session. The live chip says DeepSeek v4.1 Flash and exposes no pretend Auto/Fast/Thorough routing. Actual transcript user entries, assistant text, separated engine notes and progressively disclosed tool calls form the work document. Sending, steering, queuing and Stop cross the canonical typed transport; rejected sends preserve words. Closing only detaches. Background open tabs observe completion; reopening reads the same canonical sessionFile. Pending approvals/questions require the TUI until their answer protocol is implemented.

Plan is a quiet footer action. At widths up to the compact breakpoint it opens an explicit modal drawer; wider windows reserve a consistent right column. Selection pins details and suppresses the redundant hover preview. Escape closes the drawer and restores focus. Completed items use the shared muted check. Ready, Assigned, Running, holds, paused/waiting and stopped/interrupted remain distinct. Leaf counts avoid counting parents twice; canonical run counters label aggregate activity as active, not executing. The labeled task fixture remains exclusively under UI preview.

## Assistant Markdown

The shared Markdown primitive renders canonical assistant text as a continuous document. It uses react-markdown with remark-gfm for semantic headings, paragraphs, lists, quotes, links, fenced code and tables. Product chrome stays outside content; assistant output does not receive a card or bubble. Headings remain compact, body uses system sans, code uses system monospace, and all spacing/color/weight comes from the existing theme. Read-only task-list marks use a quiet monochrome check, independent of real task or approval state.

Long prose and code wrap within the document at 320px. Tables retain table semantics inside a named, keyboard-scrollable region when their structure requires extra width. Partial streaming Markdown is parsed from the accumulated actual text without a decorative typing animation or invented completion.

Raw HTML is disabled. HTTP/HTTPS/mailto links and local fragments pass the shared safe URL policy; unresolved relative/file paths remain text. External browser links isolate the opener. Native external-link opening requires the narrow platform bridge; the renderer never executes shell commands or arbitrary URL schemes. Images remain named references instead of fetching remote assets automatically. Literal instructions, engine notes and tool payloads preserve their original text.

Sources: [react-markdown](https://github.com/remarkjs/react-markdown) and [remark-gfm](https://github.com/remarkjs/remark-gfm).

## Canonical tool activity and live conversation identity

Render real assistant Markdown through the shared renderer. Preserve canonical text,
notes and named tool calls in record order. The history also carries raw tool-result
messages: these are not additional calls, and must not create unnamed pending rows.
Group consecutive calls behind a quiet activity disclosure; each call has its own
summary, arguments disclosure and bounded scrollable result. Fetch a full saved
result only on explicit expansion, using the canonical call ID rather than a path
supplied by the renderer. Errors retain the recorded summary and say what failed.

Use the canonical asynchronous AI title for engine-owned tab names. A manual rename
wins and remains stable across subsequent title updates and reloads. Plan stays an
explicit action with a task count; updates do not force the inspector open or steal
focus. Read failures show an error instead of an empty-plan claim. Live statuses never
say sample. Approved question answers use canonical identity and choice keys, wait
for acknowledgement, and preserve both drafts on failure; unsupported forms remain
read-only with the TUI route visible.

## Conversation and task hierarchy workspace

The hierarchy is a calm navigation rail beside the work document. It shows parent/child ownership with compact text rows, quiet state marks and explicit branch disclosure. It does not turn tasks into cards or make a dependency canvas the default. Filtering is available on demand. Typed prerequisites and dependents remain secondary task context, distinct from parent/child containment.

A normal task click opens its recorded conversation in the same tab. Parent and child use the same continuous work-document interface, including ask rows, folding, original instruction, Markdown and composer. The task identity changes; the interface language does not. Each child keeps its own draft and reading/folding state while the parent draft remains intact. A background tab opened for the same captured task shares its task document under the originating conversation/task identity; do not clone divergent corrections. Tab route history and root drafts remain independent. Recorded task excerpts are labeled as captures, and preview corrections never claim live execution. A named root breadcrumb and Back/Forward controls preserve orientation. Modifier-click opens a background task tab while keeping the current work in place. Route history, current task, drafts and reading context stay local to the tab. On compact screens, Plan and Conversation switch one readable view at a time; selecting a task moves into Conversation and returning to Plan retains its current marker. Updates never navigate automatically or steal focus.

The prototype uses authentic captured task rows and saved task text with visible provenance. Missing records are disclosed rather than reconstructed. It creates no AI request and does not claim captured work is running now. Fresh tabs remain fresh, preview links preserve existing engine attachments and drafts, and read failures do not substitute fixtures. Canonical task identity and relationship types remain intact; leaf counts avoid counting parents twice. All rows, controls, spacing, type and motion use the shared design system.


## Semantic typography contract

Keep native system sans for prose and chrome and the central system monospace stack for code. Shared document prose, heading, code, label and metadata roles live in tokens.json and the shared typography/Markdown components; no screen-specific fonts, heading sizes or syntax palettes. Markdown owns its list, heading, table, quotation and code spacing without descendant overrides from feature styles. Inline code uses the shared CodeText role; fenced code retains literal whitespace with bounded narrow-window wrapping. JSON, shell output and tool results remain literal monospace; search queries, task names and navigation labels stay prose.

Formatting is explicit content metadata, never a regex guess. WorkSection.originalFormat defaults to literal: exact user instructions, amendments, pasted context and tool arguments must remain literal. A captured task-authored Description may opt into markdown through its known projection; show it as a task instruction and retain the original stored string. Unknown/invalid formats fail storage validation rather than enabling HTML. Assistant text and explicitly marked task descriptions use the single safe shared Markdown renderer, including inline code and fenced code; never add another parser. Verify prose/code font roles, heading hierarchy, real task excerpts, exact literal user words, Light/Dark and narrow wrapping together.
