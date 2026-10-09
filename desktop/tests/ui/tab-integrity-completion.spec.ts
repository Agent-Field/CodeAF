import { test, expect, type Locator, type Page } from '@playwright/test';
import { expectAccessible } from './contracts';
import { installMockEngine } from './support/mock-engine';
import { savedWorkspace } from './support/synced-workspace';

// Tab integrity, completion: the split tab's × is hit where it is drawn (tabs audit 4b), the overview's drag regroups
// through the shared reducer and its × closes the way the strip's does, and ⌘Z takes back the window's last structural
// step (Interactions, Undo) without taking a text field's, a terminal's or a toast's own Undo. Real pointer and keys.

const KEY = 'codeaf.desktop.workspace.v1';
const SESSION = 'mock-session-1.jsonl';
type Seed = { tabs: unknown[]; groups?: unknown[]; closed?: unknown[]; activeId: string };
const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', pinned: false, kind: 'conversation', titleSource: 'manual', ...over });

async function seed(page: Page, value: Seed) {
 const state = { groups: [], closed: [], nextNumber: value.tabs.length + 1, recentIds: value.tabs.map(t => (t as { id: string }).id), ...value };
 await page.addInitScript(([key, json]) => { if (!sessionStorage.getItem('completion-seeded')) { localStorage.setItem(key, json); sessionStorage.setItem('completion-seeded', '1'); } }, [KEY, JSON.stringify(state)] as const);
}
const saved = savedWorkspace;
const tabNamed = (page: Page, name: string) => page.getByRole('tab', { name, exact: true });
const message = (page: Page) => page.getByRole('textbox', { name: 'Message', exact: true });
const primary = async (page: Page) => (await page.evaluate(() => /Mac/.test(navigator.platform)) ? 'Meta' : 'Control');
/** The strip as a person reads it: a tab's title, "*title" when pinned, or "[label: members]" for a group. */
const strip = (page: Page) => page.locator('.workspace-tabstrip').evaluate(root => Array.from(root.children).flatMap(el => {
 if (el.classList.contains('workspace-tab-group')) return [`[${el.querySelector('.workspace-group-name')?.textContent}: ${Array.from(el.querySelectorAll('.workspace-group-tab-slot:not([data-hidden="true"]) [role="tab"]')).map(t => t.getAttribute('aria-label')).join(' ')}]`];
 if (el.classList.contains('workspace-split-tab')) return [`{${Array.from(el.querySelectorAll('[role="tab"]')).map(t => t.getAttribute('aria-label')).join(' ')}}`];
 return el.matches('.workspace-tab') ? [`${el.classList.contains('is-pinned') ? '*' : ''}${el.querySelector('[role="tab"]')?.getAttribute('aria-label') ?? ''}`] : [];
}));
/** What the page draws at the centre of a box: the element a real pointer at that point would hit. */
const hitAt = (page: Page, box: { x: number; y: number; width: number; height: number }) =>
 page.evaluate(([x, y]) => { const el = document.elementFromPoint(x, y); return el ? (el.closest('.workspace-tab-close') ? 'close' : el.closest('.workspace-split-segment')?.getAttribute('aria-label') ?? el.tagName) : 'nothing'; }, [box.x + box.width / 2, box.y + box.height / 2] as const);
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
const split = (focus = 1) => ({ id: 'sp', title: 'Split', draft: '', pinned: false, kind: 'conversation', split: { layout: '1x2', focus, panes: [tab('p1', 'Lexer notes', { draft: 'lexer words' }), tab('p2', 'A very long parser notes title that fades', { draft: 'parser words' })] } });

test.describe('the split tab\'s × (tabs audit 4b)', () => {
 test.beforeEach(({ page }) => page.route('**/api/engine/**', route => route.abort()));

 test('the × is what a pointer hits where it is drawn, every segment hits itself, and a real click closes the whole split', async ({ page }) => {
  await seed(page, { tabs: [tab('o', 'Other'), split(), tab('z', 'Last')], activeId: 'sp' });
  await page.goto('/');
  const splitTab = page.locator('.workspace-split-tab');
  const close = splitTab.locator('.workspace-tab-close');
  await splitTab.hover();
  await expect(close).toBeVisible();
  expect(await hitAt(page, (await close.boundingBox())!)).toBe('close');
  for (const name of ['Lexer notes', 'A very long parser notes title that fades']) expect(await hitAt(page, (await tabNamed(page, name).boundingBox())!)).toBe(name);
  await splitTab.screenshot({ path: test.info().outputPath('split-close-hit.png') });
  // The segment and the close slot never overlap.
  const segment = (await tabNamed(page, 'A very long parser notes title that fades').boundingBox())!;
  const slot = (await splitTab.locator('.workspace-tab-close-slot').boundingBox())!;
  expect(segment.x + segment.width).toBeLessThanOrEqual(slot.x + 0.5);
  const box = (await close.boundingBox())!;
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await expect(splitTab).toHaveCount(0);
  await expect.poll(async () => (await saved(page)).closed.map((t: { id: string }) => t.id)).toEqual(['sp']);
 });

 test('an inactive split\'s × closes it too, and the segment still clips its faded title within itself', async ({ page }) => {
  await seed(page, { tabs: [tab('o', 'Other'), split(0), tab('z', 'Last')], activeId: 'o' });
  await page.goto('/');
  const splitTab = page.locator('.workspace-split-tab');
  await splitTab.hover();
  const close = splitTab.locator('.workspace-tab-close');
  await expect(close).toBeVisible();
  expect(await hitAt(page, (await close.boundingBox())!)).toBe('close');
  await expect(tabNamed(page, 'A very long parser notes title that fades')).toHaveCSS('overflow', 'hidden');
  const box = (await close.boundingBox())!;
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await expect(splitTab).toHaveCount(0);
  await expect(tabNamed(page, 'Other')).toHaveAttribute('aria-selected', 'true');
 });
});

test.describe('overview drag regroups (Interactions, overview card)', () => {
 test.beforeEach(({ page }) => page.route('**/api/engine/**', route => route.abort()));

 async function openOverview(page: Page) {
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const overview = page.getByRole('dialog', { name: 'All tabs overview' });
  await expect(overview).toBeVisible();
  return overview;
 }
 const card = (overview: Locator, id: string) => overview.locator(`.overview-card[data-card-id="${id}"]`);

 test('a card dropped on the right half of a group\'s card joins that group just after it; on "Other tabs" it leaves', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha', { groupId: 'g' }), tab('b', 'Beta', { groupId: 'g' }), tab('d', 'Delta'), tab('e', 'Echo')], groups: [{ id: 'g', title: 'Bench', collapsed: false }], activeId: 'd' });
  await page.goto('/');
  const overview = await openOverview(page);
  await drag(page, card(overview, 'd'), card(overview, 'a'), 0.8);
  await expect.poll(async () => (await saved(page)).tabs.map((t: { id: string; groupId?: string }) => `${t.id}${t.groupId ? '@' + t.groupId : ''}`)).toEqual(['a@g', 'd@g', 'b@g', 'e']);
  await expect(overview.locator('.overview-section[aria-label="Bench"] .overview-card')).toHaveCount(3);
  await drag(page, card(overview, 'b'), card(overview, 'e'), 0.2);
  await expect.poll(async () => (await saved(page)).tabs.map((t: { id: string; groupId?: string }) => `${t.id}${t.groupId ? '@' + t.groupId : ''}`)).toEqual(['a@g', 'd@g', 'b', 'e']);
  await expect(overview.locator('.overview-card[data-drop]')).toHaveCount(0);
  await expect(overview.locator('.overview-section[data-drop]')).toHaveCount(0);
  await expectAccessible(page);
  // The strip shows the same order the overview made (one reducer, one order).
  await overview.getByRole('button', { name: 'Done' }).click();
  await expect.poll(() => strip(page)).toEqual(['[Bench: Alpha Delta]', 'Beta', 'Echo']);
  // The regroup is one structural step: ⌘Z takes the last one back.
  await tabNamed(page, 'Echo').click();
  await page.keyboard.press(`${await primary(page)}+z`);
  await expect.poll(() => strip(page)).toEqual(['[Bench: Alpha Delta Beta]', 'Echo']);
 });

 test('the overview\'s × closes a running tab the way the strip does: work keeps running, the toast offers Stop it and Undo', async ({ page }) => {
  const engine = await installMockEngine(page, { initial: { running: true, title: '', entries: [{ Role: 'user', Text: 'Trailing commas' }] } });
  await seed(page, { tabs: [tab('a', 'Intro'), tab('b', 'Config stack', { sessionFile: SESSION }), tab('c', 'lexer.go')], activeId: 'a' });
  await page.goto('/');
  await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
  const overview = await openOverview(page);
  await card(overview, 'b').hover();
  await card(overview, 'b').getByRole('button', { name: 'Close Config stack' }).click();
  const toast = page.locator('.toast');
  await expect(toast).toContainText('Config stack closed and still running');
  expect(await toast.getByRole('button').allTextContents()).toEqual(['Stop it', 'Undo']);
  expect(engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/stop'))).toEqual([]);
 });
});

test.describe('⌘Z, the window\'s structural Undo (Interactions, Undo)', () => {
 test.beforeEach(({ page }) => page.route('**/api/engine/**', route => route.abort()));

 test('⌘Z takes back ⌘G, a pin, a drag and a close, newest first, each in place', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma'), tab('d', 'Delta')], activeId: 'a' });
  await page.goto('/');
  const mod = await primary(page);
  const start = ['Alpha', 'Beta', 'Gamma', 'Delta'];
  await expect.poll(() => strip(page)).toEqual(start);
  await tabNamed(page, 'Gamma').click({ modifiers: [mod] });
  await page.keyboard.press(`${mod}+g`);
  await expect.poll(() => strip(page)).toEqual(['[New group: Alpha Gamma]', 'Beta', 'Delta']);
  await tabNamed(page, 'Beta').click({ button: 'right' });
  await page.getByRole('menuitem', { name: /^Pin tab/ }).click();
  await expect.poll(() => strip(page)).toEqual(['*Beta', '[New group: Alpha Gamma]', 'Delta']);
  await drag(page, tabNamed(page, 'Delta'), tabNamed(page, 'Alpha'), 0.1);
  await expect.poll(() => strip(page)).toEqual(['*Beta', 'Delta', '[New group: Alpha Gamma]']);
  await tabNamed(page, 'Delta').click();
  await page.keyboard.press(`${mod}+w`);
  await expect(tabNamed(page, 'Delta')).toHaveCount(0);

  await tabNamed(page, 'Alpha').click();
  await page.keyboard.press(`${mod}+z`);
  await expect.poll(() => strip(page)).toEqual(['*Beta', 'Delta', '[New group: Alpha Gamma]']);
  await page.keyboard.press(`${mod}+z`);
  await expect.poll(() => strip(page)).toEqual(['*Beta', '[New group: Alpha Gamma]', 'Delta']);
  await page.keyboard.press(`${mod}+z`);
  await expect.poll(() => strip(page)).toEqual(['[New group: Alpha Gamma]', 'Beta', 'Delta']);
  await page.keyboard.press(`${mod}+z`);
  await expect.poll(() => strip(page)).toEqual(start);
  // Nothing left: ⌘Z does nothing and says nothing.
  await page.keyboard.press(`${mod}+z`);
  await expect.poll(() => strip(page)).toEqual(start);
  await expect(page.locator('.toast')).toHaveCount(0);
 });

 test('⌘Z takes back opening a group as a split, with the words in every pane', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha', { groupId: 'g', draft: 'alpha words' }), tab('b', 'Beta', { groupId: 'g', draft: 'beta words' }), tab('c', 'Gamma')], groups: [{ id: 'g', title: 'Bench', collapsed: false }], activeId: 'c' });
  await page.goto('/');
  await page.locator('.workspace-group-label').click({ button: 'right' });
  await page.getByRole('menuitem', { name: /^Open as split/ }).click();
  await expect(page.locator('.workspace-split-tab')).toHaveCount(1);
  await tabNamed(page, 'Gamma').click();
  await page.keyboard.press(`${await primary(page)}+z`);
  await expect(page.locator('.workspace-split-tab')).toHaveCount(0);
  await expect.poll(() => strip(page)).toEqual(['[Bench: Alpha Beta]', 'Gamma']);
  await tabNamed(page, 'Beta').click();
  await expect(message(page)).toHaveValue('beta words');
 });

 test('in a text field ⌘Z is the field\'s own Undo; the strip is left alone until focus leaves the field', async ({ page }) => {
  await seed(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta')], activeId: 'a' });
  await page.goto('/');
  const mod = await primary(page);
  await tabNamed(page, 'Beta').click({ button: 'right' });
  await page.getByRole('menuitem', { name: /^Pin tab/ }).click();
  await expect.poll(() => strip(page)).toEqual(['*Beta', 'Alpha']);
  await tabNamed(page, 'Alpha').click();
  await message(page).click();
  await page.keyboard.type('hello');
  await page.keyboard.press(`${mod}+z`);
  await expect(message(page)).not.toHaveValue('hello');
  await expect.poll(() => strip(page)).toEqual(['*Beta', 'Alpha']);
  await tabNamed(page, 'Alpha').click();
  await page.keyboard.press(`${mod}+z`);
  await expect.poll(() => strip(page)).toEqual(['Alpha', 'Beta']);
 });

 test('⌘Z with the closing toast on screen is that toast\'s Undo, never a second reopen', async ({ page }) => {
  await page.unrouteAll();
  const engine = await installMockEngine(page, { initial: { running: true, title: '', entries: [{ Role: 'user', Text: 'Trailing commas' }] } });
  await seed(page, { tabs: [tab('a', 'Intro'), tab('b', 'Config stack', { sessionFile: SESSION }), tab('c', 'lexer.go')], activeId: 'b' });
  await page.goto('/');
  await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
  const mod = await primary(page);
  await tabNamed(page, 'Config stack').click();
  await page.keyboard.press(`${mod}+w`);
  const toast = page.locator('.toast');
  await expect(toast).toContainText('Config stack closed and still running');
  await page.keyboard.press(`${mod}+z`);
  await expect(toast).toHaveCount(0);
  await expect(tabNamed(page, 'Config stack')).toHaveAttribute('aria-selected', 'true');
  const titles = () => page.getByRole('tab').evaluateAll(els => els.map(el => el.getAttribute('aria-label')));
  expect((await titles()).filter(t => t === 'Config stack')).toHaveLength(1);
  // The close step was spent by the toast: another ⌘Z has nothing of it to take back.
  await page.keyboard.press(`${mod}+z`);
  expect((await titles()).filter(t => t === 'Config stack')).toHaveLength(1);
  expect(engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/stop'))).toEqual([]);
 });
});
