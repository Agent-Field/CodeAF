import { test, expect, type Locator, type Page } from '@playwright/test';
import { expectAccessible, tokenColor } from './contracts';
import { installMockEngine, type Scenario } from './support/mock-engine';
import { withTasks } from './support/scenarios';
import { openApp, send } from './support/conversation';

// Tab integrity (tabs audit findings 3, 4 and 5, and the group controls the design defines). Every journey drives the
// real app with real pointer and keyboard input; a drag is a real mouse drag. Each was red before the fix it names.

const KEY = 'codeaf.desktop.workspace.v1';
type Seed = { tabs: unknown[]; groups?: unknown[]; closed?: unknown[]; activeId: string };
const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', pinned: false, kind: 'conversation', titleSource: 'manual', ...over });

/** Seeds the saved workspace once per test, so a reload reads what the app saved rather than the seed. */
async function seed(page: Page, value: Seed) {
 const state = { groups: [], closed: [], nextNumber: value.tabs.length + 1, recentIds: value.tabs.map(t => (t as { id: string }).id), ...value };
 await page.addInitScript(([key, json]) => { if (!sessionStorage.getItem('integrity-seeded')) { localStorage.setItem(key, json); sessionStorage.setItem('integrity-seeded', '1'); } }, [KEY, JSON.stringify(state)] as const);
}
const saved = (page: Page) => page.evaluate(key => JSON.parse(localStorage.getItem(key) ?? 'null'), KEY);
const tabNamed = (page: Page, name: string) => page.getByRole('tab', { name, exact: true });
const groupLabel = (page: Page, name: string) => page.locator('.workspace-group-label', { has: page.locator('.workspace-group-name', { hasText: new RegExp(`^${name}$`) }) });
const message = (page: Page) => page.getByRole('textbox', { name: 'Message', exact: true });
const primary = async (page: Page) => (await page.evaluate(() => /Mac/.test(navigator.platform)) ? 'Meta' : 'Control');
/** The strip's top-level items in order: a tab's title, or "[label: members]" for a group capsule. */
const strip = (page: Page) => page.locator('.workspace-tabstrip').evaluate(root => Array.from(root.children).flatMap(el => {
 if (el.classList.contains('workspace-tab-group')) return [`[${el.querySelector('.workspace-group-name')?.textContent}: ${Array.from(el.querySelectorAll('.workspace-group-tab-slot:not([data-hidden="true"]) [role="tab"]')).map(t => t.getAttribute('aria-label')).join(' ')}]`];
 return el.matches('.workspace-tab') ? [el.querySelector('[role="tab"]')?.getAttribute('aria-label') ?? ''] : [];
}));
/** The order the arrow keys walk, read by walking them from the first tab. */
async function keyOrder(page: Page, count: number) {
 const names: string[] = [];
 await page.getByRole('tab').first().click();
 await page.keyboard.press('Home');
 for (let i = 0; i < count; i++) {
  names.push((await page.locator('[role="tab"][aria-selected="true"]').getAttribute('aria-label'))!);
  await page.locator('[role="tab"][aria-selected="true"]').press('ArrowRight');
 }
 return names;
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

test.describe('one strip order (finding 5)', () => {
 test.beforeEach(({ page }) => page.route('**/api/engine/**', route => route.abort()));

 test('a group made from the first tab keeps its place before loose tabs, a new tab lands last, and keys walk what the strip draws', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma')], activeId: 'a' });
  await page.goto('/');
  await tabNamed(page, 'Alpha').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Move to group', exact: true }).hover();
  await page.getByRole('menuitem', { name: 'Create group', exact: true }).click();
  await expect.poll(() => strip(page)).toEqual(['[New group: Alpha]', 'Beta', 'Gamma']);
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  await expect(tabNamed(page, 'New tab')).toHaveAttribute('aria-selected', 'true');
  await expect.poll(() => strip(page)).toEqual(['[New group: Alpha]', 'Beta', 'Gamma', 'New tab']);
  expect(await keyOrder(page, 4)).toEqual(['Alpha', 'Beta', 'Gamma', 'New tab']);
  await page.reload();
  await expect.poll(() => strip(page)).toEqual(['[New group: Alpha]', 'Beta', 'Gamma', 'New tab']);
 });

 test('a tab dropped just after a group\'s last member sits beside the group; dropped on a member\'s middle it joins', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha', { groupId: 'g' }), tab('b', 'Beta', { groupId: 'g' }), tab('c', 'Gamma'), tab('d', 'Delta')], groups: [{ id: 'g', title: 'Bench', collapsed: false }], activeId: 'c' });
  await page.goto('/');
  await drag(page, tabNamed(page, 'Delta'), tabNamed(page, 'Beta'), 0.92);
  await expect.poll(() => strip(page)).toEqual(['[Bench: Alpha Beta]', 'Delta', 'Gamma']);
  expect((await saved(page)).tabs.find((t: { id: string }) => t.id === 'd').groupId).toBeUndefined();
  await drag(page, tabNamed(page, 'Gamma'), tabNamed(page, 'Alpha'), 0.5);
  await expect.poll(() => strip(page)).toEqual(['[Bench: Alpha Beta Gamma]', 'Delta']);
 });

 test('dragging a group label moves the whole group, and a pinned tab stays pinned whatever is dropped on it', async ({ page }) => {
  await seed(page, { tabs: [tab('p', 'Pinned', { pinned: true }), tab('l', 'Loose'), tab('a', 'Alpha', { groupId: 'g' }), tab('b', 'Beta', { groupId: 'g' })], groups: [{ id: 'g', title: 'Bench', collapsed: false }], activeId: 'l' });
  await page.goto('/');
  await drag(page, groupLabel(page, 'Bench'), tabNamed(page, 'Loose'), 0.1);
  await expect.poll(() => strip(page)).toEqual(['Pinned', '[Bench: Alpha Beta]', 'Loose']);
  await drag(page, tabNamed(page, 'Loose'), tabNamed(page, 'Pinned'), 0.2);
  await expect.poll(async () => (await saved(page)).tabs.map((t: { id: string; pinned: boolean }) => `${t.id}${t.pinned ? '*' : ''}`)).toEqual(['p*', 'l', 'a', 'b']);
  await expect.poll(() => strip(page)).toEqual(['Pinned', 'Loose', '[Bench: Alpha Beta]']);
 });

 test('⌘-click picks tabs (Ctrl-click on Linux) and ⌘G groups them with the active tab', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma'), tab('d', 'Delta')], activeId: 'a' });
  await page.goto('/');
  const mod = await primary(page);
  await tabNamed(page, 'Gamma').click({ modifiers: [mod] });
  await tabNamed(page, 'Delta').click({ modifiers: [mod] });
  await expect(tabNamed(page, 'Alpha')).toHaveAttribute('aria-selected', 'true');
  await expect(tabNamed(page, 'Gamma')).toHaveAttribute('aria-description', 'Selected');
  await expect(page.locator('.workspace-tab[data-picked]')).toHaveCount(2);
  await expect(page.locator('.workspace-tab[data-picked]').first()).toHaveCSS('background-color', await tokenColor(page, 'field'));
  await expectAccessible(page);
  await page.keyboard.press(`${mod}+g`);
  await expect(page.locator('.workspace-tab-group')).toHaveCount(1);
  await expect.poll(() => strip(page)).toEqual(['[New group: Alpha Gamma Delta]', 'Beta']);
  await expect(page.locator('.workspace-tab[data-picked]')).toHaveCount(0);
  // A plain click drops the picks.
  await tabNamed(page, 'Beta').click({ modifiers: [mod] });
  await tabNamed(page, 'Alpha').click();
  await expect(page.locator('.workspace-tab[data-picked]')).toHaveCount(0);
 });
});

test.describe('Reopen puts a tab back where it stood (findings 3 and 4)', () => {
 test.beforeEach(({ page }) => page.route('**/api/engine/**', route => route.abort()));

 test('⌘⇧T reopens a tab between the same neighbours and in its group, even when closing removed the group', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta', { groupId: 'g', draft: 'beta draft' }), tab('c', 'Gamma')], groups: [{ id: 'g', title: 'Solo', collapsed: false }], activeId: 'b' });
  await page.goto('/');
  await expect.poll(() => strip(page)).toEqual(['Alpha', '[Solo: Beta]', 'Gamma']);
  const mod = await primary(page);
  await page.keyboard.press(`${mod}+w`);
  await expect(tabNamed(page, 'Beta')).toHaveCount(0);
  await expect(page.locator('.workspace-tab-group')).toHaveCount(0);
  // The record of where it stood survives a reload.
  await page.reload();
  await expect(tabNamed(page, 'Alpha')).toBeVisible();
  await page.keyboard.press(`${mod}+Shift+t`);
  await expect(tabNamed(page, 'Beta')).toHaveAttribute('aria-selected', 'true');
  await expect(message(page)).toHaveValue('beta draft');
  await expect.poll(() => strip(page)).toEqual(['Alpha', '[Solo: Beta]', 'Gamma']);
 });

 test('reopening a closed split from the new-tab field brings every pane back with its draft, where the split stood', async ({ page }) => {
  const panes = [tab('p1', 'Lexer notes', { draft: 'lexer draft' }), tab('p2', 'Parser notes', { draft: 'parser draft' })];
  await seed(page, { tabs: [tab('o', 'Other'), { id: 'sp', title: 'Split', draft: '', pinned: false, kind: 'conversation', split: { layout: '1x2', focus: 1, panes } }, tab('z', 'Last')], activeId: 'sp' });
  await page.goto('/');
  await expect(tabNamed(page, 'Parser notes')).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press(`${await primary(page)}+w`);
  await expect(page.locator('.workspace-split-tab')).toHaveCount(0);
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  await page.getByRole('combobox', { name: 'Search or start' }).fill('Parser');
  await page.getByRole('option', { name: /Parser notes.*closed/ }).click();
  await expect(page.locator('.workspace-split-tab [role="tab"]')).toHaveCount(2);
  await expect(page.locator('.workspace-pane')).toHaveCount(2);
  // The focused pane is the one that was focused, and both panes kept their words.
  await expect(tabNamed(page, 'Parser notes')).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('.workspace-pane').nth(0).getByRole('textbox').first()).toHaveValue('lexer draft');
  await expect(page.locator('.workspace-pane').nth(1).getByRole('textbox').first()).toHaveValue('parser draft');
  await expect(tabNamed(page, 'New tab')).toHaveCount(0);
  const ids = (await saved(page)).tabs.map((t: { id: string }) => t.id);
  expect(ids).toEqual(['o', 'sp', 'z']);
  expect((await saved(page)).closed).toEqual([]);
 });
});

test('a task opened from a grouped conversation joins its group', async ({ page }) => {
 const base = withTasks();
 const aside = { Role: 'aside', Text: 'Migrate the settings screen done · ran 30s · Two parts finished.', TaskIDs: ['2'] };
 await installMockEngine(page, { ...base, initial: { title: base.initial.title, entries: [] }, turns: [{ entries: [aside, base.initial.entries!.at(-1)!] as NonNullable<Scenario['initial']['entries']>, patch: { tasks: base.initial.tasks } }] });
 await openApp(page);
 await send(page, 'Migrate the settings screen');
 const panel = page.getByRole('complementary', { name: 'Tasks' });
 await expect(panel).toBeVisible();
 const title = (await page.getByRole('tab').first().getAttribute('aria-label'))!;
 await tabNamed(page, title).click({ button: 'right' });
 await page.getByRole('menuitem', { name: 'Move to group', exact: true }).hover();
 await page.getByRole('menuitem', { name: 'Create group', exact: true }).click();
 await expect(page.locator('.workspace-tab-group')).toHaveCount(1);
 await panel.getByRole('button', { name: /^(?!Collapse|Expand).*Port the form fields/ }).click({ modifiers: [await primary(page)] });
 await expect(page.getByRole('tab')).toHaveCount(2);
 await expect(page.locator('.workspace-tab-group [role="tab"]')).toHaveCount(2);
 const groups = (await saved(page)).tabs.map((t: { groupId?: string }) => t.groupId);
 expect(groups[1]).toBe(groups[0]);
});
