import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from '../ui/support/mock-engine';
import { question } from '../ui/support/scenarios';

// More cases of the complete tabs audit (docs/TABS-COMPLETE-AUDIT.md); same rules as tabs-audit.spec.ts: design
// assertions, real pointer and keyboard input, real HTML drags.

const KEY = 'codeaf.desktop.workspace.v1';
const SESSION = 'mock-session-1.jsonl';
type Seed = { tabs: unknown[]; groups?: unknown[]; activeId: string };
const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', pinned: false, kind: 'conversation', titleSource: 'manual', ...over });
async function seed(page: Page, value: Seed) {
 const state = { groups: [], closed: [], nextNumber: value.tabs.length + 1, recentIds: value.tabs.map(t => (t as { id: string }).id), ...value };
 await page.addInitScript(([key, json]) => { if (!sessionStorage.getItem('audit-seeded')) { localStorage.setItem(key, json); sessionStorage.setItem('audit-seeded', '1'); } }, [KEY, JSON.stringify(state)] as const);
}
const saved = (page: Page) => page.evaluate(key => JSON.parse(localStorage.getItem(key) ?? 'null'), KEY);
const tabNamed = (page: Page, name: string) => page.getByRole('tab', { name, exact: true });
const stripOrder = (page: Page) => page.locator('.workspace-tabstrip').evaluate(strip => Array.from(strip.querySelectorAll('[role="tab"]')).filter(el => !el.closest('[inert]')).map(el => el.getAttribute('aria-label')));
const groupLabel = (page: Page, name: string) => page.locator('.workspace-group-label', { has: page.locator('.workspace-group-name', { hasText: new RegExp(`^${name}$`) }) });
const tokenColor = (page: Page, token: string) => page.evaluate(name => { const p = document.createElement('i'); p.style.color = `var(--${name})`; document.body.append(p); const c = getComputedStyle(p).color; p.remove(); return c; }, token);
const offline = (page: Page) => page.route('**/api/engine/**', route => route.abort());

test('TA-STRIP-12 an inactive tab that needs you lifts its title to full ink; a quiet one stays ink-2', async ({ page }) => {
 await installMockEngine(page, { initial: { title: 'Storage', entries: [], running: true, needsPerson: true, questions: [question] } });
 await seed(page, { tabs: [tab('live', 'Storage', { sessionFile: SESSION }), tab('quiet', 'Quiet'), tab('here', 'Here')], activeId: 'here' });
 await page.goto('/');
 const live = page.locator('.workspace-tab', { has: tabNamed(page, 'Storage') });
 await expect(live.locator('.tab-dot[data-state="waiting"]')).toBeVisible({ timeout: 15_000 });
 await expect(live).toHaveCSS('color', await tokenColor(page, 'ink'));
 await expect(page.locator('.workspace-tab', { has: tabNamed(page, 'Quiet') })).toHaveCSS('color', await tokenColor(page, 'ink-2'));
});

test('TA-STRIP-18 the hover preview goes away when a drag starts', async ({ page }) => {
 await offline(page);
 await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta', { draft: 'beta words' }), tab('c', 'Gamma')], activeId: 'a' });
 await page.goto('/');
 const box = (await tabNamed(page, 'Beta').boundingBox())!;
 await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
 const card = page.getByRole('group', { name: 'Preview of Beta' });
 await expect(card).toBeVisible({ timeout: 3000 });
 await page.mouse.down();
 await page.mouse.move(box.x + box.width / 2 + 30, box.y + box.height / 2 + 6, { steps: 6 });
 await expect(card).toBeHidden();
 await page.mouse.up();
});

test('TA-PIN-03 pinned tabs keep a predictable order: strip order, before a hairline, across a reload', async ({ page }) => {
 await offline(page);
 await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma')], activeId: 'b' });
 await page.goto('/');
 for (const name of ['Gamma', 'Alpha']) {
  await tabNamed(page, name).click({ button: 'right' });
  await page.getByRole('menuitem', { name: /^Pin tab/ }).click();
 }
 expect(await stripOrder(page)).toEqual(['Alpha', 'Gamma', 'Beta']);
 await expect(page.locator('.workspace-tabstrip > [role="separator"]')).toHaveCount(1);
 await expect(page.locator('.workspace-tab.is-pinned .workspace-tab-close')).toHaveCount(0);
 await page.reload();
 expect(await stripOrder(page)).toEqual(['Alpha', 'Gamma', 'Beta']);
});

test('TA-GRP-05 a collapsed group with a member that needs you carries the amber dot on its pill', async ({ page }) => {
 await installMockEngine(page, { initial: { title: 'Storage', entries: [], running: true, needsPerson: true, questions: [question] } });
 await seed(page, { tabs: [tab('here', 'Here'), tab('live', 'Storage', { sessionFile: SESSION, groupId: 'g' }), tab('b', 'Beta', { groupId: 'g' })], groups: [{ id: 'g', title: 'Release v2.4', collapsed: true }], activeId: 'here' });
 await page.goto('/');
 const label = groupLabel(page, 'Release v2.4');
 await expect(label.locator('.tab-dot[data-state="waiting"]')).toBeVisible({ timeout: 15_000 });
 await expect(label.locator('.workspace-group-count')).toHaveText('2');
});

test('TA-GRP-06 the group capsule has the design geometry: tab-hover fill, 12px medium label, 26px members', async ({ page }) => {
 await offline(page);
 await seed(page, { tabs: [tab('a', 'Alpha', { groupId: 'g' }), tab('b', 'Beta', { groupId: 'g' })], groups: [{ id: 'g', title: 'Trailing commas', collapsed: false }], activeId: 'a' });
 await page.goto('/');
 const capsule = page.locator('.workspace-tab-group');
 await expect(capsule).toHaveCSS('background-color', await tokenColor(page, 'tab-hover'));
 const label = groupLabel(page, 'Trailing commas');
 await expect(label).toHaveCSS('font-size', '12px');
 await expect(label).toHaveCSS('font-weight', '500');
 expect((await page.locator('.workspace-tab', { has: tabNamed(page, 'Beta') }).boundingBox())!.height).toBe(26);
});

test('TA-GRP-16 Ungroup keeps every member open, in order, with no empty capsule left', async ({ page }) => {
 await offline(page);
 await seed(page, { tabs: [tab('l', 'Loose'), tab('a', 'Alpha', { groupId: 'g' }), tab('b', 'Beta', { groupId: 'g' })], groups: [{ id: 'g', title: 'Bench', collapsed: false }], activeId: 'a' });
 await page.goto('/');
 await groupLabel(page, 'Bench').click({ button: 'right' });
 await page.getByRole('menuitem', { name: /^Ungroup/ }).click();
 await expect(page.locator('.workspace-tab-group')).toHaveCount(0);
 expect(await stripOrder(page)).toEqual(['Loose', 'Alpha', 'Beta']);
 expect((await saved(page)).groups).toEqual([]);
});

test('TA-MENU-08 menu rows are 28px (Components "Shell · menus")', async ({ page }) => {
 await offline(page);
 await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta')], activeId: 'a' });
 await page.goto('/');
 await tabNamed(page, 'Beta').click({ button: 'right' });
 const row = page.getByRole('menuitem').first();
 await expect(row).toBeVisible();
 expect((await row.boundingBox())!.height).toBe(28);
});

test('TA-SPLIT-12 at 600px a 2×2 split never overflows the page and every pane keeps its controls on screen', async ({ page }) => {
 await offline(page);
 const panes = [tab('p1', 'One'), tab('p2', 'Two'), tab('p3', 'Three'), tab('p4', 'Four')];
 await seed(page, { tabs: [{ id: 'sp', title: 'Split', draft: '', pinned: false, kind: 'conversation', split: { layout: '2x2', focus: 0, panes } }], activeId: 'sp' });
 await page.setViewportSize({ width: 600, height: 560 });
 await page.goto('/');
 expect(await page.evaluate(() => document.scrollingElement!.scrollWidth <= innerWidth)).toBe(true);
 // The design is silent below 800px (planner task t-d5-sh-split-narrow proposes focused-pane-only); until that is
 // decided this case asserts only what the responsive contract demands: nothing clipped, nothing off screen.
 for (const name of ['One', 'Two', 'Three', 'Four']) {
  const menu = (await page.getByRole('button', { name: `Pane menu ${name}`, exact: true }).boundingBox())!;
  expect(menu.x + menu.width, `${name}'s pane menu is off screen`).toBeLessThanOrEqual(600);
 }
 test.info().annotations.push({ type: 'measured', description: `focused pane width ${(await page.getByRole('region', { name: 'One', exact: true }).boundingBox())!.width}px at 600px` });
});

test('TA-SPLIT-13 the X of a three- or four-pane split is not covered by its segment titles', async ({ page }) => {
 await offline(page);
 const panes = [tab('p1', 'Left pane'), tab('p2', 'Right pane'), tab('p3', 'Bottom pane')];
 await seed(page, { tabs: [tab('a', 'Alpha'), { id: 'sp', title: 'Split', draft: '', pinned: false, kind: 'conversation', split: { layout: '2x2', focus: 0, panes } }], activeId: 'sp' });
 await page.goto('/');
 const close = page.getByRole('button', { name: /^Close split / });
 const box = (await close.boundingBox())!;
 const hit = await page.evaluate(([x, y]) => { const el = document.elementFromPoint(x, y); return el?.closest('button')?.getAttribute('aria-label') ?? el?.tagName; }, [box.x + box.width / 2, box.y + box.height / 2] as const);
 expect(hit, 'a click on the split\'s X lands on something else').toMatch(/^Close split /);
 await close.click({ timeout: 3000 });
 await expect(page.locator('.workspace-split-tab')).toHaveCount(0);
});
