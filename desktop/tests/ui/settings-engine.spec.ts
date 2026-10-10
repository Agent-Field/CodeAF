import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { openApp } from './support/conversation';
import { openPage } from './support/shell-navigation';
import { tokenColor } from './contracts';

const section = (page: Page) => page.getByRole('region', { name: 'Engine' });

async function health(page: Page, status: number) {
  await page.unroute('**/api/engine/health').catch(() => undefined);
  await page.route('**/api/engine/health', route => {
    if (status === 0) return route.abort('connectionrefused');
    return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ status: 'ready', version: 'dev', platform: 'browser' }) });
  });
}

/** State text stays the muted ink. The unreachable line does not take the danger colour. */
async function expectQuiet(page: Page) {
  const state = section(page).getByRole('status');
  const color = await state.evaluate(node => getComputedStyle(node).color);
  const ink2 = await tokenColor(page, 'ink-2');
  const danger = await tokenColor(page, 'danger');
  expect(color).toBe(ink2);
  expect(color).not.toBe(danger);
  const box = await state.evaluate(node => {
    const rect = node.getBoundingClientRect();
    return { width: rect.width, height: rect.height };
  });
  expect(box.width).toBeGreaterThan(0);
  expect(box.height).toBeGreaterThan(0);
}

test('engine section shows connected, then Retry when unreachable', async ({ page }) => {
  await installMockEngine(page, plainReply());
  await health(page, 200);
  await openApp(page);
  await openPage(page, 'Settings');
  const engine = section(page);
  await expect(engine.getByRole('status')).toHaveText('Connected');
  await expect(engine.getByRole('button', { name: 'Retry' })).toHaveCount(0);
  const where = engine.locator('.settings-engine-where');
  await expect(where).toHaveText(/^https?:\/\/.+/);
  await expectQuiet(page);
  const address = await where.innerText();

  await page.getByRole('combobox', { name: 'Theme' }).click();
  await page.getByRole('option', { name: 'Dark appearance' }).click();
  await expectQuiet(page);
  await page.getByRole('combobox', { name: 'Theme' }).click();
  await page.getByRole('option', { name: 'Light appearance' }).click();

  await health(page, 0);
  await page.reload();
  await openPage(page, 'Settings');
  await expect(engine.getByRole('status')).toHaveText("Can't reach the engine");
  const retry = engine.getByRole('button', { name: 'Retry' });
  await expect(retry).toBeVisible();
  await expect(where).toHaveText(address);
  await expectQuiet(page);
  await expect(retry).toHaveCSS('color', await tokenColor(page, 'ink-3'));
  const retryColor = await retry.evaluate(node => getComputedStyle(node).color);
  expect(retryColor).not.toBe(await tokenColor(page, 'danger'));

  await retry.click();
  await expect(engine.getByRole('status')).toHaveText("Can't reach the engine");
  await expect(retry).toBeVisible();

  await health(page, 200);
  await retry.click();
  await expect(engine.getByRole('status')).toHaveText('Connected');
  await expect(engine.getByRole('button', { name: 'Retry' })).toHaveCount(0);
  await expectQuiet(page);
});
