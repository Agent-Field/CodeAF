import { test, expect, type Page } from '@playwright/test';

// Interactions, Flows: "Dragging a tab out of the strip makes a new window".
// In a browser that door is absent, so a release outside the window leaves the
// strip as it was and does not open a tab or stop anything.
const KEY = 'codeaf.desktop.workspace.v1';

async function seed(page: Page, tabs: unknown[], activeId: string) {
  const state = { tabs, groups: [], closed: [], activeId, nextNumber: tabs.length + 1, recentIds: tabs.map(t => (t as { id: string }).id) };
  await page.addInitScript(([key, json]) => {
    const opened: string[] = [];
    (window as unknown as { __opened: string[] }).__opened = opened;
    window.open = (url?: string | URL) => { opened.push(String(url ?? '')); return null; };
    if (!localStorage.getItem(key)) localStorage.setItem(key, json);
  }, [KEY, JSON.stringify(state)] as const);
}

const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: `${title} draft`, titleSource: 'manual', kind: 'conversation', pinned: false, ...over });
const chip = (page: Page, title: string) => page.locator('.workspace-tab').filter({ has: page.getByRole('tab', { name: title, exact: true }) });

async function release(page: Page, title: string, clientX: number, clientY: number) {
  const data = await page.evaluateHandle(() => new DataTransfer());
  const target = chip(page, title);
  await target.dispatchEvent('dragstart', { dataTransfer: data, clientX: 40, clientY: 20 });
  await target.dispatchEvent('dragend', { dataTransfer: data, clientX, clientY, screenX: clientX + 100, screenY: clientY + 100 });
}

test.beforeEach(async ({ page }) => {
  await page.route('**/api/engine/**', route => route.abort());
});

test('letting go outside the window leaves the tab in the strip and opens nothing', async ({ page }) => {
  await seed(page, [tab('a', 'Alpha', { draft: 'alpha words' }), tab('b', 'Beta')], 'a');
  await page.goto('/');
  await expect(page.getByRole('tab', { name: 'Alpha', exact: true })).toHaveAttribute('aria-selected', 'true');
  const box = await page.evaluate(() => ({ width: window.innerWidth, height: window.innerHeight }));
  await release(page, 'Alpha', -8, 12);
  await expect(page.getByRole('tab', { name: 'Alpha', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('tab', { name: 'Beta', exact: true })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('alpha words');
  expect(await page.evaluate(() => (window as unknown as { __opened: string[] }).__opened)).toEqual([]);
  // A release still inside the window is a cancelled drag, not a window either.
  await release(page, 'Beta', Math.floor(box.width / 2), Math.floor(box.height / 2));
  await expect(page.getByRole('tab', { name: 'Alpha', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('tab', { name: 'Beta', exact: true })).toBeVisible();
  expect(await page.evaluate(() => (window as unknown as { __opened: string[] }).__opened)).toEqual([]);
});

test('a pinned tab stays while a retired Inbox slot drops', async ({ page }) => {
  await seed(page, [
    tab('p', 'Pinned notes', { pinned: true }),
    { id: 'inbox', kind: 'inbox', title: 'Inbox', draft: '', titleSource: 'manual', pinned: true },
    tab('a', 'Alpha'),
  ], 'a');
  await page.goto('/');
  await expect(page.getByRole('tab', { name: 'Pinned notes', exact: true })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Inbox', exact: true })).toHaveCount(0);
  await release(page, 'Pinned notes', -20, -20);
  await expect(page.getByRole('tab', { name: 'Pinned notes', exact: true })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Inbox', exact: true })).toHaveCount(0);
  await expect(page.getByRole('tab', { name: 'Alpha', exact: true })).toHaveAttribute('aria-selected', 'true');
  expect(await page.evaluate(() => (window as unknown as { __opened: string[] }).__opened)).toEqual([]);
});
