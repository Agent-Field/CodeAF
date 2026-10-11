import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';

// The rail's Recent section (owner decision 2026-10-11): the last things you closed, one click from the rail. It reads
// the same closed list ⌘⇧T reopens from, so everything here goes through the real strip and the real shortcut.
const KEY = 'codeaf.desktop.workspace.v1';
const chat = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', titleSource: 'manual', kind: 'conversation', pinned: false, ...over });

async function boot(page: Page, theme = 'light', width = 1200, closed: unknown[] = []) {
  await installMockEngine(page, {});
  const workspace = { tabs: [chat('keep', 'Keep me'), chat('one', 'First chat', { draft: 'unsent words' }), chat('two', 'Second chat'), chat('three', 'Third chat')], groups: [], closed, activeId: 'keep', nextNumber: 5, recentIds: ['keep', 'one', 'two', 'three'] };
  await page.setViewportSize({ width, height: 800 });
  await page.addInitScript(([value, mode, key]) => {
    localStorage.setItem('codeaf-theme', mode);
    if (!localStorage.getItem(key)) localStorage.setItem(key, value);
  }, [JSON.stringify(workspace), theme, KEY] as const);
  await page.goto('/');
}

const recent = (page: Page) => page.getByRole('region', { name: 'Recent' });
const row = (page: Page, title: string) => recent(page).locator('.rail-row-main').filter({ hasText: title });
const rowsOf = (page: Page) => recent(page).locator('.rail-row-main').filter({ hasNotText: /^History/ });
const strip = (page: Page) => page.locator('.workspace-tabstrip');

async function closeByKey(page: Page, title: string) {
  await strip(page).getByRole('tab', { name: new RegExp(title) }).click();
  await page.keyboard.press('Control+w');
  await expect(strip(page).getByRole('tab', { name: new RegExp(title) })).toHaveCount(0);
}

test('no closed tab means no Recent section', async ({ page }) => {
  await boot(page);
  await expect(recent(page)).toHaveCount(0);
});

test('closed tabs list newest first; a click reopens with the draft and leaves Recent; x forgets only the row', async ({ page }) => {
  await boot(page);
  for (const title of ['First chat', 'Second chat', 'Third chat']) await closeByKey(page, title);
  await expect(rowsOf(page)).toHaveText([/Third chat/, /Second chat/, /First chat/]);
  await row(page, 'First chat').click();
  await expect(strip(page).getByRole('tab', { name: /First chat/ })).toHaveAttribute('aria-selected', 'true');
  await expect(row(page, 'First chat')).toHaveCount(0);
  await expect(page.getByRole('textbox').first()).toHaveValue('unsent words');
  // Forget leaves the list and nothing else: ⌘⇧T no longer finds it, and no tab appears.
  await row(page, 'Third chat').hover();
  await recent(page).getByRole('button', { name: 'Forget Third chat' }).click();
  await expect(row(page, 'Third chat')).toHaveCount(0);
  await expect(strip(page).getByRole('tab', { name: /Third chat/ })).toHaveCount(0);
  await page.keyboard.press('Control+Shift+t');
  await expect(strip(page).getByRole('tab', { name: /Second chat/ })).toHaveAttribute('aria-selected', 'true');
  await expect(recent(page)).toHaveCount(0);
});

test('the list survives a reload and cmd-shift-T reopens the same newest tab the section shows first', async ({ page }) => {
  await boot(page);
  await closeByKey(page, 'First chat');
  await closeByKey(page, 'Second chat');
  await page.reload();
  await expect(row(page, 'Second chat')).toBeVisible();
  await page.keyboard.press('Control+Shift+t');
  await expect(strip(page).getByRole('tab', { name: /Second chat/ })).toHaveAttribute('aria-selected', 'true');
  await expect(row(page, 'Second chat')).toHaveCount(0);
  await expect(row(page, 'First chat')).toBeVisible();
});

test('the header folds the section, the fold is remembered, and the History link opens History', async ({ page }) => {
  await boot(page);
  await closeByKey(page, 'First chat');
  const head = recent(page).getByRole('button', { name: 'Recent' });
  await expect(head).toHaveAttribute('aria-expanded', 'true');
  await head.click();
  await expect(rowsOf(page)).toHaveCount(0);
  await page.reload();
  await expect(recent(page).getByRole('button', { name: 'Recent' })).toHaveAttribute('aria-expanded', 'false');
  await recent(page).getByRole('button', { name: 'Recent' }).click();
  await row(page, 'History').click();
  await expect(strip(page).getByRole('tab', { name: /History/ })).toHaveAttribute('aria-selected', 'true');
});

test('the context menu opens, opens in split, forgets and clears', async ({ page }) => {
  await boot(page);
  for (const title of ['First chat', 'Second chat', 'Third chat']) await closeByKey(page, title);
  await row(page, 'Second chat').click({ button: 'right' });
  await expect(page.getByRole('menuitem')).toHaveText(['Open', 'Open in split', 'Forget', 'Clear recent']);
  await page.getByRole('menuitem', { name: 'Forget' }).click();
  await expect(row(page, 'Second chat')).toHaveCount(0);
  await row(page, 'Third chat').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Clear recent' }).click();
  await expect(recent(page)).toHaveCount(0);
});

for (const theme of ['light', 'dark']) {
  test(`rows use the rail's own ink: title ink-2, age ink-3, no coloured text (${theme})`, async ({ page }) => {
    await boot(page, theme);
    await closeByKey(page, 'First chat');
    const colors = await page.evaluate(() => {
      const read = (el: Element | null) => (el ? getComputedStyle(el).color : '');
      const sibling = document.querySelector('.rail-foot .nav-item:not(.active)');
      const row = document.querySelector('.rail-recent .rail-row-main');
      return { title: read(row), age: read(row?.querySelector('.rail-meta') ?? null), place: read(sibling) };
    });
    expect(colors.title).toBe(colors.place);
    expect(colors.age).not.toBe(colors.title);
  });
}

test('at 320px the drawer reaches the section and a row opens a tab', async ({ page }) => {
  // The strip is not on screen in a drawer-width window, so the closed list is seeded rather than closed by hand.
  await boot(page, 'light', 320, [{ ...chat('gone', 'First chat', { draft: 'unsent words' }), closedAt: Date.now() - 120_000 }]);
  await page.getByRole('button', { name: 'Show sidebar' }).click();
  const drawer = page.getByRole('dialog', { name: 'Navigation', exact: true });
  const row = drawer.getByRole('region', { name: 'Recent' }).locator('.rail-row-main').filter({ hasText: 'First chat' });
  await expect(row).toBeVisible();
  const box = await row.boundingBox();
  expect(box!.x + box!.width).toBeLessThanOrEqual(320);
  await row.click();
  await expect(drawer.getByRole('region', { name: 'Recent' })).toHaveCount(0);
});
