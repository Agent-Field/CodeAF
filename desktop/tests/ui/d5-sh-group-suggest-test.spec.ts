import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { withTasks } from './support/scenarios';
import { expectAccessible } from './contracts';

// SH-108/271 (⌘G), SH-109 (a task joins its conversation's group), SH-110/111/112 (the suggestion pill in the real shell).
// Fixtures live in this spec only: tabs are seeded through the workspace document, workspaces through the mock world feed.
const KEY = 'codeaf.desktop.workspace.v1';
const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', pinned: false, kind: 'conversation', titleSource: 'manual', ...over });
const seed = (page: Page, tabs: unknown[], groups: unknown[] = []) => page.addInitScript(([key, json]) => {
 if (!sessionStorage.getItem('seeded')) { localStorage.setItem(key, json); sessionStorage.setItem('seeded', '1'); }
}, [KEY, JSON.stringify({ tabs, groups, closed: [], activeId: (tabs[0] as { id: string }).id, nextNumber: 20, recentIds: (tabs as { id: string }[]).map(t => t.id) })] as const);
const primary = (page: Page) => page.evaluate(() => (/Mac/.test(navigator.platform) ? 'Meta' : 'Control')) as Promise<'Meta' | 'Control'>;

for (const scheme of ['light', 'dark'] as const) {
 test(`⌘G groups two ⌘-clicked tabs, Esc clears first, in ${scheme}`, async ({ page }) => {
  await page.emulateMedia({ colorScheme: scheme });
  await page.route('**/api/engine/**', route => route.abort());
  await seed(page, [tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma')]);
  await page.goto('/');
  const mod = await primary(page);
  const beta = page.getByRole('tab', { name: 'Beta', exact: true });
  const gamma = page.getByRole('tab', { name: 'Gamma', exact: true });
  await beta.click({ modifiers: [mod] });
  await gamma.click({ modifiers: [mod] });
  await expect(page.locator('.workspace-tab[data-selected]')).toHaveCount(2);
  await expectAccessible(page, '.workspace-tabstrip');
  await page.keyboard.press('Escape');
  await expect(page.locator('.workspace-tab[data-selected]')).toHaveCount(0);
  await expect(page.locator('.workspace-tab-group')).toHaveCount(0);
  await beta.click({ modifiers: [mod] });
  await gamma.click({ modifiers: [mod] });
  await page.keyboard.press(`${mod}+g`);
  await page.keyboard.press('Enter');
  const group = page.locator('.workspace-tab-group');
  await expect(group).toHaveCount(1);
  await expect(group.getByRole('tab')).toHaveText([/Beta/, /Gamma/]);
  await expect(page.locator('.workspace-tab[data-selected]')).toHaveCount(0);
  await expectAccessible(page, '.workspace-tabstrip');
 });
}

test('a task opened from a grouped conversation lands in that group, not at the strip end', async ({ page }) => {
 await installMockEngine(page, withTasks());
 await seed(page, [tab('conv', 'Settings migration', { groupId: 'G', sessionFile: 'mock-session-1.jsonl' }), tab('other', 'Other')], [{ id: 'G', title: 'Release', collapsed: false }]);
 await page.goto('/');
 const row = page.locator('.turn-v2 .task-notice-row');
 await expect(row).toBeVisible();
 await row.click({ button: 'middle' });
 const group = page.locator('.workspace-tab-group');
 await expect(group.getByRole('tab')).toHaveCount(2);
 await expect(group.getByRole('tab').first()).toHaveAttribute('aria-selected', 'true');
 await expect(page.getByRole('tab')).toHaveCount(3);
 await expect(page.getByRole('tab').last()).toHaveAccessibleName('Other');
});

const chats = ['aaaaaaaa00000001', 'bbbbbbbb00000002', 'cccccccc00000003'];
async function openBench(page: Page, workspaces = ['/work/bench', '/work/bench', '/work/bench']) {
 await installMockEngine(page, { world: { rows: chats.map((chatId, i) => ({ chatId, workspace: workspaces[i], title: chatId, running: false, needsYou: 0, failed: 0, tasksRunning: 0, tasksTotal: 0, attached: false, archived: false })), items: [] } });
 await seed(page, chats.map((chat, i) => tab(`t${i}`, ['Bench one', 'Bench two', 'Bench three'][i], { sessionFile: `/fixture/history/${chat}/transcript.jsonl` })));
 await page.goto('/');
}
const pill = (page: Page) => page.getByRole('status').filter({ hasText: 'Group the' });

for (const scheme of ['light', 'dark'] as const) for (const width of [320, 600, 1200]) {
 test(`three tabs on one workspace show the pill once and Group groups them · ${scheme} ${width}px`, async ({ page }) => {
  await page.emulateMedia({ colorScheme: scheme });
  await page.setViewportSize({ width, height: 800 });
  await openBench(page);
  await expect(pill(page)).toHaveCount(1);
  await expect(pill(page)).toContainText('Group the 3 bench tabs as bench?');
  const frame = (await page.locator('.workspace-conversation').first().boundingBox())!;
  const box = (await pill(page).boundingBox())!;
  expect(box.x).toBeGreaterThanOrEqual(frame.x - 0.5);
  expect(box.x + box.width).toBeLessThanOrEqual(frame.x + frame.width + 0.5);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await expectAccessible(page, '.suggest-pill');
  await pill(page).getByRole('button', { name: 'Group', exact: true }).click();
  await expect(pill(page)).toHaveCount(0);
  await expect(page.locator('.workspace-tab-group').getByRole('tab')).toHaveCount(3);
 });
}

test('× hides the pill for the session, including after switching tabs', async ({ page }) => {
 await openBench(page);
 await pill(page).getByRole('button', { name: 'Dismiss', exact: true }).click();
 await expect(pill(page)).toHaveCount(0);
 await page.getByRole('tab', { name: 'Bench two', exact: true }).click();
 await page.getByRole('tab', { name: 'Bench one', exact: true }).click();
 await expect(pill(page)).toHaveCount(0);
 await expect(page.locator('.workspace-tab-group')).toHaveCount(0);
});

test('two of three tabs on one workspace draw no pill', async ({ page }) => {
 await openBench(page, ['/work/bench', '/work/bench', '/work/other']);
 await expect(page.getByRole('tab', { name: 'Bench three', exact: true })).toBeVisible();
 await expect(pill(page)).toHaveCount(0);
});
