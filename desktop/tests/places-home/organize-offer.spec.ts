import { test, expect, type Page } from '@playwright/test';

// A stateful stand-in for the engine's organize routes: the offer is due at `unplaced` chats until it is
// dismissed, and a started run reports itself running with a progress event.
async function engine(page: Page, unplaced: number) {
  const state = { dismissed: false, started: false, posts: [] as string[] };
  await page.route('**/api/engine/places/organize**', route => {
    const request = route.request();
    const path = new URL(request.url()).pathname.replace('/api/engine', '');
    if (request.method() === 'POST') {
      state.posts.push(path);
      if (path === '/places/organize/offer/dismiss') state.dismissed = true;
      if (path === '/places/organize') state.started = true;
      return route.fulfill({ json: path === '/places/organize' ? { run: 'run-1', started: true } : { dismissed: true } });
    }
    const offer = !state.dismissed && !state.started && unplaced >= 12 ? { unplaced } : undefined;
    return route.fulfill({ json: state.started
      ? { running: true, progress: { run: 'run-1', stage: 'vectors', done: 3, total: unplaced, cancellable: true } }
      : { running: false, offer } });
  });
  return state;
}

for (const theme of ['light', 'dark']) {
  test(`offer shows at 12 unplaced chats and starts the add-on run · ${theme}`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    const state = await engine(page, 12);
    await page.goto('/?offer=1');
    await expect(page.getByText('You have 12 chats. Organize them into places?')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Not now' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Options' })).toBeVisible();
    await page.getByRole('button', { name: 'Organize', exact: true }).click();
    await expect(page.getByRole('status')).toContainText('Reading 12 chats');
    await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible();
    expect(state.posts).toEqual(['/places/organize']);
  });
}

test('offer is absent below 12 unplaced chats', async ({ page }) => {
  await engine(page, 11);
  await page.goto('/?offer=1');
  await page.waitForTimeout(300);
  await expect(page.getByText(/Organize them into places/)).toHaveCount(0);
});

test('Not now hides the offer and it stays hidden after a reload', async ({ page }) => {
  const state = await engine(page, 20);
  await page.goto('/?offer=1');
  await page.getByRole('button', { name: 'Not now' }).click();
  await expect(page.getByText(/Organize them into places/)).toHaveCount(0);
  expect(state.posts).toEqual(['/places/organize/offer/dismiss']);
  await page.reload();
  await page.waitForTimeout(300);
  await expect(page.getByText(/Organize them into places/)).toHaveCount(0);
  expect(await page.evaluate(() => JSON.stringify([localStorage, sessionStorage]))).not.toContain('organize');
});
