import { savedWorkspace } from '../ui/support/synced-workspace';
import { test, expect, type Locator, type Page } from '@playwright/test';
import { expectAccessible } from '../ui/contracts';
import { installMockEngine, type Scenario } from '../ui/support/mock-engine';
import { plainReply, withTasks } from '../ui/support/scenarios';
import { openApp, send } from '../ui/support/conversation';

// The complete tabs audit (docs/TABS-COMPLETE-AUDIT.md). Each test is one case of that inventory, named by its id,
// and asserts what the design draws (Shell, Components, Interactions pages of the current ZIP), not what the code does
// today. A failing case is the reproduction for its finding. Every journey drives the real app with real pointer and
// keyboard input; a drag is a real mouse drag (Playwright's native HTML drag), never a synthetic DragEvent.

const KEY = 'codeaf.desktop.workspace.v1';
const SESSION = 'mock-session-1.jsonl';
type Seed = { tabs: unknown[]; groups?: unknown[]; closed?: unknown[]; activeId: string };

const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', pinned: false, kind: 'conversation', titleSource: 'manual', ...over });

/** Seeds the saved workspace once per test (a reload reads what the app saved, not the seed). */
async function seed(page: Page, value: Seed) {
 const state = { groups: [], closed: [], nextNumber: value.tabs.length + 1, recentIds: value.tabs.map(t => (t as { id: string }).id), ...value };
 await page.addInitScript(([key, json]) => { if (!sessionStorage.getItem('audit-seeded')) { localStorage.setItem(key, json); sessionStorage.setItem('audit-seeded', '1'); } }, [KEY, JSON.stringify(state)] as const);
}
const saved = savedWorkspace;
const tabNamed = (page: Page, name: string) => page.getByRole('tab', { name, exact: true });
/** The strip's tabs in reading order, as a person sees them (hidden members of a collapsed group excluded). */
const stripOrder = (page: Page) => page.locator('.workspace-tabstrip').evaluate(strip => Array.from(strip.querySelectorAll('[role="tab"]')).filter(el => !el.closest('[inert]')).map(el => el.getAttribute('aria-label')));
/** The strip's top-level items in order: a tab's title, or "group:<label>" for a capsule. */
const stripItems = (page: Page) => page.locator('.workspace-tabstrip').evaluate(strip => Array.from(strip.children).flatMap(el => el.classList.contains('workspace-tab-group') ? [`group:${el.querySelector('.workspace-group-name')?.textContent}`] : el.matches('.workspace-tab') ? [el.querySelector('[role="tab"]')?.getAttribute('aria-label') ?? ''] : []));
const groupLabel = (page: Page, name: string) => page.locator('.workspace-group-label', { has: page.locator('.workspace-group-name', { hasText: new RegExp(`^${name}$`) }) });
const menuNames = async (page: Page) => (await page.getByRole('menu').last().getByRole('menuitem').allTextContents()).map(text => text.replace(/\s+/g, ' ').trim());
const primary = async (page: Page) => (await page.evaluate(() => /Mac/.test(navigator.platform)) ? 'Meta' : 'Control');
/** Baseline spells these "Move to group" / "Create group"; design 3g spells them "Add to group" / "New group…". Either opens the same journey. */
const groupMenu = /^(Move|Add) to group/;
const newGroup = /^(Create group|New group)/;
const offline = (page: Page) => page.route('**/api/engine/**', route => route.abort());

async function openTabMenu(page: Page, name: string) {
 await tabNamed(page, name).click({ button: 'right' });
 await expect(page.getByRole('menu')).toBeVisible();
}
async function chooseInMenu(page: Page, ...path: (string | RegExp)[]) {
 for (const [index, item] of path.entries()) {
  const entry = page.getByRole('menuitem', { name: typeof item === 'string' ? new RegExp(`^${item.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`) : item }).last();
  await expect(entry, `the menu has no "${item}"`).toBeVisible({ timeout: 3000 });
  if (index < path.length - 1) { await entry.hover(); await entry.press('ArrowRight').catch(() => undefined); }
  else await entry.click();
 }
}
/** A real mouse drag from the middle of `from` to a fraction across `to` (0 = left edge, 1 = right edge). */
async function drag(page: Page, from: Locator, to: Locator, fraction = 0.5) {
 const a = (await from.boundingBox())!;
 const b = (await to.boundingBox())!;
 await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2);
 await page.mouse.down();
 await page.mouse.move(a.x + a.width / 2 + 10, a.y + a.height / 2 + 4, { steps: 4 });
 await page.mouse.move(b.x + b.width * fraction, b.y + b.height / 2, { steps: 10 });
 await page.mouse.move(b.x + b.width * fraction + 1, b.y + b.height / 2);
 await page.mouse.up();
}

test.describe('strip and tab primitive', () => {
 test.beforeEach(({ page }) => offline(page));

 test('TA-STRIP-07 a truncated title shows its full name in a tooltip after 500ms', async ({ page }) => {
  const long = 'Trailing commas across the config stack and env loader';
  await seed(page, { tabs: [tab('a', long), tab('b', 'Short')], activeId: 'a' });
  await page.goto('/');
  await tabNamed(page, long).hover();
  await page.waitForTimeout(700);
  // Shell 3l "Long title · full name": the full title is in the 500ms tooltip (the active tab has no hover preview).
  const tooltip = page.getByRole('tooltip');
  const native = await tabNamed(page, long).getAttribute('title');
  expect(native === long || (await tooltip.count()) > 0 && (await tooltip.first().textContent())?.includes(long), 'no tooltip carries the full title').toBe(true);
 });

 test('TA-STRIP-11 running is silent on a tab; needs you lifts an amber glyph that clears when the engine stops asking', async ({ page }) => {
  const engine = await installMockEngine(page, { initial: { title: 'Storage', entries: [], running: true, needsPerson: false, questions: [] } });
  await seed(page, { tabs: [tab('live', 'Storage', { sessionFile: SESSION }), tab('here', 'Here')], activeId: 'here' });
  await page.goto('/');
  const live = page.locator('.workspace-tab', { has: tabNamed(page, 'Storage') });
  // Running only: no dot, no spinner, no count (Shell 3j footnote).
  await page.waitForTimeout(2500);
  await expect(live.locator('.tab-dot')).toHaveCount(0);
  await expect(live.locator('.tab-badge, [role="progressbar"], .spinner')).toHaveCount(0);
  engine.update({ needsPerson: true, questions: [{ id: 3, kind: 'choice', ask: 'Pick one?', head: 'Pick one', options: [{ key: 'a', label: 'A' }, { key: 'b', label: 'B' }] }] });
  await expect(live.locator('.tab-dot[data-state="waiting"]')).toBeVisible({ timeout: 15_000 });
  await expect(tabNamed(page, 'Storage')).toHaveAttribute('aria-description', 'Needs you');
  // Status must not go stale: once the engine stops asking, the glyph goes.
  engine.update({ needsPerson: false, questions: [] });
  await expect(live.locator('.tab-dot')).toHaveCount(0, { timeout: 15_000 });
 });
});

test.describe('groups', () => {
 test.beforeEach(({ page }) => offline(page));

 test('TA-GRP-02 a renamed-to-empty group never takes a name another group already has', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha', { groupId: 'g1' }), tab('b', 'Beta', { groupId: 'g2' })], groups: [{ id: 'g1', title: 'New group', collapsed: false }, { id: 'g2', title: 'Docs', collapsed: false }], activeId: 'a' });
  await page.goto('/');
  await groupLabel(page, 'Docs').click({ button: 'right' });
  await page.getByRole('menuitem', { name: /^Rename/ }).click();
  const dialog = page.getByRole('dialog', { name: 'Rename group' });
  await dialog.getByRole('textbox', { name: 'Name' }).fill('');
  await dialog.getByRole('button', { name: 'Save' }).click();
  const titles = (await saved(page)).groups.map((g: { title: string }) => g.title.toLowerCase());
  expect(new Set(titles).size, `group names after rename: ${titles.join(', ')}`).toBe(titles.length);
 });

 test('TA-GRP-04 collapsing keeps the active member beside "Label N"; hidden members leave keys and ⌘1–9', async ({ page }) => {
  await seed(page, { tabs: [tab('d', 'Loose'), tab('a', 'Alpha', { groupId: 'g' }), tab('b', 'Beta', { groupId: 'g' }), tab('c', 'Gamma', { groupId: 'g' })], groups: [{ id: 'g', title: 'Trailing commas', collapsed: false }], activeId: 'a' });
  await page.goto('/');
  const label = groupLabel(page, 'Trailing commas');
  await label.click();
  await expect(label).toHaveAttribute('aria-expanded', 'false');
  await expect(label.locator('.workspace-group-count')).toHaveText('3');
  expect(await stripOrder(page)).toEqual(['Loose', 'Alpha']);
  // Arrow keys walk only what is shown.
  await tabNamed(page, 'Alpha').focus();
  await page.keyboard.press('ArrowLeft');
  await expect(tabNamed(page, 'Loose')).toHaveAttribute('aria-selected', 'true');
  // With the active tab outside the group, the collapsed group shows only its pill.
  expect(await stripOrder(page)).toEqual(['Loose']);
  const mod = await primary(page);
  await page.keyboard.press(`${mod}+9`);
  await expect(tabNamed(page, 'Loose')).toHaveAttribute('aria-selected', 'true');
  await label.click();
  expect(await stripOrder(page)).toEqual(['Loose', 'Alpha', 'Beta', 'Gamma']);
  await page.reload();
  expect(await stripOrder(page)).toEqual(['Loose', 'Alpha', 'Beta', 'Gamma']);
 });

 test('TA-GRP-07 a group keeps its place in the strip when it is made (design 2b draws a group before loose tabs)', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma')], activeId: 'a' });
  await page.goto('/');
  await openTabMenu(page, 'Alpha');
  await chooseInMenu(page, groupMenu, newGroup);
  await expect(page.locator('.workspace-tab-group')).toHaveCount(1);
  expect(await stripItems(page)).toEqual(['group:New group', 'Beta', 'Gamma']);
 });

 test('TA-GRP-09 a new tab opens at the end of the strip, after every group', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta', { groupId: 'g' })], groups: [{ id: 'g', title: 'Benchmarks', collapsed: false }], activeId: 'a' });
  await page.goto('/');
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  await expect(tabNamed(page, 'New tab')).toHaveAttribute('aria-selected', 'true');
  expect((await stripItems(page)).at(-1)).toBe('New tab');
 });

 test('TA-GRP-10 ⌘-click selects tabs and ⌘G groups the selection', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma')], activeId: 'a' });
  await page.goto('/');
  const mod = await primary(page);
  await tabNamed(page, 'Beta').click({ modifiers: [mod] });
  await tabNamed(page, 'Gamma').click({ modifiers: [mod] });
  await page.keyboard.press(`${mod}+g`);
  await expect(page.locator('.workspace-tab-group')).toHaveCount(1);
  const members = (await saved(page)).tabs.filter((t: { groupId?: string }) => t.groupId).map((t: { title: string }) => t.title);
  expect(members.sort()).toEqual(['Alpha', 'Beta', 'Gamma']);
 });

 test('TA-GRP-12 dragging a group label moves the whole group', async ({ page }) => {
  await seed(page, { tabs: [tab('l', 'Loose'), tab('a', 'Alpha', { groupId: 'g' }), tab('b', 'Beta', { groupId: 'g' })], groups: [{ id: 'g', title: 'Bench', collapsed: false }], activeId: 'l' });
  await page.goto('/');
  await drag(page, groupLabel(page, 'Bench'), tabNamed(page, 'Loose'), 0.1);
  expect(await stripItems(page)).toEqual(['group:Bench', 'Loose']);
 });

 test('TA-GRP-13 dragging a member onto the outer quarter of another member reorders inside the group', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha', { groupId: 'g' }), tab('b', 'Beta', { groupId: 'g' }), tab('c', 'Gamma', { groupId: 'g' })], groups: [{ id: 'g', title: 'Bench', collapsed: false }], activeId: 'a' });
  await page.goto('/');
  await drag(page, tabNamed(page, 'Gamma'), tabNamed(page, 'Alpha'), 0.1);
  await expect.poll(() => stripOrder(page)).toEqual(['Gamma', 'Alpha', 'Beta']);
  expect((await saved(page)).tabs.every((t: { groupId?: string }) => t.groupId === 'g')).toBe(true);
 });

 test('TA-GRP-14 dragging a tab onto the middle of a member of another group moves it across groups', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha', { groupId: 'g1' }), tab('b', 'Beta', { groupId: 'g2' })], groups: [{ id: 'g1', title: 'One', collapsed: false }, { id: 'g2', title: 'Two', collapsed: false }], activeId: 'a' });
  await page.goto('/');
  await drag(page, tabNamed(page, 'Beta'), tabNamed(page, 'Alpha'), 0.5);
  await expect.poll(async () => (await saved(page)).tabs.find((t: { id: string }) => t.id === 'b')?.groupId).toBe('g1');
  // The emptied group is gone (no empty capsule).
  await expect(groupLabel(page, 'Two')).toHaveCount(0);
 });

 test('TA-GRP-15 a small pointer wobble on a tab is a click, not a reorder or a group (drag threshold)', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma')], activeId: 'a' });
  await page.goto('/');
  const box = (await tabNamed(page, 'Beta').boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 2, box.y + box.height / 2 + 1, { steps: 2 });
  await page.mouse.up();
  await expect(tabNamed(page, 'Beta')).toHaveAttribute('aria-selected', 'true');
  expect(await stripOrder(page)).toEqual(['Alpha', 'Beta', 'Gamma']);
  expect((await saved(page)).groups).toEqual([]);
 });

 test('TA-GRP-17 a task opened from a grouped conversation joins its group', async ({ page }) => {
  const base = withTasks();
  const aside = { Role: 'aside', Text: 'Migrate the settings screen done · ran 30s · Two parts finished.', TaskIDs: ['2'] };
  await installMockEngine(page, { ...base, initial: { title: base.initial.title, entries: [] }, turns: [{ entries: [aside, base.initial.entries!.at(-1)!] as NonNullable<Scenario['initial']['entries']>, patch: { tasks: base.initial.tasks } }] });
  await openApp(page);
  await send(page, 'Migrate the settings screen');
  const panel = page.getByRole('complementary', { name: 'Tasks' });
  await expect(panel).toBeVisible();
  const title = (await page.getByRole('tab').first().getAttribute('aria-label'))!;
  await openTabMenu(page, title);
  await chooseInMenu(page, groupMenu, newGroup);
  await expect(page.locator('.workspace-tab-group')).toHaveCount(1);
  await panel.getByRole('button', { name: /^(?!Collapse|Expand).*Port the form fields/ }).click({ modifiers: [await primary(page)] });
  await expect(page.getByRole('tab')).toHaveCount(2);
  const groups = (await saved(page)).tabs.map((t: { groupId?: string }) => t.groupId);
  expect(groups[1], 'the task tab is outside the conversation\'s group').toBe(groups[0]);
 });
});

test.describe('menus', () => {
 test.beforeEach(({ page }) => offline(page));

 test('TA-MENU-01 the tab menu offers the design 3g actions', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma')], activeId: 'a' });
  await page.goto('/');
  await tabNamed(page, 'Beta').focus();
  await page.keyboard.press('Shift+F10');
  await expect(page.getByRole('menu')).toBeVisible();
  const names = await menuNames(page);
  const missing = ['Open in split', 'Add to group', 'Pin tab', 'Duplicate', 'Close tab', 'Close other tabs', 'Close tabs to the right'].filter(want => !names.some(name => name.startsWith(want)));
  expect(missing, `menu has: ${names.join(' | ')}`).toEqual([]);
 });

 test('TA-GRP-19 the group label menu offers Rename, Open as split, Collapse, Ungroup and Close N tabs', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha', { groupId: 'g' }), tab('b', 'Beta', { groupId: 'g' })], groups: [{ id: 'g', title: 'Bench', collapsed: false }], activeId: 'a' });
  await page.goto('/');
  await groupLabel(page, 'Bench').click({ button: 'right' });
  const names = await menuNames(page);
  const missing = ['Rename', 'Open as split', 'Collapse', 'Ungroup', 'Close 2 tabs'].filter(want => !names.some(name => name.startsWith(want)));
  expect(missing, `menu has: ${names.join(' | ')}`).toEqual([]);
 });

 test('TA-MENU-04 Duplicate puts a copy with the same draft right after the tab', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha', { draft: 'keep this draft' }), tab('b', 'Beta')], activeId: 'a' });
  await page.goto('/');
  await openTabMenu(page, 'Alpha');
  await chooseInMenu(page, 'Duplicate');
  expect(await stripOrder(page)).toEqual(['Alpha', 'Alpha', 'Beta']);
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('keep this draft');
 });

 test('TA-MENU-05 Close other tabs and Close tabs to the right keep pinned tabs and reopen one by one', async ({ page }) => {
  await seed(page, { tabs: [tab('p', 'Pinned', { pinned: true }), tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma')], activeId: 'a' });
  await page.goto('/');
  await openTabMenu(page, 'Alpha');
  await chooseInMenu(page, 'Close tabs to the right');
  expect(await stripOrder(page)).toEqual(['Pinned', 'Alpha']);
  await page.keyboard.press(`${await primary(page)}+Shift+t`);
  await openTabMenu(page, 'Alpha');
  await chooseInMenu(page, 'Close other tabs');
  expect(await stripOrder(page)).toEqual(['Pinned', 'Alpha']);
 });
});

test.describe('closing, reopening and undo', () => {
 test('TA-CLOSE-01 closing a running tab detaches only: the engine is never told to stop', async ({ page }) => {
  const engine = await installMockEngine(page, { initial: { title: 'Long job', entries: [], running: true } });
  await seed(page, { tabs: [tab('live', 'Long job', { sessionFile: SESSION }), tab('here', 'Here')], activeId: 'live' });
  await page.goto('/');
  await expect(tabNamed(page, 'Long job')).toHaveAttribute('aria-selected', 'true');
  await page.waitForTimeout(800);
  await page.getByRole('button', { name: 'Close Long job', exact: true }).click();
  await expect(tabNamed(page, 'Long job')).toHaveCount(0);
  await page.waitForTimeout(800);
  expect(engine.calls.filter(c => c.method === 'POST' && /\/(stop|cancel|close)$/.test(c.path))).toEqual([]);
 });

 test('TA-CLOSE-02 holding ⌥ over a running tab turns its X into a stop square (Close and stop)', async ({ page }) => {
  await installMockEngine(page, { initial: { title: 'Long job', entries: [], running: true } });
  await seed(page, { tabs: [tab('live', 'Long job', { sessionFile: SESSION }), tab('here', 'Here')], activeId: 'live' });
  await page.goto('/');
  await page.waitForTimeout(800);
  await tabNamed(page, 'Long job').hover();
  await page.keyboard.down('Alt');
  await expect(page.getByRole('button', { name: /^Close and stop Long job/ })).toBeVisible();
  await page.keyboard.up('Alt');
 });

 test('TA-CLOSE-03 closing a running tab shows the 6s toast with Stop it and Undo, and Undo puts it back in place', async ({ page }) => {
  await installMockEngine(page, { initial: { title: 'Long job', entries: [], running: true } });
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('live', 'Long job', { sessionFile: SESSION }), tab('c', 'Gamma')], activeId: 'live' });
  await page.goto('/');
  await page.waitForTimeout(800);
  await page.getByRole('button', { name: 'Close Long job', exact: true }).click();
  const toast = page.getByRole('status').filter({ hasText: /closed and still running/ });
  await expect(toast).toBeVisible();
  await expect(toast.getByRole('button', { name: 'Stop it' })).toBeVisible();
  await toast.getByRole('button', { name: 'Undo' }).click();
  expect(await stripOrder(page)).toEqual(['Alpha', 'Long job', 'Gamma']);
 });

 test('TA-CLOSE-05 closing the active tab selects its right neighbour; closing the last tab leaves one fresh tab', async ({ page }) => {
  await offline(page);
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma')], activeId: 'b' });
  await page.goto('/');
  const mod = await primary(page);
  await page.keyboard.press(`${mod}+w`);
  await expect(tabNamed(page, 'Gamma')).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press(`${mod}+w`);
  await expect(tabNamed(page, 'Alpha')).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press(`${mod}+w`);
  await expect(page.getByRole('tab')).toHaveCount(1);
  expect((await saved(page)).closed.map((t: { title: string }) => t.title)).toEqual(['Beta', 'Gamma', 'Alpha']);
 });

 test('TA-UNDO-01 ⌘⇧T reopens a closed tab where it was, in its group', async ({ page }) => {
  await offline(page);
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta', { groupId: 'g', draft: 'beta draft' }), tab('c', 'Gamma')], groups: [{ id: 'g', title: 'Solo', collapsed: false }], activeId: 'b' });
  await page.goto('/');
  // Whatever order the strip draws, the reopened tab must come back to the same place and the same group.
  const before = await stripItems(page);
  await page.keyboard.press(`${await primary(page)}+w`);
  await expect(tabNamed(page, 'Beta')).toHaveCount(0);
  await page.keyboard.press(`${await primary(page)}+Shift+t`);
  await expect(tabNamed(page, 'Beta')).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('beta draft');
  expect(await stripItems(page), 'the reopened tab came back somewhere else').toEqual(before);
 });

 test('TA-UNDO-02 ⌘Z outside a text field undoes the last close', async ({ page }) => {
  await offline(page);
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta')], activeId: 'b' });
  await page.goto('/');
  await tabNamed(page, 'Beta').focus();
  await page.keyboard.press(`${await primary(page)}+w`);
  await expect(tabNamed(page, 'Beta')).toHaveCount(0);
  await tabNamed(page, 'Alpha').focus();
  await page.keyboard.press(`${await primary(page)}+z`);
  await expect(tabNamed(page, 'Beta')).toBeVisible();
 });

 test('TA-UNDO-04 reopening a closed split from the new-tab field brings every pane back', async ({ page }) => {
  await offline(page);
  const panes = [tab('p1', 'Lexer notes', { draft: 'lexer draft' }), tab('p2', 'Parser notes', { draft: 'parser draft' })];
  await seed(page, { tabs: [tab('o', 'Other'), { id: 'sp', title: 'Split', draft: '', pinned: false, kind: 'conversation', split: { layout: '1x2', focus: 0, panes } }], activeId: 'sp' });
  await page.goto('/');
  // Close the split by key: its X can be covered by the segment titles (TA-SPLIT-13 is that case).
  await page.keyboard.press(`${await primary(page)}+w`);
  await expect(page.locator('.workspace-split-tab')).toHaveCount(0);
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  await page.getByRole('combobox', { name: 'Search or start' }).fill('Parser');
  await page.getByRole('option', { name: /Parser notes.*closed/ }).click();
  const state = await saved(page);
  const everywhere = JSON.stringify([state.tabs, state.closed]);
  expect(everywhere.includes('parser draft') && everywhere.includes('lexer draft'), 'a pane of the closed split is gone from the tabs and from the closed list').toBe(true);
 });
});

test.describe('selection, keys and the operating system', () => {
 test('TA-KEY-03 held Control Tab walks most-recently-used order, including after a close', async ({ page }) => {
  await offline(page);
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma')], activeId: 'a' });
  await page.goto('/');
  await tabNamed(page, 'Gamma').click();
  await tabNamed(page, 'Beta').click();
  await page.keyboard.down('Control');
  await page.keyboard.press('Tab');
  await expect(page.getByRole('option', { name: 'Gamma' })).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.up('Control');
  await expect(tabNamed(page, 'Gamma')).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press(`${await primary(page)}+w`);
  await page.keyboard.down('Control');
  await page.keyboard.press('Tab');
  await page.keyboard.up('Control');
  await expect(tabNamed(page, 'Alpha')).toHaveAttribute('aria-selected', 'true');
 });

 test('TA-KEY-06 text-editing chords in the composer are never taken by the shell', async ({ page }) => {
  await offline(page);
  await page.goto('/');
  const box = page.getByRole('textbox', { name: 'Message', exact: true });
  await box.fill('first words');
  const mod = await primary(page);
  await box.press(`${mod}+a`);
  await page.keyboard.type('replaced');
  await expect(box).toHaveValue('replaced');
  await box.press(`${mod}+z`);
  await expect(page.getByRole('tab')).toHaveCount(1);
 });

 test('TA-KEY-07 a Linux terminal keeps its own control keys (Ctrl+W, Ctrl+T, Ctrl+K reach the shell)', async ({ page }) => {
  test.skip(process.platform === 'darwin', 'On macOS the shell chords use Command and never collide with the terminal.');
  const engine = await installMockEngine(page, { ...plainReply(), terminals: [] });
  await page.goto('/');
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
  await page.keyboard.press('Control+Backquote');
  await expect(page.getByRole('tab', { name: 'zsh', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('.terminal-pane .xterm-rows')).toContainText('mock$');
  await page.locator('.terminal-pane .xterm-screen').click();
  await page.keyboard.type('echo one two');
  for (const chord of ['Control+w', 'Control+t', 'Control+k']) await page.keyboard.press(chord);
  const typed = () => engine.calls.filter(c => c.path.endsWith('/input')).map(c => Buffer.from(String((c.body as { dataBase64: string }).dataBase64), 'base64').toString()).join('');
  await expect.poll(typed).toBe('echo one two\x17\x14\x0b');
  await expect(page.getByRole('tab')).toHaveCount(2);
  await expect(page.getByRole('tab', { name: 'zsh', exact: true })).toHaveAttribute('aria-selected', 'true');
 });
});

test.describe('split', () => {
 test.beforeEach(({ page }) => offline(page));

 test('TA-SPLIT-09 Separate split gives every pane back as its own tab, in place, with its draft', async ({ page }) => {
  const panes = [tab('p1', 'Left pane', { draft: 'left words' }), tab('p2', 'Right pane', { draft: 'right words' })];
  await seed(page, { tabs: [tab('a', 'Alpha'), { id: 'sp', title: 'Split', draft: '', pinned: false, kind: 'conversation', split: { layout: '1x2', focus: 1, panes } }, tab('c', 'Gamma')], activeId: 'sp' });
  await page.goto('/');
  await page.locator('.workspace-split-tab').click({ button: 'right' });
  await chooseInMenu(page, 'Separate split');
  expect(await stripOrder(page)).toEqual(['Alpha', 'Left pane', 'Right pane', 'Gamma']);
  await expect(tabNamed(page, 'Right pane')).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('right words');
 });

 test('TA-SPLIT-10 closing a whole split and reopening it with ⌘⇧T restores all of its panes', async ({ page }) => {
  const panes = [tab('p1', 'Left pane', { draft: 'left words' }), tab('p2', 'Right pane'), tab('p3', 'Bottom pane')];
  await seed(page, { tabs: [tab('a', 'Alpha'), { id: 'sp', title: 'Split', draft: '', pinned: false, kind: 'conversation', split: { layout: '2x2', focus: 0, panes } }], activeId: 'sp' });
  await page.goto('/');
  await page.keyboard.press(`${await primary(page)}+w`);
  await expect(page.locator('.workspace-split-tab')).toHaveCount(0);
  await page.keyboard.press(`${await primary(page)}+Shift+t`);
  await expect(page.locator('.workspace-split-tab [role="tab"]')).toHaveCount(3);
  await expect(page.locator('.workspace-pane')).toHaveCount(3);
 });

 test('TA-SPLIT-11 a 2×2 split stays usable at the 800px native minimum: no page overflow, no sliver panes', async ({ page }) => {
  const panes = [tab('p1', 'One'), tab('p2', 'Two'), tab('p3', 'Three'), tab('p4', 'Four')];
  await seed(page, { tabs: [{ id: 'sp', title: 'Split', draft: '', pinned: false, kind: 'conversation', split: { layout: '2x2', focus: 0, panes } }], activeId: 'sp' });
  for (const width of [800]) {
   await page.setViewportSize({ width, height: 560 });
   await page.goto('/');
   await expect(page.locator('.workspace-pane')).toHaveCount(4);
   expect(await page.evaluate(() => document.scrollingElement!.scrollWidth <= innerWidth), `page overflows at ${width}px`).toBe(true);
   for (const name of ['One', 'Two', 'Three', 'Four']) {
    const pane = page.getByRole('region', { name, exact: true });
    const box = (await pane.boundingBox())!;
    expect(box.x + box.width, `${name} is clipped at ${width}px`).toBeLessThanOrEqual(width + 1);
    expect(box.width, `${name} is a sliver at ${width}px`).toBeGreaterThan(120);
   }
  }
 });
});

test.describe('overview', () => {
 test.beforeEach(({ page }) => offline(page));

 test('TA-OV-05 ⌘-click on an overview card opens that tab in the background and keeps the overview', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta')], activeId: 'a' });
  await page.goto('/');
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const overview = page.getByRole('dialog', { name: 'All tabs overview' });
  await expect(overview).toBeVisible();
  await overview.getByRole('button', { name: 'Open Beta', exact: true }).click({ modifiers: [await primary(page)] });
  await expect(overview, 'a background open closed the overview').toBeVisible();
  await overview.getByRole('button', { name: 'Done' }).click();
  await expect(tabNamed(page, 'Alpha')).toHaveAttribute('aria-selected', 'true');
 });

 test('TA-OV-06 right-click on an overview card gives the tab menu', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta')], activeId: 'a' });
  await page.goto('/');
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const overview = page.getByRole('dialog', { name: 'All tabs overview' });
  await overview.locator('.overview-card[data-card-id="b"]').click({ button: 'right' });
  const names = await menuNames(page);
  const missing = ['Open in split', 'Add to group', 'Pin tab', 'Duplicate', 'Close tab', 'Close other tabs'].filter(want => !names.some(name => name.startsWith(want)));
  expect(missing, `card menu has: ${names.join(' | ')}`).toEqual([]);
 });
});

test.describe('new tab field', () => {
 test.beforeEach(({ page }) => offline(page));

 test('TA-KEY-12 the Open file… hint names a key, and that key works in the field', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  const field = page.getByRole('combobox', { name: 'Search or start' });
  await expect(field).toBeFocused();
  await field.press(`${await primary(page)}+o`);
  await expect(page.locator('.newtab-caption')).toHaveText('Type part of a file name.');
 });

 test('TA-NEW-06 a pasted URL offers a web tab first, not only a question', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  await page.getByRole('combobox', { name: 'Search or start' }).fill('https://pkg.go.dev/encoding/json');
  await expect(page.getByRole('option').first()).toContainText(/web tab|Open pkg\.go\.dev/);
 });
});

test.describe('persistence and windows', () => {
 test('TA-WIN-01 two windows never drop each other\'s tabs', async ({ context }) => {
  const first = await context.newPage();
  await offline(first);
  await first.goto('/');
  await expect(first.getByRole('tab')).toHaveCount(1);
  // The Places "Open in new window" path in a browser: the same app on ?place=.
  const second = await context.newPage();
  await offline(second);
  await second.goto('/?place=now');
  await expect(second.getByRole('tab')).toHaveCount(1);
  for (const [page, words] of [[first, 'Alpha window work'], [second, 'Beta window work']] as const) {
   await page.getByRole('button', { name: 'New tab', exact: true }).click();
   const field = page.getByRole('combobox', { name: 'Search or start' });
   await field.fill(words);
   await field.press('Enter');
   await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue(words);
  }
  await first.reload();
  await expect(first.getByRole('tab', { name: 'Alpha window work', exact: true }), 'the first window lost the tab it made').toHaveCount(1);
  await second.reload();
  await expect(second.getByRole('tab', { name: 'Beta window work', exact: true }), 'the second window lost the tab it made').toHaveCount(1);
 });

 test('TA-PERS-03 a conversation keeps its scroll position when you leave its tab and come back', async ({ page }) => {
  const turns = Array.from({ length: 30 }, (_, i) => [{ Role: 'user', Text: `Question ${i + 1}` }, { Role: 'assistant', Answer: true, Text: `Answer ${i + 1}. ${'A longer paragraph that takes room. '.repeat(6)}` }]).flat();
  await installMockEngine(page, { initial: { title: 'Long talk', entries: turns as NonNullable<Scenario['initial']['entries']> } });
  await seed(page, { tabs: [tab('long', 'Long talk', { sessionFile: SESSION }), tab('other', 'Other')], activeId: 'long' });
  await page.setViewportSize({ width: 1200, height: 560 });
  await page.goto('/');
  // Long histories fold: open the earlier turns so the conversation is long enough to scroll.
  await page.getByRole('button', { name: /earlier turns$/ }).click();
  await expect(page.getByText('Question 1', { exact: true })).toBeVisible();
  const target = await page.evaluate(() => {
   const scrollers = Array.from(document.querySelectorAll<HTMLElement>('.workspace-pane-body *')).filter(el => el.scrollHeight > el.clientHeight + 100 && getComputedStyle(el).overflowY !== 'visible');
   const el = scrollers.sort((a, b) => b.scrollHeight - a.scrollHeight)[0];
   el.dataset.auditScroller = '';
   el.scrollTop = Math.round(el.scrollHeight / 3);
   return el.scrollTop;
  });
  await page.waitForTimeout(300);
  await tabNamed(page, 'Other').click();
  await tabNamed(page, 'Long talk').click();
  await page.waitForTimeout(500);
  const now = await page.evaluate(() => {
   const scrollers = Array.from(document.querySelectorAll<HTMLElement>('.workspace-pane-body *')).filter(el => el.scrollHeight > el.clientHeight + 100 && getComputedStyle(el).overflowY !== 'visible');
   return scrollers.sort((a, b) => b.scrollHeight - a.scrollHeight)[0]?.scrollTop ?? -1;
  });
  expect(Math.abs(now - target), `scrollTop was ${target}, came back as ${now}`).toBeLessThan(40);
 });
});

test.describe('appearance and access', () => {
 test.beforeEach(({ page }) => offline(page));

 for (const scheme of ['light', 'dark'] as const) {
  for (const width of [320, 600, 800]) {
   test(`TA-A11Y-${scheme}-${width} a grouped, pinned, collapsed strip is accessible and reachable at ${width}px in ${scheme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
    await page.setViewportSize({ width, height: 600 });
    await seed(page, { tabs: [tab('p', 'Pinned', { pinned: true }), tab('a', 'Alpha', { groupId: 'g' }), tab('b', 'Beta', { groupId: 'g' }), tab('c', 'Gamma', { groupId: 'h' }), tab('d', 'Delta'), tab('e', 'Epsilon')], groups: [{ id: 'g', title: 'Bench', collapsed: false }, { id: 'h', title: 'Folded', collapsed: true }], activeId: 'a' });
    await page.goto('/');
    await expect(tabNamed(page, 'Alpha')).toHaveAttribute('aria-selected', 'true');
    expect(await page.evaluate(() => document.scrollingElement!.scrollWidth <= innerWidth), 'page overflows').toBe(true);
    for (const control of [page.getByRole('button', { name: 'New tab', exact: true }), page.getByRole('button', { name: 'All tabs', exact: true })]) {
     const box = (await control.boundingBox())!;
     expect(box.x >= 0 && box.x + box.width <= width + 1, 'a strip action is off screen').toBe(true);
    }
    // Reduced motion: nothing in the strip animates.
    const moving = await page.evaluate(() => document.getAnimations().filter(a => (a.effect as KeyframeEffect | null)?.target instanceof Element && ((a.effect as KeyframeEffect).target as Element).closest('.workspace-tabbar')).length);
    expect(moving).toBe(0);
    await expectAccessible(page);
   });
  }
 }
});
