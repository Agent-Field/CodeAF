import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { richReply } from './support/scenarios-v2';
import { openApp, send } from './support/conversation';

// The browser build has no native web: the web kind is unbacked, so a modified click on a link opens the default
// browser (a popup here) and never adds a web tab. The desktop side is native-web.spec.ts and web-integration.spec.ts.
test('without native web no web tab ever opens and a link modifier-click opens the browser', async ({ page }) => {
  await installMockEngine(page, richReply());
  await openApp(page);
  await send(page, 'Fix the flaky login test and draw a logo');
  await expect(page.getByText('so the login test no longer reads the real time.')).toBeVisible();
  const link = page.locator('.answer-block a[href="https://pkg.go.dev/time"]');
  const modifier = await page.evaluate(() => (/Mac/.test(navigator.platform) ? 'Meta' : 'Control') as 'Meta' | 'Control');
  const popup = page.waitForEvent('popup');
  await link.click({ modifiers: [modifier] });
  expect((await popup).url()).toBe('https://pkg.go.dev/time');
  await expect(page.locator('.workspace-tab[data-kind="web"]')).toHaveCount(0);
});
