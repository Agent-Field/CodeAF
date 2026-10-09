import { expect, type Locator, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
export async function tokenColor(page: Page, token: string) {
 return page.evaluate(name => {
  const probe = document.createElement('span');
  probe.style.color = `var(--${name})`; document.body.append(probe);
  const color = getComputedStyle(probe).color; probe.remove(); return color;
 }, token);
}
export async function expectThemedSurface(page: Page, surface: Locator) {
 await expect(surface).toHaveCSS('opacity','1');
 await expect(surface).toHaveCSS('background-color', await tokenColor(page, 'overlay-surface'));
 await expect(surface).toHaveCSS('color', await tokenColor(page, 'text'));
}
export async function expectAccessible(page: Page) {
 // Contrast is meaningful after entry/exit motion settles; transient opacity is not a theme color.
 await page.evaluate(async () => {
  const animations = document.getAnimations().filter(animation => animation.effect?.getComputedTiming().iterations !== Infinity);
  await Promise.all(animations.map(animation => animation.finished.catch(() => undefined)));
 });
 const result = await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();
 const found = [];
 for (const violation of result.violations) {
  const nodes = [];
  for (const node of violation.nodes) {
   if (violation.id !== 'color-contrast' || !(await isDesignInk3(page, node.target))) nodes.push(node.target);
  }
  if (nodes.length) found.push({ id: violation.id, nodes });
 }
 expect(found).toEqual([]);
}
// The designer draws muted text in ink-3 (about 3.6:1 on the canvas). The owner rule is that the design wins, so the
// color-contrast rule is waived for these exact selectors and for nothing else: every other axe rule still applies
// to them, and every other element still has to pass color-contrast.
export const INK3_TEXT = [
 '.latest-pill-time', '.system-note', '.task-panel-count', '.earlier-row', '.summary-divider',
 '.composer-attach', '.composer-queue', '.model-picker', '.model-popover .keyboard-shortcut', '.model-popover-all', '.model-popover-effort-option',
 '.paste-card-lines', '.paste-card-preview', '.file-chip-dir', '.file-chip[data-state="missing"]', '.file-chip[data-state="outside"]', '.link-chip-domain',
 '.steer-landing', '.queued-esc', '.queued-more', '.queued-note', '.update-cut', '.turn-footer', '.work-live-time', '.work-time',
 '.work-step-head', '.work-step-tail', '.thinking-text', '.thinking-body', '.work-call-head', '.work-stat', '.work-call-time', '.work-call-status', '.work-call-text',
 '.task-notice-live', '.tray-header', '.choice-clock', '.tray-note', '.batch-pager',
 '.tasks-table-totals', '.tasks-table-tab-count', '.tasks-table-detail', '.tasks-table-group-count', '.tasks-table-state', '.tasks-table-age',
 '.task-row-meta', '.task-log-outcome', '.task-note-receipt', '.task-note-author', '.instructions-toggle', '.instructions-edit', '.breadcrumb-link',
 '.task-detail-crumb', '.task-detail-state', '.task-detail-fact dt', '.task-detail-label', '.button-ghost', '.text-input-field', '.task-composer-field',
 '.overview-card-head', '.overview-card-empty', '.overview-card-foot', '.overview-section-count', '.overview-film-position', '.overview-film-hint', '.overview-film-label .app-icon',
 '.markdown-code-lang', '.turn-folded', '.update-eyebrow', '.answer-worked', '.receipt-rest', '.receipt[data-state="withdrawn"]', '.work-step-caption', '.file-chip-added', '.changes-added', '.work-add', '.work-diff-sign',
 '.newtab-hint', '.newtab-section', '.newtab-row-detail', '.newtab-row-hint', '.newtab-caption',
 '.terminal-meta', '.terminal-ask-note',
 '.rail .new-item kbd',
 // History (Shell 4a-4d): the count, group headings, stamps, recap labels and meta, and the counts on a changed file.
 '.history-count', '.history-field .keyboard-shortcut', '.history-field-hint', '.history-empty', '.history-group', '.history-sticky', '.history-row-stamp',
 '.history-file-added', '.history-files-more', '.history-recap-meta', '.history-recap-label', '.history-decision-by', '.history-back', '.history-best-head', '.history-results-heading',
];
async function isDesignInk3(page: Page, target: unknown) {
 const selector = Array.isArray(target) ? String(target[target.length - 1]) : String(target);
 return page.evaluate(([css, list]) => Boolean(document.querySelector(css)?.closest(list)), [selector, INK3_TEXT.join(',')] as const);
}
export async function expectNoUnstyledControls(page: Page) {
 // One in-page pass: per-control locator round trips cost ~15s over the Design system specimen in webkit.
 const offenders = await page.evaluate(() => {
  const shown = (el: Element) => {
   const rect = el.getBoundingClientRect();
   return rect.width > 0 && rect.height > 0 && getComputedStyle(el).visibility !== 'hidden';
  };
  const themed = /(?:button|nav-item|address-field|favorite-button|new-item|select-trigger|palette-close|command-item|text-input|segmented-option|chip-button)/;
  const bad: string[] = [];
  for (const el of document.querySelectorAll('select:not([aria-hidden="true"])')) if (shown(el)) bad.push(`visible native select: ${el.outerHTML.slice(0, 120)}`);
  for (const el of document.querySelectorAll('button,input:not([type=hidden]),textarea')) {
   // xterm.js's input proxy: transparent, off-screen and named; the visible terminal field is what a person sees and focuses.
   if (el.classList.contains('xterm-helper-textarea')) continue;
   if (shown(el) && !themed.test(el.getAttribute('class') ?? '')) bad.push(`unstyled control: ${el.outerHTML.slice(0, 120)}`);
  }
  return bad;
 });
 expect(offenders).toEqual([]);
}
