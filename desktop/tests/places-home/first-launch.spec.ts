import { test, expect } from '@playwright/test';

const sentence = 'Places hold work that belongs together, with what the AI should know about it. Open a folder or repo to make one, or just name one.';
for (const theme of ['light', 'dark']) {
  test(`d5-pl-test-home: first launch matches Places 8e and names inline · ${theme}`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await page.goto('/?scenario=first');
    await expect(page.getByText(sentence, { exact: true })).toBeVisible();
    await expect(page.getByRole('searchbox')).toHaveCount(0);
    await expect(page.getByTestId('all-places-count')).toHaveCount(0);
    await expect(page.locator('.home-empty-sentence')).toHaveCSS('max-width', '500px');
    await expect(page.locator('.home-empty-sentence')).toHaveCSS('font-size', '15px');
    await expect(page.locator('.home-empty-sentence')).toHaveCSS('line-height', '24px');
    const geometry = await page.locator('.home-column').evaluate(el => {
      const title = el.querySelector('h1')!.getBoundingClientRect();
      const sentence = el.querySelector('.home-empty-sentence')!.getBoundingClientRect();
      const grid = el.querySelector('.places-tile-grid')!;
      return { titleGap: sentence.top - title.bottom, tileGap: grid.getBoundingClientRect().top - sentence.bottom,
        columns: getComputedStyle(grid).gridTemplateColumns.split(' ').length };
    });
    expect(geometry).toEqual({ titleGap: 10, tileGap: 28, columns: 3 });
    const folder = page.getByRole('button', { name: 'Open a folder or repo', exact: true });
    await expect(folder).toHaveCSS('height', '84px');
    await expect(folder.locator('[data-icon="folderOpen"]')).toBeVisible();
    await folder.click();
    await expect(page.getByRole('list', { name: 'Callback log' })).toContainText('openFolder');
    await page.getByRole('button', { name: 'Name a place', exact: true }).click();
    const name = page.getByRole('textbox', { name: 'Place name', exact: true });
    await expect(name).toBeFocused();
    await name.fill('First project');
    await name.press('Enter');
    await expect(page.getByRole('list', { name: 'Callback log' })).toContainText('create:First project:tide:root');
    await expect(page.getByText(sentence, { exact: true })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Open a folder or repo', exact: true })).toHaveCount(0);
  });
}

for (const width of [320, 480, 600, 800, 1200]) {
  test(`d5-pl-test-home: first launch tiles stay reachable at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 560 });
    for (const theme of ['light', 'dark']) {
      await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
      await page.goto('/?scenario=first');
      const name = page.getByRole('button', { name: 'Name a place', exact: true });
      await name.focus();
      await name.press('ArrowRight');
      await expect(page.getByRole('button', { name: 'Open a folder or repo', exact: true })).toBeFocused();
      expect(await page.locator('.home-column').evaluate(el => el.scrollWidth - el.clientWidth)).toBe(0);
    }
  });
}

test('d5-pl-test-home: folder picker uses one canonical write before Go to; cancellation and failure never navigate', async ({ page }) => {
  const requests: { path: string; body: unknown }[] = [];
  let refused = false;
  await page.route('**/api/engine/places**', route => {
    if (route.request().method() === 'GET') return route.fulfill({ json: { generation: 3 } });
    requests.push({ path: new URL(route.request().url()).pathname, body: route.request().postDataJSON() });
    return refused ? route.fulfill({ status: 400, json: {} }) : route.fulfill({ json: {
      revision: 4, noop: false, receipts: [{ id: 'r1', action: 'from-folder', beforeRevision: 3, afterRevision: 4 }], undo: ['r1'],
      place: { id: 'pl_0123456789abcdef', name: 'repo', parents: [], effectiveTint: 'tide', archived: false, pinned: false,
        hasInstructions: false, sourceCount: 1, alsoIn: [], counts: { children: 0, descendants: 0, chats: 0, chatsInclusive: 0 },
        status: { chats: 0, running: 0, needsYou: 0, incomplete: 0, failedTasks: 0 },
        statusInclusive: { chats: 0, running: 0, needsYou: 0, incomplete: 0, failedTasks: 0 },
        instructions: '', sources: [{ id: 'folder', kind: 'folder', ref: '/work/repo', check: { state: 'ok' } }], policy: {} },
    } });
  });
  await page.goto('/?scenario=first');
  const run = (status: string) => page.evaluate(async status => {
    const module = await import('./first-launch-flow.ts');
    return module.firstLaunchFlow(status);
  }, status);
  expect(await run('cancelled')).toEqual({ navigated: [] });
  expect(await run('busy')).toEqual({ navigated: [], error: 'Another chooser is already open.' });
  expect(requests).toEqual([]);
  expect(await run('picked')).toEqual({ navigated: ['pl_0123456789abcdef'] });
  expect(requests).toEqual([{ path: '/api/engine/places/from-folder', body: { path: '/work/repo/nested', ifGeneration: 3 } }]);
  refused = true;
  expect(await run('picked')).toEqual({ navigated: [], error: 'Folder could not be opened.' });
});
