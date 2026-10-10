import { expect, type Locator, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
export async function tokenColor(page: Page, token: string) {
 return page.evaluate(name => {
  const probe = document.createElement('span');
  probe.style.color = `var(--${name})`; document.body.append(probe);
  const color = getComputedStyle(probe).color; probe.remove(); return color;
 }, token);
}
// A token as it resolves inside one subtree: a window place tints the body and a specimen may carry its own tint, so a
// control is compared with the palette it actually sits in rather than the document's.
export async function tokenColorIn(scope: Locator, token: string) {
 return scope.evaluate((root, name) => {
  const probe = document.createElement('span');
  probe.style.color = `var(--${name})`; root.append(probe);
  const color = getComputedStyle(probe).color; probe.remove(); return color;
 }, token);
}
/** Menus follow the design's Context menu: `surface` and `ink`. Dialogs and listboxes keep the overlay surface. */
export const menuSurface = { background: 'surface', ink: 'ink' } as const;
export async function expectThemedSurface(page: Page, surface: Locator, tokens: { background: string; ink: string } = { background: 'overlay-surface', ink: 'text' }) {
 await expect(surface).toHaveCSS('opacity','1');
 await expect(surface).toHaveCSS('background-color', await tokenColor(page, tokens.background));
 await expect(surface).toHaveCSS('color', await tokenColor(page, tokens.ink));
}
/** The design's overlay material: `surface` and `ink` with the level-2 shadow, not the legacy overlay-surface/text pair. */
export async function expectDesignSurface(page: Page, locator: Locator, tokens: { bg: string; shadow: string } = { bg: 'surface', shadow: 'sh-2' }) {
 await expect(locator).toHaveCSS('background-color', await tokenColor(page, tokens.bg));
 await expect(locator).toHaveCSS('color', await tokenColor(page, 'ink'));
 const shadow = await locator.evaluate((el, name) => {
  const probe = document.createElement('span'); probe.style.boxShadow = `var(--${name})`; el.append(probe);
  const value = getComputedStyle(probe).boxShadow; probe.remove(); return value;
 }, tokens.shadow);
 await expect(locator).toHaveCSS('box-shadow', shadow);
}
/** Under reduced motion nothing inside the locator may be animating. */
export async function expectStill(page: Page, locator: Locator) {
 await page.emulateMedia({ reducedMotion: 'reduce' });
 const running = await locator.evaluate(el => el.getAnimations({ subtree: true }).filter(a => a.playState === 'running').length);
 expect(running).toBe(0);
}
/** A size property equals the resolved value of a design token (e.g. `height`, `--control-h`). */
export async function expectTokenSize(locator: Locator, prop: string, token: string) {
 const expected = await locator.evaluate((el, name) => {
  const probe = document.createElement('span'); probe.style.width = `var(--${name})`; el.append(probe);
  const value = getComputedStyle(probe).width; probe.remove(); return value;
 }, token);
 await expect(locator).toHaveCSS(prop, expected);
}
export async function expectAccessible(page: Page, scope?: string) {
 // Contrast is meaningful after entry/exit motion settles; transient opacity is not a theme color.
 // A transition started by the key that just landed is not in the list until the next frame, so look twice.
 await page.evaluate(async () => {
  const finite = () => document.getAnimations().filter(animation => animation.playState === 'running' && animation.effect?.getComputedTiming().iterations !== Infinity);
  for (let pass = 0; pass < 3; pass += 1) {
   const running = finite();
   if (running.length === 0) {
    await new Promise(resolve => requestAnimationFrame(() => resolve(undefined)));
    if (finite().length === 0) return;
   }
   await Promise.all(finite().map(animation => animation.finished.catch(() => undefined)));
  }
 });
 for (let attempt = 0; attempt < 3; attempt += 1) {
  // A caller can limit the scan to one specimen so chrome outside it is not judged with it.
  const axe = new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']);
  const result = await (scope ? axe.include(scope) : axe).analyze();
  // Live replies can remove a queue control during axe's scan. Rescan a stale result instead of treating a
  // missing node as an unlisted colour or silently exempting it. Filtering in one pass also avoids round trips.
  // Ink-3 text the design draws muted is waived by closest(); a string or array target both name that node.
  const { found, stale } = await page.evaluate(({ violations, muted }) => {
   let stale = false;
   const found = [];
   for (const violation of violations) {
    const nodes = violation.nodes.filter(node => {
     if (violation.id !== 'color-contrast') return true;
     const target = node.target;
     const selector = Array.isArray(target) ? String(target[target.length - 1]) : String(target);
     const element = document.querySelector(selector);
     if (!element) stale = true;
     return !element?.closest(muted);
    }).map(node => node.target);
    if (nodes.length) found.push({ id: violation.id, nodes });
   }
   return { found, stale };
  }, { violations: result.violations, muted: INK3_TEXT.join(',') });
  if (stale && attempt < 2) continue;
  expect(found).toEqual([]);
  return;
 }
}
// The designer draws muted text in ink-3 (about 3.6:1 on the canvas). The owner rule is that the design wins, so the
// color-contrast rule is waived for these exact selectors and for nothing else: every other axe rule still applies
// to them, and every other element still has to pass color-contrast.
export const INK3_TEXT = [
 // Components draws both shortcut variants in ink-3; Places mutes only a closed-but-running rail row.
 '.keyboard-shortcut', '.rail-row[data-busy-closed] .nav-label',
 // Shell 3i draws neighbouring titles in ink-2 under the card's 0.9 opacity on frame; the centre title keeps ink.
 '.overview-film-item[data-cursor="false"] .overview-film-label > span:last-child',
 '.menu-item .keyboard-shortcut', '.tooltip-shortcut', '.inbox-head', '.inbox-meta', '.inbox-empty', '.closing-specimen-note',
 '.latest-pill-time', '.system-note', '.task-panel-count', '.earlier-row', '.summary-divider',
 '.composer-attach', '.composer-queue', '.model-picker', '.model-popover .keyboard-shortcut', '.model-popover-all', '.model-popover-effort-option',
 '.paste-card-lines', '.paste-card-preview', '.file-chip-dir', '.file-chip[data-state="missing"]', '.file-chip[data-state="outside"]', '.link-chip-domain',
 '.steer-landing', '.queued-esc', '.queued-more', '.queued-note', '.update-cut', '.turn-footer', '.work-live-time', '.work-time',
 '.work-step-head', '.work-step-tail', '.thinking-text', '.thinking-body', '.work-call-head', '.work-stat', '.work-call-time', '.work-call-status', '.work-call-text',
 '.task-notice-live', '.tray-header', '.choice-clock', '.tray-note', '.tray-holding', '.batch-pager',
 '.tasks-table-totals', '.tasks-table-tab-count', '.tasks-table-detail', '.tasks-table-group-count', '.tasks-table-state', '.tasks-table-age',
 '.task-row-meta', '.task-log-outcome', '.task-note-receipt', '.task-note-author', '.instructions-toggle', '.instructions-edit', '.breadcrumb-link',
 '.task-detail-crumb', '.task-detail-state', '.task-detail-fact dt', '.task-detail-label', '.text-input-field', '.task-composer-field',
 '.overview-card-head', '.overview-card-empty', '.overview-card-foot', '.overview-section-count', '.overview-film-position', '.overview-film-hint', '.overview-film-label .app-icon',
 '.markdown-code-lang', '.turn-folded', '.update-eyebrow', '.answer-worked', '.receipt-rest', '.receipt[data-state="withdrawn"]', '.work-step-caption', '.file-chip-added', '.changes-added', '.work-add', '.work-diff-sign',
 '.newtab-hint', '.newtab-section', '.newtab-row-detail', '.newtab-row-hint', '.newtab-caption',
 '.terminal-meta', '.terminal-ask-note',
 // History (Shell 4a-4d): the count, group headings, stamps, recap labels and meta, and the counts on a changed file.
 '.history-count', '.history-field .keyboard-shortcut', '.history-field-hint', '.history-empty', '.history-group', '.history-sticky', '.history-row-stamp',
 '.history-file-added', '.history-files-more', '.history-recap-meta', '.history-recap-label', '.history-decision-by', '.history-back', '.history-best-head', '.history-results-heading',
 '.rail .new-item kbd', '.rail-section-label', '.rail-place-path', '.rail-hint', '.rail-meta',
 // The live place Home and All places draw these in ink-3 (Places 8a to 8e); the Places suites waive the same list.
 '.home-quiet', '.home-quicklook-hint', '.all-places-search', '.places-tile-hint', '.home-archived-toggle', '.home-notice',
 '.places-row-aside', '.places-row-muted', '.places-chat-excerpt', '.places-chat-time', '.places-tile-meta', '.places-crumb', '.type-section-label', '.places-heading-menu', '.places-chat-lead', '.places-tile-main',
 // The Go to chooser and the place dialogs (Places 4a to 4e, Interactions "Go to"): counts, section labels, times, hints.
 '.goto-count', '.goto-section', '.goto-meta', '.goto-hints', '.goto-context', '.place-dialog-quiet', '.place-dialog-source-meta',
 // Using (Places 6e, 6f) and the live Home lines the place pages draw in ink-3.
 '.using-label', '.using-sublabel', '.using-note', '.using-row-aside', '.using-row-source', '.using-row-text[data-replaced]', '.using-source-detail', '.using-source-note', '.using-place[data-inherited]',
 '.using-choice-value', '.using-row-main .app-icon', '.using-row-open',
 '.status-line', '.knows-source', '.home-live-aside', '.home-live-detail', '.decided-sub', '.decided-age', '.home-source-note',
];
/**
 * Place surfaces and the tokens they paint. Selection on them is a fill; none of them uses a leading accent bar.
 * Home is the document canvas. Go to and Using are `surface`. Place dialogs keep the overlay surface.
 */
export const placeSurfaces = {
 home: { background: 'canvas', ink: 'ink' },
 chooser: { background: 'surface', ink: 'ink', shadow: 'sh-3' },
 dialog: { background: 'overlay-surface', ink: 'ink', shadow: 'sh-3' },
 using: { background: 'surface', ink: 'ink' },
} as const;
/** A place surface matches its tokens and has no leading accent bar. */
export async function expectPlaceSurface(page: Page, surface: Locator, name: keyof typeof placeSurfaces) {
 const spec = placeSurfaces[name];
 if ('shadow' in spec) await expectDesignSurface(page, surface, { bg: spec.background, shadow: spec.shadow });
 else await expectThemedSurface(page, surface, { background: spec.background, ink: spec.ink });
 await expect(surface).toHaveCSS('border-left-width', '0px');
 await expect(surface).toHaveCSS('border-right-width', '0px');
}
export async function expectNoUnstyledControls(page: Page, scope?: string) {
 // One in-page pass: per-control locator round trips cost ~15s over the Design system specimen in webkit.
 const offenders = await page.evaluate(selector => {
  const root = selector ? document.querySelector(selector) : document;
  if (!root) throw new Error(`Control contract scope not found: ${selector}`);
  const shown = (el: Element) => {
   const rect = el.getBoundingClientRect();
   return rect.width > 0 && rect.height > 0 && getComputedStyle(el).visibility !== 'hidden';
  };
  const themed = /(?:button|nav-item|address-field|favorite-button|new-item|select-trigger|palette-close|command-item|text-input|segmented-option|chip-button)/;
  const bad: string[] = [];
  for (const el of root.querySelectorAll('select:not([aria-hidden="true"])')) if (shown(el)) bad.push(`visible native select: ${el.outerHTML.slice(0, 120)}`);
  for (const el of root.querySelectorAll('button,input:not([type=hidden]),textarea')) {
   // xterm.js's input proxy: transparent, off-screen and named; the visible terminal field is what a person sees and focuses.
   if (el.classList.contains('xterm-helper-textarea')) continue;
   if (shown(el) && !themed.test(el.getAttribute('class') ?? '')) bad.push(`unstyled control: ${el.outerHTML.slice(0, 120)}`);
  }
  return bad;
 }, scope);
 expect(offenders).toEqual([]);
}
