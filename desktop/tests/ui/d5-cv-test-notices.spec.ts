import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { message, openApp, send } from './support/conversation';
import { tokenColorIn } from './contracts';
import { RECONNECT_WINDOW_MS } from '../../src/features/conversation/offline/reconnect';

/** A conversation with a task, so the header is on screen when the engine goes quiet. */
async function openWithHeader(page: Page) {
  const engine = await installMockEngine(page, {
    initial: { entries: [] },
    turns: [{
      entries: [{ Role: 'assistant', Text: 'Ready.', Answer: true }],
      patch: {
        title: 'Offline chat',
        tasks: [{ ID: 'task-1', Title: 'Build it', Status: 'running', Waits: [], Seat: 'worker', Steps: 1 }],
      },
    }],
  });
  await openApp(page);
  await send(page, 'Start');
  await expect(page.locator('.conversation-bar')).toBeVisible();
  return engine;
}

/** The line is ink, never the danger colour, and it carries no side stripe. */
async function expectMuted(page: Page) {
  const notice = page.locator('.engine-notice');
  // The pane's tint recomputes ink, so the probe has to sit on the line itself.
  const ink = await tokenColorIn(notice, 'ink-3');
  const danger = await tokenColorIn(notice, 'danger');
  await expect(notice).toHaveCSS('color', ink);
  const paint = await notice.evaluate(node => {
    const style = getComputedStyle(node);
    return { color: style.color, borderLeftWidth: style.borderLeftWidth };
  });
  expect(paint.color).not.toBe(danger);
  expect(paint.borderLeftWidth).toBe('0px');
}

for (const theme of ['light', 'dark'] as const) {
  test.describe(`engine offline notice · ${theme}`, () => {
    test.beforeEach(async ({ page }) => { await page.emulateMedia({ colorScheme: theme }); });

    test('offline: reconnecting under the header, then Retry after 30s, then gone', async ({ page }) => {
      await page.clock.install();
      const engine = await openWithHeader(page);
      engine.setOffline(true);
      await message(page).fill('Still here');
      await page.getByRole('button', { name: 'Send', exact: true }).click();

      const notice = page.locator('.engine-notice');
      await expect(notice).toHaveAttribute('data-engine-notice', 'reconnecting');
      await expect(notice).toHaveText('Reconnecting to the engine…');
      await expect(notice.getByRole('button', { name: 'Retry' })).toHaveCount(0);
      await expect(page.locator('.conversation-view')).not.toContainText('codeaf engine is not running');
      await expect(page.locator('.conversation-footer .engine-notice')).toHaveCount(0);

      const placed = await page.evaluate(() => {
        const bar = document.querySelector('.conversation-bar')!.getBoundingClientRect();
        const line = document.querySelector('.engine-notice')!.getBoundingClientRect();
        return { barBottom: bar.bottom, lineTop: line.top };
      });
      expect(placed.lineTop).toBeGreaterThanOrEqual(placed.barBottom - 1);
      await expectMuted(page);
      await expect(message(page)).toBeEnabled();
      await expect(message(page)).toHaveValue('Still here');

      await page.clock.fastForward(RECONNECT_WINDOW_MS);
      await expect(notice).toHaveAttribute('data-engine-notice', 'unreachable');
      await expect(notice).toContainText("Can't reach the engine");
      const retry = notice.getByRole('button', { name: 'Retry', exact: true });
      await expect(retry).toBeVisible();
      await expect(retry).toHaveCSS('color', await tokenColorIn(notice, 'ink-3'));
      expect(await retry.evaluate(node => getComputedStyle(node).color)).not.toBe(await tokenColorIn(notice, 'danger'));
      await expectMuted(page);
      await expect(message(page)).toHaveValue('Still here');

      engine.setOffline(false);
      await retry.click();
      await expect(notice).toHaveCount(0);
      await expect(message(page)).toHaveValue('Still here');
      await expect(page.locator('.conversation-view')).not.toContainText('codeaf engine is not running');
    });
  });
}
