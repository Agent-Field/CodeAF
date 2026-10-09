import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';

async function queue(page: import('@playwright/test').Page) {
  await installMockEngine(page, { ...plainReply(), initial: { id: 'specimen', entries: [] } });
  await page.goto('/');
  await page.getByRole('button', { name: 'Design system', exact: true }).click();
  const list = page.getByRole('list', { name: 'Queued messages' });
  await list.scrollIntoViewIfNeeded();
  return list;
}

test('queued rows show two, collapse the rest and expand on request', async ({ page }) => {
  const list = await queue(page);
  await expect(list.getByRole('button', { name: 'Remove queued message' })).toHaveCount(2);
  await page.getByRole('button', { name: '2 more queued' }).click();
  await expect(list.getByRole('button', { name: 'Remove queued message' })).toHaveCount(4);
  await page.getByRole('button', { name: 'Show fewer' }).click();
  await expect(list.getByRole('button', { name: 'Remove queued message' })).toHaveCount(2);
});

test('a queued row edits in place: Esc keeps the text, Save changes it, actions reach the keyboard', async ({ page }) => {
  const list = await queue(page);
  const edit = list.getByRole('button', { name: 'Edit queued message' }).first();
  await edit.focus();
  await expect(edit).toBeFocused();
  await page.keyboard.press('Enter');
  const field = list.getByRole('textbox', { name: 'Edit queued message' });
  await field.fill('changed and dropped');
  await page.keyboard.press('Escape');
  await expect(list.getByText('After that, update the changelog')).toBeVisible();
  await edit.click();
  await field.fill('After that, bump the changelog');
  await list.getByRole('button', { name: 'Save' }).click();
  await expect(list.getByText('After that, bump the changelog')).toBeVisible();
});
