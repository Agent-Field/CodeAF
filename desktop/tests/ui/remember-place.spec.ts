import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces } from './support/mock-places';

for (const theme of ['light', 'dark'] as const) {
test(`a remembered place line renders its exact engine Undo token · ${theme}`, async ({ page }) => {
  await page.emulateMedia({ colorScheme: theme });
  const token = `remember_ln_saved_${'a'.repeat(64)}`;
  await installMockEngine(page, {
    initial: { entries: [], title: '' },
    turns: [{ entries: [
      { Role: 'aside', AsideKind: 'places', Text: 'Saved to Release: Run tests before launch', UndoReceipts: [token] },
      { Role: 'assistant', Text: 'Saved.', Answer: true },
    ] }],
  });
  await installMockPlaces(page, { places: [{ name: 'Release' }], chats: [] });
  const requests: unknown[] = [];
  await page.route('**/places/undo', async route => {
    requests.push(route.request().postDataJSON());
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ revision: 2, undone: 1 }) });
  });
  await page.goto('/');
  const composer = page.getByRole('textbox', { name: 'Message', exact: true });
  await composer.fill('Remember: run tests before launch');
  await composer.press('Enter');
  const note = page.locator('.system-note').filter({ hasText: 'Saved to Release: Run tests before launch' });
  await expect(note).toBeVisible();
  await note.getByRole('button', { name: 'Undo', exact: true }).click();
  await expect.poll(() => requests.length).toBe(1);
  expect(requests[0]).toMatchObject({ receipts: [token] });
  await expect(note.getByRole('button', { name: 'Undo', exact: true })).toHaveCount(0);
});
}
