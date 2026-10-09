# codeaf repository contract — all contributors and agents

These rules supplement the parent repository contract and apply to every change under desktop/, including nested features. Preserve the approved minimal Arc-like design. A new screen is not permission to introduce a new visual language.

## 1. Read before changing UI

Read `docs/DESIGN.md` and inspect the live Design system screen. Reuse existing primitives in `src/components/ui/`. Check the smallest supported desktop window and both Light and Dark appearance. Honor explicit user instructions; record intentional design changes in the standard rather than creating hidden exceptions.

## 2. Use the single sources

- `src/design/tokens.json` owns colors, spacing, dimensions, radii, type sizes/weights/leading/tracking, icon stroke/motion, shadows, native window geometry and the monochrome brand mark.
- `src/styles/tokens.css`, favicon/mask SVGs and native icon source are generated. Do not hand-edit them. Change the source and run `npm run design:generate`; run `npm run brand:icons` when changing the brand.
- `src/styles/ui.css` and `src/components/ui/` own reusable control appearance and behavior. Screen CSS owns layout only and uses existing token references.
- `src/design/ThemeProvider.tsx` owns appearance persistence, system-theme resolution, native-window appearance and reduced motion. Do not reimplement these per screen.
- `src/design/index.template.html` owns the HTML shell. Native Tauri config owns permissions/build/bundling; its main-window geometry is generated from design tokens.

## 3. Non-negotiable design rules

Use shared Button, IconButton, NavigationItem, SidebarAction, TextInput, TextArea, Select, ThemeSelect, ContextMenu, DropdownMenu, HoverPreview, Typography, Surface, Separator and KeyboardShortcut components. Add or extend a shared primitive when needed; do not clone one into a feature. No inline styles or arbitrary CSS color/length/type/opacity values. The only fixed media-query thresholds are the declared breakpoints, checked against tokens. Proportional layout values such as percentages, fractions and viewport units are permitted.

Lucide outline through `src/components/ui/Icon.tsx` is the ONLY UI icon family. The implementation is `@animateicons/react` with explicit per-icon imports. Add semantic names in that central registry. No direct icon-package imports elsewhere; no mixed Heroicons/Tabler/Phosphor/Hugeicons, emoji icons, icon fonts, inline hand-drawn SVGs or per-screen stroke overrides. Icons use currentColor, the declared size roles and one central stroke weight. Only the shared BrandMark may render our own SVG artwork. The shared DependencyMap may render measured SVG paths for data connections, never icons; graph nodes remain semantic DOM controls.

Motion must communicate a state change. Navigation, favorite, search, settings and plus glyphs stay still. Only explicitly opted-in directional arrows translate; disclosure chevrons rotate with open state. Durations, curves and keyframe distances come from tokens; shared keyframes live in ui.css. Sidebar collapse uses the central layout transition and makes hidden controls inert. Reduced motion removes interpolation while preserving instant state indicators. No idle loops, decorative morphs, per-screen keyframes or upstream default icon animations. Every interactive control must implement themed hover, pressed, focus-visible and disabled states; persistent selection uses aria-current or aria-pressed. Use shared Select for choices and shared ContextMenu/DropdownMenu for action menus and HoverPreview for delayed tab previews. Native HTML select and direct Radix imports outside those shared boundaries are prohibited. Icon-only controls require an accessible name. Keep visible keyboard focus, modal focus restoration, native window controls and drag regions. Never simulate Mac traffic lights on Linux. Avoid promotional hero cards, gradients within content, large saturated navigation states, unnecessary borders or decorative UI. Preserve macOS native sidebar material and Linux's honest tinted fallback.

## 4. Required gate

Run commands from desktop/ (or use npm --prefix desktop). Run `npm run check` before committing: it checks generated assets, design policy, TypeScript/frontend build, policy regression tests and Go contracts. Do not bypass, silence or weaken `design:check` to make a feature pass. A deliberate standard change must update tokens/components, documentation and the live specimen together; explain the reason in review.

For UI changes also run `npm run test:ui` in Chromium and WebKit. Extend the reusable theme/accessibility contracts for new controls; verify Light, Dark, System, reduced motion, keyboard navigation and control states. Do not suppress accessibility failures or treat these checks as a substitute for visual review.

For native changes also run `npm run engine:build` and `cargo fmt --check --manifest-path src-tauri/Cargo.toml`, then validate native compilation on macOS and Linux. Browser screenshots do not prove native materials or sidecar behavior. CI calls the same gate; unavailable CI must be reported honestly. The root `CLAUDE.md` imports this contract so Claude-based contributors follow the same rules.

## 5. Architecture and evidence

Stack: Tauri 2 shell, React/TypeScript UI, Go sidecar. Rust stays a narrow typed native bridge. Do not grant renderer arbitrary shell access. Explicit engine attachment runs the canonical engine. Unconnected tabs and named fixtures remain previews; never depict them as live sessions. Keep screenshots and recordings outside the source tree; attach review evidence directly to the relevant PR or report the attachment blocker. Production brand icons are app assets, not review evidence. Keep private infrastructure details out of public review content.

## Shared engine and branch policy

The root codeaf repository owns the real session engine, storage, model calls,
task splitting, prompts and harnesses. The desktop engine/ folder is a historical health placeholder and is not packaged.
Do not extend it into an independent AI implementation.
Packaging must use the canonical root make build output and preserve
its packed manuals, furrow runtime and build identity. Views attach to the
same persistent engine; closing a tab detaches, while Stop explicitly cancels.

This public integration branch is work/8f3c2a9d. Its name is not a privacy
boundary. The user explicitly requested no PR for this integration branch.
Preserve dev/main/staging; merge upstream/dev into this branch rather than
rebasing or force pushing shared history. Never copy secrets, evidence, native
build directories or private machine instructions into source commits.

## Responsive layout contract

Responsiveness is mandatory. Preserve the native 800 x 560 minimum while
supporting browser widths from 320px to wide desktop windows. At the declared
small breakpoint use the shared navigation drawer, not a squeezed sidebar.
Keep every control reachable; deliberate vertical scrolling and wrapping are
allowed, horizontal page overflow and clipped actions are not. Restore focus
on dismissal and honor reduced motion. Use centralized breakpoints and
geometry tokens. Verify Light and Dark at 320, 480, 600, 800 and 1200px,
including short-height windows and nested themed menus. New screens must
extend the responsive browser contracts rather than assume desktop width.

## Tab workspace contract

Tabs belong in the horizontal strip above workspace content. Keep the sidebar
for navigation; do not duplicate conversation tabs there. Use the shared menu
primitives for right-click and overflow actions, with keyboard equivalents.
Selected tabs use aria-selected and a roving keyboard focus; grouping uses
an explicit disclosure control. Keep the selected tab visible when its group
collapses. Pinning and grouping must have predictable ordering and names.

Closing a tab detaches its view; it must never silently stop engine work.
Unconnected tabs remain local previews. Explicit Connect engine attaches a persistent
canonical session; preserve drafts, model identity and real history across tab switches
and reloads. Never invent assistant responses or running statuses. Engine attachment
must remain distinct from named samples. Persisted UI state requires validation and safe fallback.

Only the strip may scroll horizontally. New-tab and overflow actions remain
reachable at 320px; every hover action needs a keyboard or menu equivalent.
Reuse theme tokens for tab geometry, selected/hover/pressed states, group
markers and motion. Never add per-tab colors or feature-local keyframes.
Extend browser coverage for tab isolation, close/reopen, grouping, keyboard
menus, persistence and narrow-screen overflow before publishing changes.

Workspace chrome must remain one row. Do not restore a duplicate page title or
command-search row above tabs. Large tab collections scroll at readable widths;
New tab and overview controls stay fixed. Preserve held-modifier MRU switching,
Escape cancellation, delayed noninteractive hover previews and accessible
scrollable overview cards. Preview text must come from the actual local draft.
All overview organization actions use shared menus and remain reachable by
keyboard. Verify nested menu dismissal restores focus to its owning overview.

Keep tab overflow quiet: no exposed scrollbar, no unreadable slivers, and no
vertical-wheel hijacking. Direction controls appear only when needed; all
content stays reachable through scrolling, keyboard navigation or overview.
The overview has one compact toolbar and a flat grid with one title per tab.
Do not reintroduce duplicate headings/captions, faux window chrome or decorative
card colors. Organization actions require hover, focus and coarse-pointer
access. New tabs and overview actions must remain visible at 320px.

Platform shortcut labels and matching helpers belong in src/design/keyboard.ts.
Use Command for Mac tab creation/closing/reopening and Control for Linux.
Recent-tab switching remains Control+Tab on both platforms; Command+Tab must
remain available to macOS. Menu hints must describe the host platform.

## Work document and composer contract

Use the final Tab States wireframe hierarchy through the existing design system.
Tabs contain continuous work documents, not chat bubbles or repeated message cards.
A fresh tab has a usable hairline instruction input without a hero. The product owns
ask rows, section spacing, folding controls and the footer; document content uses
shared typography and semantic structure. No raw wireframe colors, icons, fonts,
gradients or arbitrary geometry may bypass the central design rules.

Folded sections show ask → digest. Result disclosure and original-instruction
visibility are independent, named keyboard controls with honest expanded states.
Literal instructions and steering amendments use the shared quotation bar when
requested; live steers remain footnotes and folded sections retain amended counts.
Do not manufacture an AI title or digest for custom local submissions.

Use tab-local Auto/Fast/Thorough presets with separate cycle and picker actions,
shared searchable model/effort selection and preserved draft/focus. Presets are UI
fixtures until canonical engine routing exists; do not invent provider availability,
latency, automatic routing or prices. Suggestions are not pending decisions. Answer
controls require an explicit pending question and honest acknowledgement.

Continue, Steer, New section, Queue and Stop remain distinct. Enter continues/steers;
Shift+Enter adds a line; Command+Enter on Mac or Control+Enter on Linux starts a new
section. Queue stays a distinct menu action; Mac Control+Enter may queue. Shortcut
matching and labels live in keyboard.ts. Preserve IME input and drafts, dismiss
overlays before Escape may stop a working preview, and keep a named Stop button. Map future operations to
the root engine deliberately rather than treating these UI labels as engine APIs.

Named sample documents, working state and decisions must remain explicitly labeled
UI previews. Local submissions and sample choices never run AI or fabricate replies.
Preserve persisted tab drafts and reading context, validate storage, and extend
fold/unfold, literal-word recovery, steering, decision, preset, narrow-window and
accessibility coverage before publishing. Geometry/motion remain centralized; no
feature-local keyframes or decorative idle animation.

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

At the start of each development session and before completion, fetch upstream dev
and check whether it is an ancestor of HEAD. Incorporate new dev commits on the
integration branch using a normal merge; preserve local edits, never reset or force
push. If changes overlap unfinished edits, checkpoint or isolate them before merging.
Run the affected upstream checks and desktop gates after syncing. Do not create a PR
when the task explicitly requests branch-only development.

## Surface hierarchy and compact tab controls

Use surface for document paper and overlay-surface for every transient menu, preview, picker and dialog. Tab-strip frames the document; the selected tab joins its paper without an accent outline. Keep these roles centralized in tokens.json and visible in the design specimen. Close is inside the tab, revealed on hover or keyboard focus and always available on coarse pointers; it never adds a separate flex slot or changes tab width.

Overview uses document paper inside the raised overlay. Show real tab-local draft or latest section content, with an honest empty state. Selection uses a quiet Current label rather than a focus-like card outline. Keep two-tab overviews compact, sizing by total count so filtering does not resize the dialog. Preserve named keyboard focus separately from selection, theme/accessibility contracts, and reduced motion.

Completion stays quiet: use the shared monochrome check in muted text, without a green fill, badge, circle or animation. Its accessible label and preview say Completed. Reserve warning/error color for conditions needing attention; do not recolor success across navigation.

Tab entry fades without scaling or changing its hit area. When the strip overflows, keep both direction controls mounted and disable unavailable directions; removing controls during scrolling changes the viewport. Preserve stable scroll geometry and verify immediate scroll after creating tabs in WebKit.

## Canonical live attachment contract

Connect engine is explicit; no new live session or provider call runs on opening an unconnected tab. Connected tabs use the canonical authenticated loopback transport, persistent sessionFile and exact fixed model deepseek/deepseek-v4.1-flash for every role. Model presets remain preview-only until real routing is implemented; live tabs show the actual fixed model. History, tool steps and system notes come from canonical records, never invented optimistic replies. Keep replay sequence IDs, drafts on rejected writes, and status updates for inactive open tabs. Detaching a stream never sends Stop. Approval/question answers are not wired: explain the TUI requirement, preserve drafts and block fake acknowledgements. Do not place provider keys or transport tokens in renderer storage, URLs, logs or source. Browser startup explicitly selects vite.config.ts; generated JavaScript must not shadow its authenticated proxy.

Group creation is named Create group and is distinct from group destination names. Default group names are unique; collapsed group labels accept drops and moving opens the destination. Extend grouping, engine attach/detach/replay and task inspector browser contracts in both Chromium and WebKit. Current plan display filters archived runs to avoid joining repeated task IDs from distinct stores; retain canonical IDs. Parent connectors are containment only. Waits supports prerequisite details, not inferred full typed graphs.

## Assistant Markdown contract

Every live assistant text block uses the shared Markdown primitive in components/ui/Markdown.tsx. Preserve the continuous work-document aesthetic: system typography, compact token spacing, restrained headings, readable lists and document tables; no chat bubbles or per-message card skins. Literal user instructions, tool arguments/output and engine notes remain literal text unless their canonical contract explicitly defines Markdown.

The shared renderer owns GFM parsing and all Markdown appearance in styles/markdown.css. Do not introduce another Markdown parser, per-screen renderer, syntax-color palette or hardcoded content geometry. Extend the shared primitive, centralized tokens, design specimen and browser coverage together. Long code lines wrap and table regions scroll deliberately within their own viewport; the document/page never gains horizontal overflow at 320px.

Assistant output is untrusted content. Raw HTML stays disabled; never enable rehype-raw, dangerouslySetInnerHTML, script content or model-provided event handlers. Links pass through the shared safe URL transform: explicit HTTP/HTTPS/mailto and local fragments only. Unresolved file/relative links remain readable text until a canonical file-opening bridge exists. Browser external links isolate their opener; native opening must use a narrow explicit platform bridge, never arbitrary schemes, shell commands or app navigation. Remote images render as named references without automatic fetching. Read-only task-list marks use the shared monochrome icon/theme language; they are not app approval or decision controls.

Verify Light/Dark, semantic GFM structure, partial streaming Markdown, URL/HTML safety, preserved literal words and 320px wrapping. Markdown completion marks stay monochrome. Never let generated checklist content answer an engine approval or change a task's canonical status.

Live history must preserve record order. Only named tool calls become activity rows;
raw tool-result messages must not produce duplicate unnamed or forever-pending calls.
Full result requests use canonical call IDs on explicit disclosure, never renderer
file paths. Preserve compact results on read errors. AI tab names come from the
canonical title subscription; explicit manual names always win. Plan errors and
unsupported decision forms must be visible, never silently replaced with empty data
or approval. Live status labels may not call real execution a sample.

## Conversation and task hierarchy workspace

The primary plan navigation is a quiet parent/child outline beside the work document. The outline describes containment, not a dependency diagram. Avoid node cards, connector canvases, stage columns and expanded metadata competing with the conversation. Use shared compact rows, still monochrome states, explicit branch disclosures and an on-demand filter. Dependencies remain secondary context with their canonical direction and feeds_into/blocks/suggests type; never convert Waits into parent relationships or invent missing edges.

Opening a task navigates the existing tab to that task's recorded conversation. Child tasks reuse the same shared work-document, ask rows, Markdown, folding, original-instruction disclosure and composer as the parent. Never replace a child conversation with a separate record viewer, card detail sheet or custom input. Each child owns its draft and reading/folding state; the parent draft stays separate. A background tab opened for the same captured task shares its task document under the originating conversation/task identity; do not clone divergent corrections. Tab route history and root drafts remain independent. Captured instructions/results may populate this interface only with honest provenance; local preview input must not be presented as a live correction. Keep a named root breadcrumb and explicit Back/Forward controls. Modifier-click opens a background task tab without changing the current task, losing the root draft or starting work. Route history, selected task, view choice and reading context belong to that tab and survive the supported reload path. Parent conversation steering and task-specific corrections must remain distinct; never forward a task-view instruction to the parent engine accidentally.

Wide windows may show the hierarchy beside the document. Narrow windows switch one readable view at a time; selecting a task opens its conversation and returning to Plan keeps the task marked as current. Navigation and filtering must remain reachable by keyboard, preserve focus and obey shared reduced-motion rules. Plan updates never force navigation, open a view or steal focus. Counts use recorded leaves and do not count completed parents twice.

Prototype data is available only through explicit UI preview or a clearly identified preview link. Use authentic captured hierarchy, typed relationships and saved task text; label provenance and unavailable records honestly. Do not generate or reconstruct task conversations, fabricate completion, pretend fixture work is live or silently replace a failed engine read with sample data. Fresh tabs remain quiet. Test hierarchy disclosure/filtering, same-tab task journeys, Back/Forward/root recovery, background task tabs, root-draft preservation, tab isolation/reload, narrow view switching, Light/Dark and accessibility before publishing navigation changes.


## Semantic typography contract

Keep native system sans for prose and chrome and the central system monospace stack for code. Shared document prose, heading, code, label and metadata roles live in tokens.json and the shared typography/Markdown components; no screen-specific fonts, heading sizes or syntax palettes. Markdown owns its list, heading, table, quotation and code spacing without descendant overrides from feature styles. Inline code uses the shared CodeText role; fenced code retains literal whitespace with bounded narrow-window wrapping. JSON, shell output and tool results remain literal monospace; search queries, task names and navigation labels stay prose.

Formatting is explicit content metadata, never a regex guess. WorkSection.originalFormat defaults to literal: exact user instructions, amendments, pasted context and tool arguments must remain literal. A captured task-authored Description may opt into markdown through its known projection; show it as a task instruction and retain the original stored string. Unknown/invalid formats fail storage validation rather than enabling HTML. Assistant text and explicitly marked task descriptions use the single safe shared Markdown renderer, including inline code and fenced code; never add another parser. Verify prose/code font roles, heading hierarchy, real task excerpts, exact literal user words, Light/Dark and narrow wrapping together.
