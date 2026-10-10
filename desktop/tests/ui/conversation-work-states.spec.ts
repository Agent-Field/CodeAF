import { openPage } from './support/shell-navigation';
import { test, expect, type Locator, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { expectAccessible, tokenColorIn } from './contracts';

// Work block and step states from design v3 Components §3 and Interactions I-ICV-8..13, measured on the Work and Turns
// specimens (fixed clock, so nothing here waits on wall time), Light and Dark, at the three widths the layout changes at.

const WIDTHS = [320, 600, 1200];

async function openWork(page: Page, scheme: 'light' | 'dark', width: number) {
  await page.addInitScript(() => {
    (window as any).copied = [];
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (text: string) => { (window as any).copied.push(text); }, readText: async () => '' } });
  });
  await page.emulateMedia({ colorScheme: scheme });
  await page.setViewportSize({ width, height: 900 });
  await installMockEngine(page, { ...plainReply(), initial: { id: 'specimen', entries: [] } });
  await page.goto('/');
  await openPage(page, 'Design system');
}

const block = (page: Page, label: string) =>
  page.locator('.work-specimen-section').filter({ hasText: label }).first().locator('.work-block');
const copied = (page: Page) => page.evaluate(() => (window as any).copied as string[]);
const step = (scope: Locator, title: string) => scope.locator('.work-step').filter({ hasText: title });

for (const scheme of ['light', 'dark'] as const) {
  for (const width of WIDTHS) {
    test.describe(`${scheme} ${width}px`, () => {
      test.beforeEach(async ({ page }) => openWork(page, scheme, width));

      test('live header reads Working with a mono clock; the settled step is 28px ink-3 and the running one ink (CV-054, CV-055)', async ({ page }) => {
        const live = block(page, 'a finished step, then tests in flight');
        const toggle = live.locator('.work-toggle');
        await toggle.scrollIntoViewIfNeeded();
        await expect(toggle.locator('.work-summary-text > span').first()).toHaveText('Working');
        await expect(toggle.locator('.work-live-time')).toHaveText('42s');
        await expect(toggle.locator('.work-live-time')).toHaveCSS('font-family', /mono|Menlo|Consolas/i);

        const done = step(live, 'Read the display record shaping').locator('.work-step-head');
        await expect(done).toHaveCSS('height', '28px');
        await expect(done).toHaveCSS('color', await tokenColorIn(done, 'ink-3'));
        await expect(step(live, 'Read the display record shaping').locator('.work-time')).toHaveText('0.2s');
        const running = step(live, 'Running the session tests').locator('.work-step-head');
        await expect(running).toHaveCSS('color', await tokenColorIn(running, 'ink'));
        await expect(step(live, 'Running the session tests').locator('.work-step-dot[data-mark="running"]')).toBeVisible();
        expect(await page.evaluate(() => document.scrollingElement!.scrollWidth <= innerWidth)).toBe(true);
      });

      test('a failed step is ink-2 with a red 6px dot and no coloured words; a stopped one carries the stopped mark (CV-055, Q19)', async ({ page }) => {
        const settled = block(page, 'Settled, opened');
        const failed = step(settled, 'Ran the tests');
        await failed.scrollIntoViewIfNeeded();
        await expect(failed.locator('.work-step-head')).toHaveCSS('color', await tokenColorIn(failed, 'ink-2'));
        const dot = failed.locator('.work-step-dot[data-mark="failed"]');
        const mark = await dot.evaluate(el => {
          const after = getComputedStyle(el, '::before');
          return { background: after.backgroundColor, width: after.width, height: after.height };
        });
        expect(mark.background).toBe(await tokenColorIn(failed, 'danger'));
        expect([mark.width, mark.height]).toEqual(['6px', '6px']);
        const word = failed.locator('.work-step-word');
        await expect(word).toHaveText('failed · 21s');
        await expect(word).not.toHaveCSS('color', await tokenColorIn(failed, 'danger'));
        await expect(step(settled, 'Fetched the Go testing docs').locator('.work-step-word')).toHaveText('stopped');
      });

      test('only Thinking shimmers while it streams; no settled or folded block animates (CV-058)', async ({ page }) => {
        await block(page, 'a finished step, then tests in flight').scrollIntoViewIfNeeded();
        await expect(page.locator('.work-step-title[data-shimmer]')).toHaveCount(0);
        const animated = await page.locator('.work-specimen .work-block *').evaluateAll(
          els => els.filter(el => getComputedStyle(el).animationName !== 'none').map(el => el.className));
        expect(animated.every(name => String(name).includes('thinking-shimmer'))).toBe(true);
        await expect(block(page, 'Settled, opened').locator('.thinking-shimmer')).toHaveCount(0);
      });

      test('a forming call reads Preparing… at .55; a refused one is struck through with the word refused; waiting on you never folds (CV-061, CV-062)', async ({ page }) => {
        const forming = block(page, 'a call still forming');
        await forming.scrollIntoViewIfNeeded();
        const formingStep = step(forming, 'Editing the shaping loop');
        await expect(formingStep.locator('.work-step-word')).toHaveText('preparing…');
        await expect(formingStep.locator('.work-step-title')).toHaveCSS('opacity', '0.55');
        const calls = page.locator('.work-specimen-section').filter({ hasText: 'Tool call row states' });
        const preparing = calls.locator('.work-call[data-state="forming"]');
        await expect(preparing.locator('.work-call-text')).toHaveText('Preparing…');
        await expect(preparing.locator('.work-call-head')).toHaveCSS('opacity', '0.55');
        const refused = calls.locator('.work-call[data-state="refused"]');
        await expect(refused.locator('.work-call-text')).toHaveCSS('text-decoration-line', 'line-through');
        await expect(refused.locator('.work-call-status')).toHaveText('refused');
        await expect(calls.locator('.work-call[data-state="waiting"] .work-call-status')).toHaveText('waiting on you');
        await expect(calls.locator('.work-call[data-state="stopped"] .work-call-status')).toHaveText('Stopped');

        const waiting = step(block(page, 'waiting on you'), 'Removing the old build');
        await expect(waiting.locator('.work-step-head')).toHaveAttribute('aria-expanded', 'true');
        await expect(waiting.locator('.work-call')).toHaveCount(1);
        await expect(waiting.locator('.work-step-dot[data-mark="waiting"]')).toBeVisible();
      });

      test('the work line and step rows change fill on hover and nothing else (CV-051, CV-065)', async ({ page }) => {
        const settled = block(page, 'Settled, folded');
        const line = settled.locator('.work-toggle');
        await line.scrollIntoViewIfNeeded();
        const rest = await line.evaluate(el => getComputedStyle(el).backgroundColor);
        await line.hover();
        await expect(line).toHaveCSS('background-color', await tokenColorIn(line, 'field'));
        expect(rest).not.toBe(await line.evaluate(el => getComputedStyle(el).backgroundColor));
        await expect(line).toHaveCSS('transition-duration', /0\.12s/);

        const row = step(block(page, 'Settled, opened'), 'Searched for where notes are dropped').locator('.work-step-head');
        await row.scrollIntoViewIfNeeded();
        const before = await row.evaluate(el => getComputedStyle(el).backgroundColor);
        await row.hover();
        await expect.poll(() => row.evaluate(el => getComputedStyle(el).backgroundColor)).not.toBe(before);
        await expect(row).toHaveCSS('height', '28px');
      });

      test('Thought for Ns is collapsed until opened (CV-067)', async ({ page }) => {
        const settled = block(page, 'Settled, opened');
        const thought = settled.getByRole('button', { name: 'Thought for 6s' });
        await thought.scrollIntoViewIfNeeded();
        await expect(thought).toHaveAttribute('aria-expanded', 'false');
        await expect(settled.locator('.thinking-body')).toHaveCount(0);
        await thought.click();
        await expect(settled.locator('.thinking-body')).toContainText('Notes must survive shaping');
      });

      test('a steer lands on the running step, then reads as consumed without its landing clause (CV-068, CV-069)', async ({ page }) => {
        const turns = page.locator('section[aria-label="Turns specimen"]');
        const pending = turns.locator('.steer[data-consumed="false"]');
        await pending.scrollIntoViewIfNeeded();
        await expect(pending.locator('.steer-landing')).toHaveText('waiting for the running step');
        await expect(pending.locator('.steer-elbow')).toBeVisible();
        await expect(pending.locator('.steer-text')).toHaveText('also check the tests');
        await expect(pending.locator('.steer-text')).toHaveCSS('color', await tokenColorIn(pending, 'ink'));
        const consumed = turns.locator('.steer[data-consumed="true"]');
        await expect(consumed.locator('.steer-landing')).toHaveCount(0);
        await expect(consumed.locator('.steer-text')).toHaveText('and keep the old timeout');
        await expect(consumed.locator('.steer-text')).toHaveCSS('color', await tokenColorIn(consumed, 'ink-2'));
      });

      test('right-click copies the log from the work line and the command or output from a step (CV-053, CV-064)', async ({ page }) => {
        const settled = block(page, 'Settled, folded');
        const line = settled.locator('.work-toggle');
        await line.scrollIntoViewIfNeeded();
        await line.click({ button: 'right' });
        await page.getByRole('menuitem', { name: 'Copy log' }).click();
        await expect.poll(async () => (await copied(page)).length).toBe(1);
        const log = (await copied(page))[0];
        expect(log).toContain('Ran the tests');
        expect(log).toContain('go test ./internal/session ./internal/tui3');
        expect(log.indexOf('Searched for where notes are dropped')).toBeLessThan(log.indexOf('Ran the tests'));

        const row = step(block(page, 'Settled, opened'), 'Ran the tests').locator('.work-step-head');
        await row.scrollIntoViewIfNeeded();
        await row.click({ button: 'right' });
        await page.getByRole('menuitem', { name: 'Copy command or output' }).click();
        await expect.poll(async () => (await copied(page)).length).toBe(2);
        const text = (await copied(page))[1];
        expect(text).toContain('go test ./internal/session ./internal/tui3');
        expect(text.split('\n\n')).toHaveLength(2);
      });

      test('steps, the work line and the Thought row are reached by Tab, and Enter toggles them (CV-284)', async ({ page }) => {
        const settled = block(page, 'Settled, opened');
        const thought = settled.getByRole('button', { name: 'Thought for 6s' });
        await thought.scrollIntoViewIfNeeded();
        await settled.locator('.work-toggle').focus();
        const reached: string[] = [];
        for (let at = 0; at < 3; at += 1) {
          await page.keyboard.press('Tab');
          reached.push(await page.evaluate(() => document.activeElement?.textContent?.trim() ?? ''));
        }
        expect(reached[0]).toContain('Thought for 6s');
        expect(reached[1]).toContain('Searched for where notes are dropped');
        expect(reached[2]).toContain('Edited the shaping loop');
        const focused = settled.locator('.work-step-head:focus-visible');
        await expect(focused).toHaveCount(1);
        await expect(focused).toHaveCSS('box-shadow', /.+/);
        await page.keyboard.press('Enter');
        await expect(settled.locator('.work-step-head:focus')).toHaveAttribute('aria-expanded', 'true');
      });

      test('receipts are reached by Tab and open on Enter (CV-284)', async ({ page }) => {
        const tray = page.locator('section[aria-label="Decision tray specimen"]');
        const receipt = tray.locator('button.receipt[data-state="answered"]').first();
        await receipt.scrollIntoViewIfNeeded();
        await receipt.focus();
        await expect(receipt).toBeFocused();
        await expect(receipt).toHaveAttribute('aria-expanded', 'false');
        await page.keyboard.press('Enter');
        await expect(receipt).toHaveAttribute('aria-expanded', 'true');
        await page.keyboard.press('Shift+Tab');
        await page.keyboard.press('Tab');
        await expect(receipt).toBeFocused();
      });

      test('the work specimen has no new axe violations', async ({ page }) => {
        await block(page, 'Settled, opened').scrollIntoViewIfNeeded();
        await expectAccessible(page, '.work-specimen');
      });
    });
  }
}
