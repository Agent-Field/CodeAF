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

Stack: Tauri 2 shell, React/TypeScript UI, Go sidecar. Rust stays a narrow typed native bridge. Do not grant renderer arbitrary shell access. The first send attaches the canonical engine; fixtures exist only in tests and the Design system specimen and are never depicted as live sessions. Keep screenshots and recordings outside the source tree; attach review evidence directly to the relevant PR or report the attachment blocker. Production brand icons are app assets, not review evidence. Keep private infrastructure details out of public review content.

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
New tabs are quiet, local and make no engine call. The first send creates the
canonical session and saved tabs reattach automatically; preserve drafts, model
identity and real history across tab switches and reloads. Never invent assistant
responses or running statuses, and never show fixtures or samples in the workspace.
Persisted UI state requires validation and safe fallback.

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

## Conversation and composer contract

`docs/CHAT.md` is the authoritative conversation spec; it replaces the retired work-document wireframe. A conversation reads like natural chat with sectional folding: a centred reading column (chat-column-max-width), right-aligned literal user bubbles, plain assistant prose, no header row. Each turn folds from a gutter chevron (button with aria-expanded, always visible on coarse pointers); a folded turn shows the user message and a one-line digest and unfolds on click. Auto-scroll only when the reader is at the bottom, otherwise show the Latest pill ("Latest · Working 1m 13s" while the engine works). Long histories fold: beyond 12 turns the oldest sit in one earlier-turns group, ⌘↑/⌘↓ step between turns, Esc returns to the latest, and the "Earlier messages summarized" divider draws only after an engine compaction. Do not manufacture a digest or title in the UI.

Choice cards in the tray tag the engine's pick "Suggested" and show the countdown and Hold only when the question has a deadline; a batch of permissions is one card ("Allow N actions?") with a One by one pager.

Accessibility deviation from the design: where axe flags `ink-3` text as under WCAG AA (4.5:1; the designed ink-3 is about 3.6:1), the text is drawn in `ink-2` (ghost Button labels, the model label, tray crumb and foot, clarification links). Never loosen the axe checks to keep the designed colour; see docs/COMPONENTS.md.

Tool calls are one quiet line per group ("Worked · 3 steps", or the live step while running) that expands to one row per step with args and output in capped monospace and explicit Show full output. Failed steps color only the mark. Thinking is one muted expandable line. Task notices are compact rows that open the task. Errors are inline with Retry that resends the last message. No machine-describing labels ("Engine connected", "Original instruction", "UI preview", "sample").

The composer is one rounded field (radius-composer) with autosizing textarea (1 to 8 lines, then it scrolls under a top fade; a paste over 12 lines becomes a card and is sent as a `<pasted-text>` block), Attach and the model label on the left, Send on the right. Empty conversations show "What are we building?" with the composer centred under it and dock it after the first send using shared layout motion. While working: Send becomes Stop when empty; with text, a Queue button (Alt/⌥ Enter) sits beside an accent Steer pill and Enter steers. There is no send-options menu. Enter sends, Shift+Enter adds a line, IME composition never sends, Escape closes menus then blurs, Up in an empty field recalls the last message. Drafts persist per tab and per task route. Shortcut matching and labels live in keyboard.ts. Keep a named Stop control. Never invent attachments, resumable jobs, activity history, provider availability, latency or prices. Replaced and removed: the UI preview selector and named samples (fixtures live only in tests and the Design system specimen, marked Specimen), Auto/Fast/Thorough presets (fixed model label), ghost suggestion pills, the Original instruction disclosure, and the New section button.

Tab identity is stable across execution steps. Keep a clear active surface, medium label weight and a tab joined to the content surface; selection and work status are separate. The tab label is title only, with one still, named state indicator at the leading edge (working, needs you, failed); completed shows nothing extra. No decorative spinner or invented percentage. Delayed hover and keyboard previews show the full title, at most one two-line draft or summary, and one status/time line; omit empty paragraphs. Do not put timestamps, changing step titles or badges across the tab strip.

Tab indicators survive switching, closing/reopening and reload. Activity timestamps change on meaningful state events, not scrolling, focus or opening a menu. The active tab has upper corners only, the pane's surface and no persistent accent outline. Tab changes may fade and settle the conversation with shared document-enter motion, without scaling text or resetting the composer. Pointer focus on the input uses a quiet border; never remove keyboard focus.

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

There is no Connect button. A new tab is quiet and makes no engine or provider call on open; the first send creates the session, then sends. Saved tabs reattach their persisted sessionFile automatically. If the engine is unreachable the composer shows one muted line with Retry and keeps the draft. No provider call happens before the person sends. The model is fixed: exact deepseek/deepseek-v4.1-flash for every role, shown as a muted label (a Select only once real routing exists). The one separate AI call is the engine's title lane (asynchronous, after the first message, delivered through the snapshot title); the UI makes no AI call of its own and digests are the first line of the final answer.

Use the canonical authenticated loopback transport. History, tool steps and system notes come from canonical records, never invented optimistic replies. Keep replay sequence IDs, drafts on rejected writes, and status updates for inactive open tabs. Detaching a stream never sends Stop. Pending questions and approvals appear as one paged tray card above the composer (a 40px compact bar while the reader is scrolled away) and are answered only through the engine answer endpoint with honest acknowledgement. Do not place provider keys or transport tokens in renderer storage, URLs, logs or source. Browser startup explicitly selects vite.config.ts; generated JavaScript must not shadow its authenticated proxy.

Group creation is named Create group and is distinct from group destination names. Default group names are unique; collapsed group labels accept drops and moving opens the destination. Extend grouping, engine attach/detach/replay and task panel browser contracts in Chromium and WebKit. The task panel lists every task the conversation ran, ended runs included, each canonical ID once (the newest record wins, so a repeated ID from an older store never joins a second row); retain canonical IDs.

## Assistant Markdown contract

Every live assistant text block uses the shared Markdown primitive in components/ui/Markdown.tsx: plain prose at column width, system typography, compact token spacing, no per-message card skins. User messages are literal text in a soft bubble (`pre-wrap`, never Markdown, long ones clamp with Show more); tool arguments/output and engine notes also stay literal unless their canonical contract defines Markdown.

The shared renderer owns GFM parsing and all Markdown appearance in styles/markdown.css. Do not introduce another Markdown parser, per-screen renderer, syntax-color palette or hardcoded content geometry. Extend the shared primitive, tokens, design specimen and browser coverage together. Long code lines wrap and tables scroll within their own viewport; the page never gains horizontal overflow at 320px.

Assistant output is untrusted content. Raw HTML stays disabled; never enable rehype-raw, dangerouslySetInnerHTML, script content or model-provided event handlers. Links pass through the shared safe URL transform: explicit HTTP/HTTPS/mailto and local fragments only. Unresolved file/relative links remain readable text until a canonical file-opening bridge exists. Browser external links isolate their opener; native opening must use a narrow explicit platform bridge, never arbitrary schemes, shell commands or app navigation. Remote images render as named references without automatic fetching. Read-only task-list marks use the shared monochrome icon/theme language; they are not approval controls and generated checklist content never answers an engine approval or changes a task's status.

Verify Light/Dark, semantic GFM structure, partial streaming Markdown, URL/HTML safety, literal user words and 320px wrapping.

Live history preserves record order. Only named tool calls become step rows; raw tool-result messages must not produce duplicate unnamed or forever-pending calls. Full results are requested by canonical call ID on explicit disclosure, never by renderer file path; keep compact results on read errors. Tab names follow the engine title; a manual name always wins. Plan errors and unsupported question forms are visible, never silently replaced with empty data or approval.

## Conversation and task hierarchy workspace

`docs/CHAT.md` section 5 is authoritative. Tasks live in a right task panel, present only when the conversation has tasks: one 30px row per task with a status mark drawn by the shared StatusMark (through TaskMark), title, counts and duration; nesting by indentation, explicit branch disclosure and 1px hairline guide lines joined to open parents. No node cards, canvases, stage columns or graph. Expand tasks opens the expanded tasks view (route `#tasks`, a tab-local stop for Back, Forward and reload) with a table and a detail pane. Dependencies stay secondary context with their canonical direction; never turn Waits into parent relationships or invent edges. Counts cover every visible non-archived row, parents included, and count each row once.

Opening a task (row or notice) navigates the tab to a task view: the same conversation components drawn from the task page (Description as the message, Steps as a tools group, Result as the reply, Checks, child task notice rows, Notes). Never use a separate record viewer, detail sheet or custom input. The breadcrumb exists only in task view; each segment is a link, with Back/Forward, and modifier-click opens a background tab. Route history, selected task and reading context belong to the tab and survive reload. The task view composer is disabled and returns to the main conversation; never forward a task-view instruction to the parent engine. Plan updates never force navigation or steal focus. Narrow windows turn the panel into an overlay sheet. Use authentic captured hierarchy and saved task text; never reconstruct task conversations, fabricate completion or replace a failed read with sample data. Test panel disclosure, task journeys, Back/Forward, background tabs, draft preservation, reload, narrow widths, Light/Dark and accessibility.


## Semantic typography contract

Keep native system sans for prose and chrome and the central system monospace stack for code. Shared document prose, heading, code, label and metadata roles live in tokens.json and the shared typography/Markdown components; no screen-specific fonts, heading sizes or syntax palettes. Markdown owns its list, heading, table, quotation and code spacing without descendant overrides from feature styles. Inline code uses the shared CodeText role; fenced code retains literal whitespace with bounded narrow-window wrapping. JSON, shell output and tool results remain literal monospace; search queries, task names and navigation labels stay prose.

Formatting is explicit content metadata, never a regex guess. User messages, steering text, pasted context and tool arguments are literal; a captured task-authored Description may opt into markdown through its known projection while the original stored string is retained. Unknown/invalid formats fail storage validation rather than enabling HTML. Assistant text and explicitly marked task descriptions use the single safe shared Markdown renderer, including inline code and fenced code; never add another parser. Verify prose/code font roles, heading hierarchy, real task excerpts, exact literal user words, Light/Dark and narrow wrapping together.
