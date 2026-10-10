import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { richReply } from './support/scenarios-v2';
import { openApp, send } from './support/conversation';

// Interactions I-ICV-9 and I-ICV-12: the work line copies the log, a step row copies its command or output.
test('the work line and a step row each offer their copy, by right-click and by the menu key', async ({ page, context, browserName }) => {
  if (browserName === 'chromium') await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await installMockEngine(page, richReply());
  await openApp(page);
  await send(page, 'Fix the flaky login test and draw a logo');
  const summary = page.getByRole('button', { name: /^Worked \d+s · 2 steps · 5 calls$/ });
  await summary.click();

  await summary.click({ button: 'right' });
  const logItem = page.getByRole('menuitem', { name: 'Copy log' });
  await expect(logItem).toBeVisible();
  await page.keyboard.press('Escape');

  const step = page.getByRole('button', { name: /^Pinning the clock/ });
  await step.focus();
  await page.keyboard.press('Shift+F10');
  const stepItem = page.getByRole('menuitem', { name: 'Copy command or output' });
  await expect(stepItem).toBeVisible();
  await expect(page.getByRole('menuitem', { name: 'Copy log' })).toHaveCount(0);
  await page.keyboard.press('Escape');

  if (browserName !== 'chromium') return;
  await summary.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Copy log' }).click();
  const log = await page.evaluate(() => navigator.clipboard.readText());
  expect(log).toContain('Pinning the clock');
  expect(log.indexOf('Reading the login test')).toBeLessThan(log.indexOf('Pinning the clock'));
});
