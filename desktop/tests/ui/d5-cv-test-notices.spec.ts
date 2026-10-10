import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { message, openApp, posts, send } from './support/conversation';
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
      // The send is held in the pane. A draft typed after it stays; Retry does not send that draft.
      await expect(message(page)).toHaveValue('');
      await expect(page.locator('.user-message[data-sending]')).toHaveText('Still here');
      await message(page).fill('not sent');

      await page.clock.fastForward(RECONNECT_WINDOW_MS);
      await expect(notice).toHaveAttribute('data-engine-notice', 'unreachable');
      await expect(notice).toContainText("Can't reach the engine");
      const retry = notice.getByRole('button', { name: 'Retry', exact: true });
      await expect(retry).toBeVisible();
      await expect(retry).toHaveCSS('color', await tokenColorIn(notice, 'ink-3'));
      expect(await retry.evaluate(node => getComputedStyle(node).color)).not.toBe(await tokenColorIn(notice, 'danger'));
      await expectMuted(page);
      await expect(message(page)).toHaveValue('not sent');

      engine.setOffline(false);
      await retry.click();
      await expect(notice).toHaveCount(0);
      await expect(message(page)).toHaveValue('not sent');
      await expect(page.locator('.user-message[data-sending]')).toHaveCount(0);
      await expect(page.locator('.user-message-text').filter({ hasText: 'Still here' })).toBeVisible();
      await expect(page.locator('.conversation-view')).not.toContainText('codeaf engine is not running');
    });

    test('offline-send: plain text queues at 60% and posts in order; a file stays; a refusal restores the draft', async ({ page }) => {
      await page.clock.install();
      const engine = await openWithHeader(page);
      const turnTexts = () => posts(engine, '/turn').map(call => String(call.body.text ?? ''));
      engine.setOffline(true);
      await send(page, 'first offline');
      await expect(page.locator('.user-message[data-sending]')).toHaveText('first offline');
      await expect(message(page)).toHaveValue('');
      await send(page, 'second offline');

      const bubbles = page.locator('.user-message[data-sending]');
      await expect(bubbles).toHaveText(['first offline', 'second offline']);
      await expect(bubbles.first()).toHaveCSS('opacity', '0.6');
      await expect(bubbles.nth(1)).toHaveCSS('opacity', '0.6');
      await expect(message(page)).toBeEnabled();
      await expect(message(page)).toHaveValue('');
      // The second send waited here. It did not call the engine while it was unreachable.
      expect(turnTexts().filter(text => text === 'second offline')).toEqual([]);

      await page.getByTestId('composer-file-input').setInputFiles([{ name: 'a.txt', mimeType: 'text/plain', buffer: Buffer.from('a') }]);
      await message(page).fill('with a file');
      await page.getByRole('button', { name: 'Send', exact: true }).click();
      await expect(message(page)).toHaveValue('with a file');
      await expect(page.getByRole('list', { name: 'Attachments' }).getByRole('listitem')).toHaveCount(1);
      await expect(bubbles).toHaveText(['first offline', 'second offline']);
      expect(turnTexts().filter(text => text === 'with a file')).toEqual([]);
      await page.getByRole('button', { name: 'Remove a.txt' }).click();

      await message(page).fill('left in the box');
      engine.setOffline(false);
      await page.clock.fastForward(1_500);
      await expect.poll(() => turnTexts().slice(-2)).toEqual(['first offline', 'second offline']);
      await expect(bubbles).toHaveCount(0);
      await expect(message(page)).toHaveValue('left in the box');
      // Older turns fold, so the words live on the turn, not only in an open bubble.
      const column = page.locator('.conversation-scroll .conversation-column');
      await expect(column.getByText('first offline', { exact: true })).toBeVisible();
      await expect(column.getByText('second offline', { exact: true })).toBeVisible();
      const order = await column.innerText();
      expect(order.indexOf('first offline')).toBeGreaterThanOrEqual(0);
      expect(order.indexOf('first offline')).toBeLessThan(order.indexOf('second offline'));

      engine.setOffline(true);
      await send(page, 'bring me back');
      await expect(page.locator('.user-message[data-sending]')).toHaveText('bring me back');
      await page.route('**/api/engine/sessions/*/turn', async route => {
        await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: 'Mock engine forced turn failure' }) });
      }, { times: 1 });
      engine.setOffline(false);
      await page.clock.fastForward(1_500);
      await expect(message(page)).toHaveValue('bring me back');
      await expect(page.getByText('Mock engine forced turn failure')).toBeVisible();
      await expect(page.locator('.user-message[data-sending]')).toHaveCount(0);
      await expect(page.locator('.engine-notice')).toHaveCount(0);
    });
  });
}
