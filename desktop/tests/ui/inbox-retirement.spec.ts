import { expect, test } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { deliverLinks, installDeepLinkMock } from './support/deep-link-mock';

for (const colorScheme of ['light', 'dark'] as const) {
 test(`retired Inbox drops on reload, preserving pinned tabs and drafts (${colorScheme})`, async ({ page }) => {
  await page.emulateMedia({ colorScheme });
  const engine = await installMockEngine(page, {});
  await page.addInitScript(() => {
   localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({
    tabs: [
     { id: 'inbox', kind: 'inbox', pinned: true },
     { id: 'notes', kind: 'conversation', title: 'Pinned notes', draft: 'Keep these words', pinned: true },
     { id: 'other', kind: 'conversation', title: 'Other chat', draft: 'Other draft', pinned: false },
    ], closed: [], groups: [], activeId: 'inbox', recentIds: ['inbox', 'other'], nextNumber: 7,
   }));
  });
  await page.goto('/');
  await expect(page.getByRole('tab', { name: 'Inbox', exact: true })).toHaveCount(0);
  await expect(page.getByRole('tab', { name: 'Pinned notes', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('textarea')).toHaveValue('Keep these words');
  await page.getByRole('tab', { name: 'Other chat', exact: true }).click();
  await expect(page.locator('textarea')).toHaveValue('Other draft');
  await page.reload();
  await expect(page.getByRole('tab', { name: 'Inbox', exact: true })).toHaveCount(0);
  await expect(page.getByRole('tab', { name: 'Other chat', exact: true })).toBeVisible();
  expect(engine.calls.filter(call => call.path.endsWith('/turn'))).toHaveLength(0);
 });
}

test('an old Inbox deep link starts Next up without opening a tab or calling a model', async ({ page }) => {
 await installDeepLinkMock(page);
 const engine = await installMockEngine(page, {});
 await page.addInitScript(() => {
  let starts = 0;
  window.addEventListener('codeaf:next-up-start', () => { document.documentElement.dataset.nextUpStarts = String(++starts); });
 });
 await page.goto('/');
 await expect(page.getByRole('tab')).toHaveCount(1);
 await deliverLinks(page, ['codeaf://inbox']);
 await expect(page.locator('html')).toHaveAttribute('data-next-up-starts', '1');
 await page.evaluate(() => window.dispatchEvent(new CustomEvent('codeaf:shell-open', { detail: 'inbox' })));
 await expect(page.locator('html')).toHaveAttribute('data-next-up-starts', '2');
 await expect(page.getByRole('tab', { name: 'Inbox', exact: true })).toHaveCount(0);
 await expect(page.getByRole('tab')).toHaveCount(1);
 expect(engine.calls.filter(call => call.path.endsWith('/turn'))).toHaveLength(0);
});
