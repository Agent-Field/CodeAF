import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { openApp } from './support/conversation';
import { openPage } from './support/shell-navigation';

const scenario = () => ({ ...plainReply(), initial: { entries: [], title: '' } });

async function openSettingsWithKey(page: Page, status: number, body: unknown) {
  await installMockEngine(page, scenario());
  await page.route('**/settings/key', route => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) }));
  await openApp(page);
  await openPage(page, 'Settings');
  await expect(page.getByRole('heading', { name: 'Models', level: 1 })).toBeVisible();
}

test('the provider key row says where the key comes from and never shows a value', async ({ page }) => {
  await openSettingsWithKey(page, 200, { present: true, source: 'OPENROUTER_API_KEY', key: 'sk-canary-value' });
  const row = page.locator('[data-setting="provider-key"]');
  await expect(row).toContainText('Provider key');
  await expect(row).toContainText('From OPENROUTER_API_KEY');
  await expect(page.getByText('sk-canary-value')).toHaveCount(0);
});

test('no key reads Not set', async ({ page }) => {
  await openSettingsWithKey(page, 200, { present: false });
  await expect(page.locator('[data-setting="provider-key"]')).toContainText('Not set');
});

test('a failed key status draws no row', async ({ page }) => {
  await openSettingsWithKey(page, 500, { error: 'down' });
  await expect(page.locator('[data-setting="provider-key"]')).toHaveCount(0);
});
