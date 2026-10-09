import { savedWorkspace } from './support/synced-workspace';
import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, tokenColor } from './contracts';
import { installMockEngine } from './support/mock-engine';
import { pendingQuestion } from './support/scenarios';
import type { AttentionItem, WorldRow } from '../../src/features/chat/world-client';

// The menus lane on top of the current shell: the shared toast with its structural Undo, a close-and-stop that fails, and the
// Inbox fed by the engine-wide world feed (stale rows, failures marked seen). The mock engine holds one session; a tab with
// that sessionFile is running, a tab without one is idle. Nothing here calls a model.
const SESSION = 'mock-session-1.jsonl';
const running = { initial: { running: true, title: '', entries: [{ Role: 'user', Text: 'Trailing commas across the config stack' }] } };

async function seed(page: Page, tabs: { id: string; title: string; running?: boolean }[], active: string) {
  const make = (t: (typeof tabs)[number]) => ({ id: t.id, title: t.title, draft: '', titleSource: 'manual', kind: 'conversation', pinned: false, ...(t.running ? { sessionFile: SESSION } : {}) });
  const state = { tabs: tabs.map(make), groups: [], closed: [], activeId: active, nextNumber: tabs.length + 1, recentIds: tabs.map(t => t.id) };
  await page.addInitScript(value => { if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
}
const row = (session: string, over: Partial<WorldRow> = {}): WorldRow => ({ session, title: `Chat ${session}`, project: 'p', sourceFolders: [], state: 'idle', live: false, open: false, running: false, needsYou: false, failed: 0, tasks: { running: 0, incomplete: 0, done: 0, failed: 0, total: 0 }, ...over });
const ask = (session: string, text: string): AttentionItem => ({ key: `${session}:question:1`, session, kind: 'question', text, sourceFolders: [], answerable: false });
const recent = () => new Date(Date.now() - 60_000).toISOString();
const noOverflow = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth);

test.describe('closing that fails to stop (3l)', () => {
  test('Close and stop that cannot stop leaves the work visible in the Inbox, with Try again and a structural Undo', async ({ page }) => {
    const engine = await installMockEngine(page, { ...running, fail: { stop: 500 } });
    await seed(page, [{ id: 'a', title: 'Intro' }, { id: 'b', title: 'Config stack', running: true }], 'b');
    await page.goto('/');
    await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
    await page.getByRole('tab', { name: 'Config stack', exact: true }).click({ button: 'right' });
    await page.getByRole('menuitem', { name: /^Close and stop/ }).click();
    const toast = page.locator('.toast');
    await expect(toast).toContainText('Could not stop Config stack. It is still running.');
    expect(await toast.getByRole('button').allTextContents()).toEqual(['Try again', 'Undo']);
    await expect(toast.locator('.toast-dot')).toHaveCSS('background-color', await tokenColor(page, 'danger'));
    // The tab is gone but the work is not: it is on the Inbox's running list.
    await page.getByRole('tab', { name: 'Inbox', exact: true }).click();
    await expect(page.getByRole('region', { name: 'Inbox' }).getByRole('button', { name: /Config stack/ })).toBeVisible();
    // Undo is reached by keyboard and reopens the tab where it was.
    await toast.getByRole('button', { name: 'Undo' }).focus();
    await page.keyboard.press('Enter');
    await expect(toast).toHaveCount(0);
    await expect(page.getByRole('tab', { name: 'Config stack', exact: true })).toHaveAttribute('aria-selected', 'true');
  });

  test('Try again asks the engine to stop again', async ({ page }) => {
    const engine = await installMockEngine(page, { ...running, fail: { stop: 500 } });
    await seed(page, [{ id: 'a', title: 'Intro' }, { id: 'b', title: 'Config stack', running: true }], 'b');
    await page.goto('/');
    await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
    await page.getByRole('tab', { name: 'Config stack', exact: true }).click({ button: 'right' });
    await page.getByRole('menuitem', { name: /^Close and stop/ }).click();
    const stops = () => engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/stop')).length;
    await expect.poll(stops).toBe(1);
    await page.locator('.toast').getByRole('button', { name: 'Try again' }).click();
    await expect.poll(stops).toBe(2);
    await expect(page.locator('.toast')).toContainText('Could not stop Config stack');
  });
});

test.describe('the Inbox from the engine world feed', () => {
  const world = (over: Partial<{ rows: WorldRow[]; items: AttentionItem[] }> = {}) => ({
    ...running, initial: { running: false, entries: [] },
    world: {
      rows: [row('w1', { title: 'Nightly build', running: true }), row('w2', { title: 'Release notes', needsYou: true }), row('w3', { title: 'Lexer rewrite', failed: 2, at: recent() })],
      items: [ask('w2', 'Which branch should the notes cover?')], ...over,
    },
  });

  test('work, questions and failures with no tab here come from the feed; rows with nowhere to go are text, not dead buttons', async ({ page }) => {
    await installMockEngine(page, world());
    await seed(page, [{ id: 'a', title: 'Intro' }], 'a');
    await page.goto('/');
    const inbox = page.getByRole('tab', { name: 'Inbox', exact: true });
    await expect(inbox).toBeVisible({ timeout: 8000 });
    await expect(page.locator('.tab-badge')).toHaveCount(1);
    await inbox.click();
    const card = page.getByRole('region', { name: 'Inbox' });
    await expect(card.getByRole('heading', { name: 'Running in the background' })).toBeVisible();
    await expect(card.getByRole('heading', { name: 'Needs you' })).toBeVisible();
    await expect(card.getByRole('heading', { name: 'Failed' })).toBeVisible();
    await expect(card).toContainText('Nightly build');
    await expect(card).toContainText('Release notes');
    await expect(card).toContainText('2 tasks');
    // The shell has not supplied an opener for chats with no tab, so none of these rows is a button; only "Seen" is.
    await expect(card.getByRole('button', { name: /^(Nightly build|Release notes|Lexer rewrite)/ })).toHaveCount(0);
    expect(await card.getByRole('button').allTextContents()).toEqual(['Seen']);
    await expectAccessible(page);
  });

  test('marking a failure seen removes it, keeps it gone after a reload, and a new failure brings it back', async ({ page }) => {
    const engine = await installMockEngine(page, world());
    await seed(page, [{ id: 'a', title: 'Intro' }], 'a');
    await page.goto('/');
    await page.getByRole('tab', { name: 'Inbox', exact: true }).click();
    const card = page.getByRole('region', { name: 'Inbox' });
    await card.getByRole('button', { name: 'Mark failure in Lexer rewrite as seen' }).click();
    await expect(card.getByRole('heading', { name: 'Failed' })).toHaveCount(0);
    await page.reload();
    await page.getByRole('tab', { name: 'Inbox', exact: true }).click();
    await expect(page.getByRole('region', { name: 'Inbox' })).toContainText('Nightly build');
    await expect(page.getByRole('region', { name: 'Inbox' }).getByRole('heading', { name: 'Failed' })).toHaveCount(0);
    engine.setWorld({ rows: [row('w1', { title: 'Nightly build', running: true }), row('w3', { title: 'Lexer rewrite', failed: 3, at: recent() })] });
    await expect(page.getByRole('region', { name: 'Inbox' }).getByRole('heading', { name: 'Failed' })).toBeVisible({ timeout: 8000 });
    await expect(page.getByRole('region', { name: 'Inbox' })).toContainText('3 tasks');
  });

  test('the list follows the stream: finished work and answered questions leave, and an empty Inbox says what arrives', async ({ page }) => {
    const engine = await installMockEngine(page, world());
    await seed(page, [{ id: 'a', title: 'Intro' }], 'a');
    await page.goto('/');
    await page.getByRole('tab', { name: 'Inbox', exact: true }).click();
    const card = page.getByRole('region', { name: 'Inbox' });
    await expect(card).toContainText('Nightly build');
    engine.setWorld({ rows: [row('w1', { title: 'Nightly build' }), row('w2', { title: 'Release notes' })], items: [] });
    await expect(card).not.toContainText('Nightly build', { timeout: 8000 });
    await expect(card).not.toContainText('Release notes');
    await expect(page.locator('.tab-badge')).toHaveCount(0);
    await expect(card).toContainText('lands here');
  });

  for (const scheme of ['light', 'dark'] as const) for (const width of [320, 600]) {
    test(`the Inbox with every section fits and passes accessibility: ${scheme} ${width}px`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      await page.setViewportSize({ width, height: 720 });
      await installMockEngine(page, world());
      await seed(page, [{ id: 'a', title: 'Intro' }], 'a');
      await page.goto('/');
      const inbox = page.getByRole('tab', { name: 'Inbox', exact: true });
      await expect(inbox).toBeVisible({ timeout: 8000 });
      await inbox.click();
      const card = page.getByRole('region', { name: 'Inbox' });
      await expect(card.getByRole('heading', { name: 'Failed' })).toBeVisible();
      // Keyboard: the only control is reachable and activates.
      const seen = card.getByRole('button', { name: /as seen$/ });
      await seen.focus();
      await expect(seen).toBeFocused();
      expect(await noOverflow(page)).toBe(true);
      await expectAccessible(page);
      await page.keyboard.press('Enter');
      await expect(card.getByRole('heading', { name: 'Failed' })).toHaveCount(0);
    });
  }
});

test.describe('the tab menu offers only what can work', () => {
  test('in a browser a tab never sent has no Copy link and no Move to new window, and nothing is disabled in their place', async ({ page }) => {
    await page.route('**/api/engine/**', route => route.abort());
    await seed(page, [{ id: 'a', title: 'Config stack' }, { id: 'b', title: 'lexer.go' }], 'a');
    await page.goto('/');
    await page.getByRole('tab', { name: 'lexer.go', exact: true }).click({ button: 'right' });
    const menu = page.getByRole('menu', { name: 'Actions for lexer.go', exact: true });
    await expect(menu).toBeVisible();
    await expect(menu.getByRole('menuitem', { name: /Copy link|Move to new window/ })).toHaveCount(0);
    await page.keyboard.press('Escape');
  });
});

// Bulk closes (the rest of the strip, the tabs to the right, a whole group) share ONE structural Undo that restores every
// pane of every split, the group's own title and collapsed state, and each tab's place. Raw workspace state is seeded so a
// split tab and a collapsed group are part of the picture.
type RawTab = Record<string, unknown>;
const conv = (id: string, title: string, over: RawTab = {}): RawTab => ({ id, kind: 'conversation', title, draft: `draft ${id}`, titleSource: 'manual', pinned: false, ...over });
async function seedRaw(page: Page, tabs: RawTab[], groups: { id: string; title: string; collapsed?: boolean }[], active: string) {
  const state = { tabs, groups: groups.map(g => ({ collapsed: false, ...g })), closed: [], activeId: active, nextNumber: tabs.length + 1, recentIds: tabs.map(t => t.id) };
  await page.addInitScript(value => { if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
}
const saved = savedWorkspace;
const order = async (page: Page) => (await saved(page)).tabs.map(t => t.id);
const split = (id: string, over: RawTab = {}): RawTab => ({ ...conv(id, 'Pair', over), split: { layout: '1x2', focus: 1, panes: [conv(`${id}1`, 'Left'), conv(`${id}2`, 'Right')] } });

test.describe('bulk closes have one structural Undo', () => {
  test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

  test('closing a group offers Undo, and Undo restores its title, collapsed state, split panes and place', async ({ page }) => {
    await seedRaw(page, [conv('p', 'Pinned', { pinned: true }), conv('a', 'Solo'), conv('b', 'Config stack', { groupId: 'g' }), split('s', { groupId: 'g' }), conv('c', 'Tail')], [{ id: 'g', title: 'Trailing commas', collapsed: false }], 'a');
    await page.goto('/');
    const before = await order(page);
    await page.locator('.workspace-group-label').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Close 2 tabs' }).click();
    const toast = page.locator('.toast').filter({ hasText: /^Closed / });
    await expect(toast).toContainText('Closed 2 tabs');
    expect(await toast.getByRole('button').allTextContents()).toEqual(['Undo']);
    await expect(page.locator('.workspace-group-label')).toHaveCount(0);
    expect(await order(page)).not.toEqual(before);
    await toast.getByRole('button', { name: 'Undo' }).click();
    await expect(toast).toHaveCount(0);
    const after = await saved(page);
    expect(after.tabs.map(t => t.id)).toEqual(before);
    expect(after.groups).toEqual([{ id: 'g', title: 'Trailing commas', collapsed: false }]);
    expect(after.tabs.find(t => t.id === 's')!.groupId).toBe('g');
    expect(after.tabs.find(t => t.id === 's')!.split!.panes.map(p => [p.id, p.draft])).toEqual([['s1', 'draft s1'], ['s2', 'draft s2']]);
    await expect(page.locator('.workspace-group-label')).toContainText('Trailing commas');
  });

  test('Close other tabs and Close tabs to the right each undo to the exact prior order, by keyboard', async ({ page }) => {
    await seedRaw(page, [conv('a', 'One'), conv('b', 'Two'), split('s'), conv('d', 'Four')], [], 'b');
    await page.goto('/');
    const before = await order(page);
    for (const [item, title] of [['Close other tabs', 'Two'], ['Close tabs to the right', 'One']] as const) {
      await page.getByRole('tab', { name: title, exact: true }).click({ button: 'right' });
      await page.getByRole('menuitem', { name: item, exact: true }).click();
      const toast = page.locator('.toast').filter({ hasText: /^Closed / });
      await expect(toast).toContainText(/Closed \d tabs?/);
      const undo = toast.getByRole('button', { name: 'Undo' });
      await undo.focus();
      await page.keyboard.press('Enter');
      await expect(toast).toHaveCount(0);
      expect(await order(page)).toEqual(before);
      expect((await saved(page)).tabs.find(t => t.id === 's')!.split!.panes).toHaveLength(2);
    }
  });

  for (const scheme of ['light', 'dark'] as const) test(`a bulk-close toast fits and passes accessibility at 320px ${scheme}`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      await page.setViewportSize({ width: 320, height: 640 });
      await seedRaw(page, [conv('a', 'Solo'), conv('b', 'Config stack', { groupId: 'g' }), conv('c', 'Fixtures', { groupId: 'g' })], [{ id: 'g', title: 'Trailing commas' }], 'a');
      await page.goto('/');
      await page.locator('.workspace-group-label').click({ button: 'right' });
      await page.getByRole('menuitem', { name: 'Close 2 tabs' }).click();
      await expect(page.locator('.toast').filter({ hasText: /^Closed / })).toBeVisible();
      expect(await noOverflow(page)).toBe(true);
      await expectAccessible(page);
  });
});

test.describe('failed tasks and the Inbox', () => {
  const failing = (over: Partial<WorldRow> = {}) => ({ ...running, initial: { running: false, entries: [] }, world: { rows: [row('w3', { title: 'Lexer rewrite', failed: 2, at: recent(), ...over })], items: [] as AttentionItem[] } });

  test('a failed task alone summons the Inbox with a red dot named for it, and Seen clears the dot', async ({ page }) => {
    await installMockEngine(page, failing());
    await seed(page, [{ id: 'a', title: 'Intro' }], 'a');
    await page.goto('/');
    const inbox = page.getByRole('tab', { name: 'Inbox', exact: true });
    await expect(inbox).toBeVisible({ timeout: 8000 });
    await expect(inbox).toHaveAccessibleDescription('A task failed');
    await expect(page.locator('.tab-badge')).toHaveAttribute('data-kind', 'failed');
    await expect(page.locator('.tab-badge')).toHaveCSS('background-color', await tokenColor(page, 'danger'));
    await inbox.click();
    await page.getByRole('region', { name: 'Inbox' }).getByRole('button', { name: 'Mark failure in Lexer rewrite as seen' }).click();
    await expect(page.locator('.tab-badge')).toHaveCount(0);
  });

  test('a question outranks a failure on the dot', async ({ page }) => {
    await installMockEngine(page, { ...failing(), world: { rows: [row('w3', { title: 'Lexer rewrite', failed: 1, at: recent() }), row('w2', { title: 'Release notes', needsYou: true })], items: [ask('w2', 'Which branch?')] } });
    await seed(page, [{ id: 'a', title: 'Intro' }], 'a');
    await page.goto('/');
    await expect(page.locator('.tab-badge')).toHaveAttribute('data-kind', 'needsYou', { timeout: 8000 });
    await expect(page.getByRole('tab', { name: 'Inbox', exact: true })).toHaveAccessibleDescription('Needs you');
  });
});

test.describe('open-inbox lands on the oldest question', () => {
  const asked = (session: string, text: string, ago: number): AttentionItem => ({ ...ask(session, text), asked: new Date(Date.now() - ago).toISOString() });
  const openFromShell = (page: Page) => page.evaluate(() => window.dispatchEvent(new CustomEvent('codeaf:shell-open', { detail: 'inbox' })));

  test('the shell request lists and orders the question that has waited longest first', async ({ page }) => {
    await installMockEngine(page, { ...running, initial: { running: false, entries: [] }, world: { rows: [row('n', { title: 'Newer', needsYou: true }), row('o', { title: 'Older', needsYou: true })], items: [asked('n', 'Newer?', 60_000), asked('o', 'Older?', 3_600_000)] } });
    await seed(page, [{ id: 'a', title: 'Intro' }], 'a');
    await page.goto('/');
    await expect(page.getByRole('tab', { name: 'Inbox', exact: true })).toBeVisible({ timeout: 8000 });
    await openFromShell(page);
    const card = page.getByRole('region', { name: 'Inbox' });
    await expect(card).toBeVisible();
    const rows = card.locator('[data-section="needsYou"] li');
    await expect(rows.first()).toContainText('Older');
    await expect(rows.nth(1)).toContainText('Newer');
  });

  test('with a tab-backed question the row is a button and receives focus', async ({ page }) => {
    const asking = pendingQuestion();
    await installMockEngine(page, { ...asking, initial: { ...asking.initial, running: true } });
    await seed(page, [{ id: 'a', title: 'Intro' }, { id: 'b', title: 'Release v2.4', running: true }], 'a');
    await page.goto('/');
    await expect(page.getByRole('tab', { name: 'Inbox', exact: true })).toBeVisible({ timeout: 8000 });
    await openFromShell(page);
    await expect(page.getByRole('region', { name: 'Inbox' }).getByRole('button', { name: /Release v2.4/ })).toBeFocused();
  });
});
