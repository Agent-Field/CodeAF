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
      { Role: 'aside', AsideKind: 'remember-line', Text: 'Saved to Marketing: Run tests before launch', UndoReceipts: [token] },
      { Role: 'assistant', Text: 'Saved.', Answer: true },
    ] }],
  });
  await installMockPlaces(page, { places: [{ name: 'Marketing' }], chats: [] });
  const requests: unknown[] = [];
  await page.route('**/places/knows', async route => {
    expect(route.request().method()).toBe('DELETE');
    requests.push(route.request().postDataJSON());
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ revision: 2, removed: true }) });
  });
  await page.goto('/');
  const composer = page.getByRole('textbox', { name: 'Message', exact: true });
  await composer.fill('Remember: run tests before launch');
  await composer.press('Enter');
  const note = page.locator('.system-note').filter({ hasText: 'Saved to Marketing: Run tests before launch' });
  await expect(note).toBeVisible();
  const style = await note.evaluate(el => ({ size: getComputedStyle(el).fontSize, color: getComputedStyle(el).color, ink: getComputedStyle(el).getPropertyValue('--ink-3').trim(), height: el.getBoundingClientRect().height }));
  expect(style.size).toBe('12px');
  expect(style.height).toBeGreaterThanOrEqual(24);
  await expect(note).toContainText('·');
  await note.getByRole('button', { name: 'Undo', exact: true }).click();
  await expect.poll(() => requests.length).toBe(1);
  expect(requests[0]).toMatchObject({ token });
  await expect(page.locator('.system-note[data-kind=remember]')).toHaveText('Removed');
  await expect(page.locator('.system-note[data-kind=remember]').getByRole('button')).toHaveCount(0);
});
}

for (const theme of ['light', 'dark'] as const) {
  test(`Remember waits for acknowledgement and preserves a refused save · ${theme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    const token = `remember_ln_saved_${'b'.repeat(64)}`;
    await installMockEngine(page, {
      initial: { entries: [], title: '' },
      turns: [{ entries: [
        { Role: 'aside', AsideKind: 'places', Text: `Saved to Marketing: ${'Keep the launch review in this place. '.repeat(6)}`, UndoReceipts: [token] },
        { Role: 'assistant', Text: 'Saved.', Answer: true },
      ] }],
    });
    await installMockPlaces(page, { places: [{ name: 'Marketing' }], chats: [] });
    let release!: () => void;
    const held = new Promise<void>(resolve => { release = resolve; });
    let requests = 0;
    await page.route('**/places/knows', async route => {
      expect(route.request().method()).toBe('DELETE');
      expect(route.request().postDataJSON()).toEqual({ token });
      requests++;
      if (requests === 1) {
        await held;
        await route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: 'That line has changed.', code: 'stale' }) });
      } else {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ revision: 3, removed: true }) });
      }
    });
    await page.goto('/');
    const composer = page.getByRole('textbox', { name: 'Message', exact: true });
    await composer.fill('Remember the launch review');
    await composer.press('Enter');
    const note = page.locator('.system-note[data-kind=remember]');
    const undo = note.getByRole('button', { name: 'Undo', exact: true });
    for (const width of [320, 480, 600, 800, 1200]) {
      await page.setViewportSize({ width, height: 560 });
      await expect(undo).toBeVisible();
      const rect = await undo.boundingBox();
      expect(rect!.x).toBeGreaterThanOrEqual(0);
      expect(rect!.x + rect!.width).toBeLessThanOrEqual(width);
    }
    await undo.click();
    await expect(undo).toBeDisabled();
    await expect(note).toContainText('Saved to Marketing:');
    expect(requests).toBe(1);
    release();
    await expect(page.getByText('That line has changed.', { exact: true })).toBeVisible();
    await expect(undo).toBeEnabled();
    await expect(note).toContainText('Saved to Marketing:');
    await undo.click();
    await expect(note).toHaveText('Removed');
    expect(requests).toBe(2);
  });
}
