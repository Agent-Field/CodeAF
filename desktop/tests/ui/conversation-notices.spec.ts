import { test, expect, type Page } from '@playwright/test';
import { installMockEngine, type ScriptedTurn } from './support/mock-engine';
import { withTaskTree } from './support/scenarios';
import { openApp, send } from './support/conversation';
import { expectAccessible } from './contracts';

// Turn notices: stopped, interrupted, retrying, compaction, no error dialog, status in words.
// The offline line and the offline queue are pinned by d5-cv-test-notices.spec.ts.

const WIDTHS = [320, 600, 1200];

/** A conversation whose one turn ends the way `turn` says. */
async function openWith(page: Page, turn: ScriptedTurn, manual = false) {
  const engine = await installMockEngine(page, { initial: { title: 'Notices', entries: [] }, turns: [turn], manual });
  await openApp(page);
  await send(page, 'Go');
  // An event pushed before the page reads the stream is not drawn, so wait until it has.
  if (manual) await expect.poll(() => engine.calls.some(call => call.path.endsWith('/events'))).toBe(true);
  return engine;
}

/** A turn cut off by the person's Stop or by an engine restart: the engine marks the entry Interrupted. */
const CUT: ScriptedTurn = { entries: [{ Role: 'assistant', Text: 'Starting the', Answer: false, Interrupted: true }] };

for (const theme of ['light', 'dark'] as const) {
  test.describe(`conversation notices · ${theme}`, () => {
    test.beforeEach(async ({ page }) => { await page.emulateMedia({ colorScheme: theme }); });

    for (const width of WIDTHS) {
      test(`stopped and interrupted turns read Stopped with the ban icon, nothing red, at ${width}px (CV-261, CV-262)`, async ({ page }) => {
        await page.setViewportSize({ width, height: 800 });
        await openWith(page, CUT);
        const footer = page.locator('.turn-footer');
        await expect(footer).toHaveText('Stopped');
        await expect(footer).toHaveAttribute('role', 'status');
        await expect(footer).not.toHaveAttribute('data-failed', 'true');
        await expect(footer.locator('svg').first()).toBeVisible();
        await expect(page.getByRole('alert')).toHaveCount(0);
        await expect(page.getByRole('dialog')).toHaveCount(0);
        expect(await page.evaluate(() => document.scrollingElement!.scrollWidth <= innerWidth)).toBe(true);
        await expectAccessible(page);
      });
    }

    test('retrying shows a mono countdown only when the engine sent a delay (CV-263)', async ({ page }) => {
      const engine = await openWith(page, { entries: [{ Role: 'assistant', Text: 'Done.', Answer: true }] }, true);
      const note = page.locator('.system-note[data-kind="retrying"]');
      await expect(async () => {
        engine.retrying(4, 'the provider was busy');
        await expect(note).toContainText('Retrying — the provider was busy', { timeout: 1000 });
      }).toPass({ timeout: 10_000 });
      await expect(note).toHaveAttribute('role', 'status');
      await expect(note.locator('.system-note-time')).toHaveText('4s');
      expect(await note.locator('.system-note-time').evaluate(node => getComputedStyle(node).fontFamily)).toMatch(/mono/i);
      await expect(page.getByRole('dialog')).toHaveCount(0);
    });

    test('retrying without a delay shows no countdown (CV-263)', async ({ page }) => {
      const engine = await openWith(page, { entries: [{ Role: 'assistant', Text: 'Done.', Answer: true }] }, true);
      const note = page.locator('.system-note[data-kind="retrying"]');
      await expect(async () => {
        engine.retrying(undefined, 'busy again');
        await expect(note).toContainText('Retrying — busy again', { timeout: 1000 });
      }).toPass({ timeout: 10_000 });
      await expect(note.locator('.system-note-time')).toHaveCount(0);
    });

    test('compacted inserts the divider live; compacting alone draws nothing (CV-264)', async ({ page }) => {
      const engine = await openWith(page, { entries: [{ Role: 'assistant', Text: 'Done.', Answer: true }] }, true);
      const draws = () => page.locator('.system-note').count();
      const before = await draws();
      engine.compacting();
      await page.waitForTimeout(250);
      expect(await draws()).toBe(before);
      // The event is idempotent, so a push the page was too early for is simply sent again.
      await expect(async () => {
        engine.compacted();
        await expect(page.locator('.system-note[data-kind="compaction"]')).toContainText('Earlier messages summarized', { timeout: 1000 });
      }).toPass({ timeout: 10_000 });
      await expect(page.locator('.system-note[data-kind="compaction"]')).toBeVisible();
    });

    test('no engine error opens a dialog (CV-268)', async ({ page }) => {
      const engine = await installMockEngine(page, { initial: { title: 'Notices', entries: [] }, fail: { turn: 500 } });
      await openApp(page);
      await send(page, 'Go');
      await expect(page.getByText(/Mock engine forced turn failure|failed/i).first()).toBeVisible();
      await expect(page.getByRole('dialog')).toHaveCount(0);
      await expect(page.getByRole('alertdialog')).toHaveCount(0);
      await expect(page.locator('dialog[open], [aria-modal="true"]')).toHaveCount(0);
      engine.setOffline(true);
      await send(page, 'again');
      await expect(page.getByRole('dialog')).toHaveCount(0);
      await expect(page.locator('dialog[open], [aria-modal="true"]')).toHaveCount(0);
    });

    test('status reaches screen readers as words, not colour (CV-283)', async ({ page }) => {
      const base = withTaskTree();
      await installMockEngine(page, { ...base, initial: { ...base.initial, entries: [] }, turns: [{ entries: base.initial.entries as never, patch: { tasks: base.initial.tasks } }] });
      await openApp(page);
      await send(page, 'Ship it');
      const marks = page.getByRole('complementary', { name: 'Tasks' }).locator('.status-mark');
      await expect(marks.first()).toBeVisible();
      const labels = await marks.evaluateAll(nodes => nodes.map(node => node.getAttribute('aria-label') ?? ''));
      for (const label of labels) expect(label.trim()).not.toBe('');
      expect(labels.some(label => /your call/i.test(label))).toBe(true);
      expect(labels.some(label => /running/i.test(label))).toBe(true);
      expect(await marks.evaluateAll(nodes => nodes.every(node => node.getAttribute('role') === 'img'))).toBe(true);
    });
  });
}
