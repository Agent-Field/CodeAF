import { test, expect, type Locator, type Page } from '@playwright/test';
import type { EngineQuestion } from '../../src/features/chat/engine-client';
import { expectAccessible } from './contracts';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { savedWorkspace } from './support/synced-workspace';

// SH-138, SH-183, SH-184. The overview card's right-click is the tab menu, in the Shell 3g order.
// A needs-you task card asks "Allow N actions?" and Allow all answers the engine. A terminal or diff card
// quotes lines only when the tab points at something. An empty conversation shows its title and nothing else.
const SAVED = 'projects/9446cc2627f3deae/transcript.jsonl';
// Idle saved tab in the browser: Copy link is present (the session file names a chat), Move to new window is
// absent (a tab moves only in the desktop app), and Close and stop is absent (nothing is running).
const DESIGN_ORDER = ['Open in split', 'Add to group', 'Pin tab', 'Duplicate', 'Rename tab', 'Copy link', 'Close tab', 'Close other tabs', 'Close tabs to the right'];
// A tab that was never sent has no link, so Copy link is left off. The remaining items stay in that same order.
const UNSENT_ORDER = ['Open in split', 'Add to group', 'Pin tab', 'Duplicate', 'Rename tab', 'Close tab', 'Close other tabs', 'Close tabs to the right'];
// Waiting work keeps Close and stop, listed after Close tab (Shell 3l). A task session file that is not a saved chat has no Copy link.
const WAITING_ORDER = ['Open in split', 'Add to group', 'Pin tab', 'Duplicate', 'Rename tab', 'Close tab', 'Close and stop', 'Close other tabs', 'Close tabs to the right'];

const permission = (id: number, head: string, tasks: string[]): EngineQuestion => ({
  id, kind: 'consent', ask: 'permission', head, batch: 'step:1',
  options: [{ key: '1', label: 'allow once' }, { key: '3', label: 'deny', safe: true }],
  blocking: { turn: true, tasks },
});
const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, titleSource: 'manual', kind: 'conversation', draft: '', pinned: false, ...over });

async function seed(page: Page, tabs: Record<string, unknown>[], activeId: string) {
  const state = { tabs, groups: [], closed: [], activeId, nextNumber: tabs.length + 1, recentIds: tabs.map(item => item.id) };
  await page.addInitScript(value => { localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
}

const overview = (page: Page) => page.getByRole('dialog', { name: 'All tabs overview', exact: true });
const card = (page: Page, id: string) => overview(page).locator(`.overview-card[data-card-id="${id}"]`);
const labels = (menu: Locator) => menu.locator(':scope > [role^="menuitem"] .menu-label').allTextContents();
const answers = (engine: MockEngine) => engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/answer'));

async function openOverview(page: Page) {
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  await expect(overview(page)).toBeVisible();
}

async function cardMenu(page: Page, title: string) {
  await overview(page).locator('.overview-card', { has: page.locator('.overview-card-title', { hasText: new RegExp(`^${title}$`) }) }).click({ button: 'right', position: { x: 40, y: 40 } });
  const menu = page.getByRole('menu', { name: `Actions for ${title}`, exact: true });
  await expect(menu).toBeVisible();
  return menu;
}

const diff = {
  lines: 40,
  hunks: [{ header: '@@ -1 +1 @@', oldStart: 1, oldLines: 1, newStart: 1, newLines: 1, lines: [
    { kind: 'del' as const, text: 'return Token{Kind: Comma}' },
    { kind: 'add' as const, text: 'if l.peekClose() && !l.strict {' },
  ] }],
};

test('right-clicking a card lists the tab menu in design order, the same list as the strip', async ({ page }) => {
  await page.route('**/api/engine/**', route => route.abort());
  await seed(page, [tab('a', 'Config stack', { sessionFile: SAVED }), tab('b', 'lexer.go')], 'a');
  await page.goto('/');
  await page.getByRole('tab', { name: 'Config stack', exact: true }).click({ button: 'right' });
  const strip = page.getByRole('menu', { name: 'Actions for Config stack', exact: true });
  await expect(strip).toBeVisible();
  const fromStrip = await labels(strip);
  await page.keyboard.press('Escape');
  await openOverview(page);
  const menu = await cardMenu(page, 'Config stack');
  expect(await labels(menu)).toEqual(DESIGN_ORDER);
  expect(await labels(menu)).toEqual(fromStrip);
  await expect(menu.getByRole('menuitem', { name: 'Move to new window' })).toHaveCount(0);
  await expect(menu.getByRole('menuitem', { name: /^Close and stop/ })).toHaveCount(0);
});

for (const scheme of ['light', 'dark'] as const) {
  test(`Shift+F10 on a card opens that menu, Enter pins, and Escape restores focus: ${scheme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await page.route('**/api/engine/**', route => route.abort());
    await seed(page, [tab('a', 'Config stack', { sessionFile: SAVED }), tab('b', 'lexer.go')], 'a');
    await page.goto('/');
    await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', scheme);
    await openOverview(page);
    const open = card(page, 'a').getByRole('button', { name: 'Open Config stack', exact: true });
    await open.focus();
    await page.keyboard.press('Shift+F10');
    const menu = page.getByRole('menu', { name: 'Actions for Config stack', exact: true });
    await expect(menu).toBeVisible();
    await page.mouse.move(0, 0);
    await expect(menu.getByRole('menuitem', { name: 'Open in split' })).toBeFocused();
    expect(await labels(menu)).toEqual(DESIGN_ORDER);
    await page.keyboard.press('ArrowDown');
    await expect(menu.getByRole('menuitem', { name: 'Add to group' })).toBeFocused();
    // The menu moves focus on a timeout, so Enter waits until Pin tab actually has it.
    await page.keyboard.press('ArrowDown');
    await expect(menu.getByRole('menuitem', { name: 'Pin tab' })).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(menu).toHaveCount(0);
    await expect(overview(page)).toBeVisible();
    expect((await savedWorkspace(page)).tabs.find(item => item.id === 'a')?.pinned).toBe(true);

    await open.focus();
    await page.keyboard.press('Shift+F10');
    await expect(menu.getByRole('menuitem', { name: 'Unpin tab' })).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(menu).toHaveCount(0);
    await expect(overview(page)).toBeVisible();
    await expect(open).toBeFocused();
  });
}

test('a needs-you task card says Allow N actions and Allow all answers the engine; Review opens the tab', async ({ page }) => {
  const base = plainReply();
  const questions = [permission(1, 'Run git step 1', ['t3']), permission(2, 'Run git step 2', ['t3']), permission(3, 'Run git step 3', ['t3'])];
  const engine = await installMockEngine(page, { ...base, initial: { ...base.initial, needsPerson: true, questions, tasks: [{ ID: 't3', Title: 'Port', Status: 'running' }] } });
  await seed(page, [
    tab('quiet', 'Quiet chat'),
    tab('task', 'Port fix', { kind: 'task', sessionFile: 'fix.jsonl', route: { taskId: 't3', back: [''], forward: [] } }),
  ], 'task');
  await page.goto('/');
  await openOverview(page);
  const task = card(page, 'task');
  await expect(task).toContainText('Allow 3 actions?');
  const waiting = await cardMenu(page, 'Port fix');
  expect(await labels(waiting)).toEqual(WAITING_ORDER);
  await page.keyboard.press('Escape');
  await expect(waiting).toHaveCount(0);
  await expect(task.locator('.overview-card-state')).toHaveText('Needs you');
  await expect(task).toContainText('Allow 3 actions?');
  const allow = task.getByRole('button', { name: 'Allow all', exact: true });
  const review = task.getByRole('button', { name: 'Review', exact: true });
  await expect(allow).toBeVisible();
  await expect(review).toBeVisible();
  await allow.click();
  await expect.poll(() => answers(engine).length).toBe(3);
  expect(answers(engine).map(call => call.body.id)).toEqual([1, 2, 3]);
  expect(answers(engine).map(call => call.body.key)).toEqual(['1', '1', '1']);
  await expect(overview(page)).toBeVisible();
  await expect(task.getByRole('button', { name: 'Allow all', exact: true })).toHaveCount(0);

  await engine.update({ needsPerson: true, questions: [permission(9, 'Run once more', ['t3'])] });
  await expect(task).toContainText('Run once more');
  await task.getByRole('button', { name: 'Review', exact: true }).focus();
  await page.keyboard.press('Enter');
  await expect(overview(page)).toHaveCount(0);
  await expect(page.getByRole('tab', { name: 'Port fix', exact: true })).toHaveAttribute('aria-selected', 'true');
});

test('a terminal or diff card shows lines only with a target, and an empty conversation shows its title only', async ({ page }) => {
  const base = plainReply();
  await installMockEngine(page, {
    ...base,
    terminals: [{ id: 'job-1', command: 'nightly-bench', title: 'nightly-bench', output: 'goos: darwin\r\nok codeaf/parse 4.2s\r\n' }],
    diffs: { 'internal/parse/lexer.go': diff },
  });
  await seed(page, [
    tab('quiet', 'Quiet chat'),
    tab('term-bare', 'Bare terminal', { kind: 'terminal' }),
    tab('term', 'Bench output', { kind: 'terminal', target: { sessionId: 'mock-1', terminalId: 'job-1' } }),
    tab('diff-bare', 'Bare diff', { kind: 'diff' }),
    tab('diff', 'Lexer changes', { kind: 'diff', target: { sessionId: 'mock-1', path: 'internal/parse/lexer.go' } }),
  ], 'quiet');
  await page.goto('/');
  await openOverview(page);

  const quiet = card(page, 'quiet');
  await expect(quiet.locator('.overview-card-title')).toHaveText('Quiet chat');
  await expect(quiet.locator('.overview-card-head')).toContainText('Conversation');
  await expect(quiet.locator('.overview-card-body')).toHaveText('');
  await expect(quiet.locator('.overview-card-state')).toHaveCount(0);
  await expect(quiet.locator('.overview-card-foot')).toHaveCount(0);
  expect((await quiet.innerText()).split('\n').map(line => line.trim()).filter(Boolean)).toEqual(['Conversation', 'Quiet chat']);

  await expect(card(page, 'term').getByRole('group', { name: 'Last output' })).toContainText('ok codeaf/parse 4.2s');
  await expect(card(page, 'term-bare').getByRole('group', { name: 'Last output' })).toHaveCount(0);
  await expect(card(page, 'term-bare').locator('.overview-card-body')).toHaveText('');
  await expect(card(page, 'term-bare').locator('.overview-card-title')).toHaveText('Bare terminal');

  const changes = card(page, 'diff').getByRole('group', { name: 'First changes' });
  await expect(changes).toContainText('− return Token{Kind: Comma}');
  await expect(changes).toContainText('+ if l.peekClose() && !l.strict {');
  await expect(card(page, 'diff-bare').getByRole('group', { name: 'First changes' })).toHaveCount(0);
  await expect(card(page, 'diff-bare').locator('.overview-card-body')).toHaveText('');
  await expect(card(page, 'diff-bare').locator('.overview-card-title')).toHaveText('Bare diff');
});

for (const scheme of ['light', 'dark'] as const) for (const width of [320, 600, 1200]) {
  test(`the card menu and bodies stay on screen: ${scheme} at ${width}px`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await page.setViewportSize({ width, height: 800 });
    const base = plainReply();
    const questions = [permission(1, 'Run git step 1', ['t3']), permission(2, 'Run git step 2', ['t3']), permission(3, 'Run git step 3', ['t3'])];
    await installMockEngine(page, {
      ...base,
      initial: { ...base.initial, needsPerson: true, questions, tasks: [{ ID: 't3', Title: 'Port', Status: 'running' }] },
      terminals: [{ id: 'job-1', command: 'nightly-bench', title: 'nightly-bench', output: 'ok codeaf/parse 4.2s\r\n' }],
      diffs: { 'internal/parse/lexer.go': diff },
    });
    await seed(page, [
      tab('a', 'Config stack'),
      tab('quiet', 'Quiet chat'),
      tab('task', 'Port fix', { kind: 'task', sessionFile: 'fix.jsonl', route: { taskId: 't3', back: [''], forward: [] } }),
      tab('term-bare', 'Bare terminal', { kind: 'terminal' }),
      tab('term', 'Bench output', { kind: 'terminal', target: { sessionId: 'mock-1', terminalId: 'job-1' } }),
      tab('diff-bare', 'Bare diff', { kind: 'diff' }),
    ], 'task');
    await page.goto('/');
    await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', scheme);
    await openOverview(page);

    const task = card(page, 'task');
    await expect(task).toContainText('Allow 3 actions?');
    await expect(task.getByRole('button', { name: 'Allow all', exact: true })).toBeVisible();
    await expect(task.getByRole('button', { name: 'Review', exact: true })).toBeVisible();
    await expect(card(page, 'quiet').locator('.overview-card-body')).toHaveText('');
    await expect(card(page, 'term').getByRole('group', { name: 'Last output' })).toBeVisible();
    await expect(card(page, 'term-bare').locator('.overview-card-body')).toHaveText('');
    await expect(card(page, 'diff-bare').locator('.overview-card-body')).toHaveText('');

    const open = card(page, 'a').getByRole('button', { name: 'Open Config stack', exact: true });
    await open.focus();
    await page.mouse.move(0, 0);
    await page.keyboard.press('Shift+F10');
    const menu = page.getByRole('menu', { name: 'Actions for Config stack', exact: true });
    await expect(menu).toBeVisible();
    expect(await labels(menu)).toEqual(UNSENT_ORDER);
    await expect(menu.getByRole('menuitem', { name: 'Open in split' })).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(menu).toHaveCount(0);
    await expect(overview(page)).toBeVisible();
    // A press at the card's left edge is the width-sensitive path: the menu has to stay inside the window.
    await page.mouse.move(0, 0);
    const pointer = await cardMenu(page, 'Config stack');
    await page.evaluate(async () => {
      const animations = document.getAnimations().filter(animation => animation.effect?.getComputedTiming().iterations !== Infinity);
      await Promise.all(animations.map(animation => animation.finished.catch(() => undefined)));
    });
    const box = (await pointer.boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.y).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(width + 1);
    expect(box.y + box.height).toBeLessThanOrEqual(801);
    await page.keyboard.press('Escape');
    await expect(pointer).toHaveCount(0);
    await expectAccessible(page);
    expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(1);
  });
}
