import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { expectAccessible, INK3_TEXT, tokenColor } from './contracts';
import { installMockEngine } from './support/mock-engine';
import { NOW, designConversations, withHistory } from './support/scenarios-history';
import type { MockConversation } from './support/history-engine';
import type { WorldRow } from '../../src/features/world/types';

// TH-04, TH-12, TH-13, TH-14, TH-21, TN-07. History routes come from history-engine through the mock.
// The world feed is not that list: page.route publishes the other-window rows the list lays on top.
const option = (page: Page, title: string) => page.getByRole('option', { name: new RegExp(title) });
const schemeOf = (page: Page, scheme: 'light' | 'dark') => page.addInitScript(value => localStorage.setItem('codeaf-theme', value), scheme);

async function openHistory(page: Page, conversations: MockConversation[], places?: 'typical-day' | 'empty') {
  await page.clock.setFixedTime(NOW);
  const engine = await installMockEngine(page, { ...withHistory(conversations), ...(places ? { places } : {}) });
  await page.goto('/');
  await page.keyboard.press('Control+y');
  await expect(page.getByRole('heading', { name: 'History' })).toBeVisible();
  return engine;
}

/** One world reset for every reader, held until the list has drawn the closed rows. */
async function holdWorld(page: Page, rows: WorldRow[]) {
  let released = false;
  const waiting: (() => void)[] = [];
  await page.route('**/api/engine/events?*', async route => {
    const after = Number(new URL(route.request().url()).searchParams.get('after') ?? '0');
    if (after > 0) {
      await route.fulfill({ status: 200, contentType: 'text/event-stream', body: ': connected\n\n' });
      return;
    }
    if (!released) await new Promise<void>(resolve => waiting.push(resolve));
    const record = { epoch: 'hist-live', seq: 1, type: 'reset', at: NOW.toISOString(), payload: { rows, items: [] } };
    await route.fulfill({ status: 200, headers: { 'Cache-Control': 'no-cache' }, contentType: 'text/event-stream', body: `: connected\n\nid: 1\ndata: ${JSON.stringify(record)}\n\n` });
  });
  return () => { released = true; for (const go of waiting) go(); };
}

const live = (chatId: string, over: Partial<WorldRow> = {}): WorldRow => ({
  chatId, running: false, needsYou: 0, failed: 0, tasksRunning: 0, tasksTotal: 0, attached: false, archived: false, ...over,
});

/**
 * The same scan as expectAccessible. Delete uses the shared danger button — danger text on danger-soft,
 * Foundations' light token — and that pair is under 4.5:1. The design wins, so only that contrast node is
 * waived. Dark already clears it. Every other rule on the open confirm still has to pass.
 */
async function expectConfirmAccessible(page: Page) {
  await page.evaluate(async () => {
    const finite = () => document.getAnimations().filter(animation => animation.playState === 'running' && animation.effect?.getComputedTiming().iterations !== Infinity);
    for (let pass = 0; pass < 3; pass += 1) {
      const running = finite();
      if (running.length === 0) {
        await new Promise(resolve => requestAnimationFrame(() => resolve(undefined)));
        if (finite().length === 0) return;
      }
      await Promise.all(finite().map(animation => animation.finished.catch(() => undefined)));
    }
  });
  for (let attempt = 0; attempt < 3; attempt += 1) {
    const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze();
    const { found, stale } = await page.evaluate(({ violations, muted }) => {
      let stale = false;
      const found: { id: string; nodes: string[][] }[] = [];
      for (const violation of violations) {
        const nodes = violation.nodes.filter(node => {
          if (violation.id !== 'color-contrast') return true;
          const target = node.target;
          const selector = Array.isArray(target) ? String(target[target.length - 1]) : String(target);
          const element = document.querySelector(selector);
          if (!element) stale = true;
          return !element?.closest(muted) && !element?.closest('.button-danger');
        }).map(node => node.target);
        if (nodes.length) found.push({ id: violation.id, nodes });
      }
      return { found, stale };
    }, { violations: result.violations, muted: INK3_TEXT.join(',') });
    if (stale && attempt < 2) continue;
    expect(found).toEqual([]);
    return;
  }
}

test('TH-04: a conversation attached elsewhere, or closed but running, shows its live Open row', async ({ page }) => {
  await page.clock.setFixedTime(NOW);
  await installMockEngine(page, withHistory(designConversations()));
  // Registered after the mock so this feed is the one the history list reads.
  const release = await holdWorld(page, [
    // Another window's transcript. The chat id is not this bridge's, so only the session file can lift the row.
    live('other-window', { sessionFile: '/mock/places/naming/transcript.jsonl', running: true, tasksRunning: 2, tasksTotal: 2 }),
    live('ci', { needsYou: 1 }),
    live('lexer'),
  ]);
  await page.goto('/');
  await page.keyboard.press('Control+y');
  await expect(page.getByRole('heading', { name: 'History' })).toBeVisible();
  const naming = option(page, 'Naming for the config');
  const waiting = option(page, 'Why does CI fail');
  await expect(naming).toContainText('Discussed Load vs Open. No decision yet');
  await expect(waiting).toContainText('A trailing comma in the shared fixture');
  const titles = () => page.locator('.history-row-title').allTextContents();
  const before = await titles();
  release();
  await expect(naming).toContainText('Open · 2 tasks running');
  await expect(naming.locator('.history-row-stamp')).toHaveText('Open');
  await expect(naming.locator('.history-lead')).toHaveAttribute('data-lead', 'working');
  await expect(waiting).toContainText('Open · waiting on you');
  await expect(waiting.locator('.history-lead')).toHaveAttribute('data-lead', 'needs-you');
  await expect(option(page, 'Fix it in the lexer')).toContainText('Decided to keep strict mode');
  expect(await titles()).toEqual(before);
  await expect(page.getByRole('tab', { name: /Naming for the config/ })).toHaveCount(0);
  const working = naming.locator('.history-dot');
  const question = waiting.locator('.history-dot');
  const box = await working.evaluate(el => el.getBoundingClientRect());
  expect(box.width).toBe(6);
  expect(box.height).toBe(6);
  await expect(working).toHaveCSS('background-color', await tokenColor(page, 'mark-running'));
  await expect(question).toHaveCSS('background-color', await tokenColor(page, 'mark-waiting'));
  const line = await naming.locator('.history-row-line').evaluate(el => getComputedStyle(el).color);
  const dot = await working.evaluate(el => getComputedStyle(el).backgroundColor);
  expect(line).not.toBe(dot);
  await expect(naming.locator('.history-row-line')).toHaveCSS('color', await tokenColor(page, 'ink-2'));
});

test('TH-12: the row menu is Continue, Read, Add to place, then Archive or Delete', async ({ page }) => {
  const filed: { path: string; body: { chats?: string[] } }[] = [];
  const engine = await installMockEngine(page, { ...withHistory(designConversations().map(row => row.id === 'lexer' ? { ...row, archived: true } : row)), places: 'typical-day' });
  await page.clock.setFixedTime(NOW);
  await page.route('**/api/engine/places/*/members', async route => {
    if (route.request().method() === 'POST') filed.push({ path: new URL(route.request().url()).pathname, body: route.request().postDataJSON() });
    await route.fallback();
  });
  await page.goto('/');
  await page.keyboard.press('Control+y');
  await expect(page.getByRole('navigation', { name: 'Places' }).getByText('codeaf', { exact: true })).toBeVisible();
  const idle = option(page, 'Does JSON5 handle this');
  await idle.click({ button: 'right' });
  const idleMenu = page.getByRole('menu', { name: 'Does JSON5 handle this? actions' });
  await expect(idleMenu.getByRole('menuitem')).toHaveText(['Continue', 'Read', 'Add to place', 'Archive']);
  await expect(idleMenu.getByRole('separator')).toHaveCount(1);
  await expect(idleMenu.getByRole('menuitem', { name: 'Delete…' })).toHaveCount(0);
  await page.keyboard.press('Escape');

  const archived = option(page, 'Fix it in the lexer');
  await archived.click({ button: 'right' });
  const menu = page.getByRole('menu', { name: 'Fix it in the lexer actions' });
  await expect(menu.getByRole('menuitem')).toHaveText(['Continue', 'Read', 'Add to place', 'Unarchive', 'Delete…']);
  await menu.getByRole('menuitem', { name: 'Add to place' }).hover();
  const places = page.locator('.app-menu-submenu');
  await expect(places.getByRole('menuitem').first()).toHaveText('codeaf');
  await expect(places.getByRole('menuitem', { name: 'All places…' })).toBeVisible();
  await places.getByRole('menuitem', { name: 'codeaf', exact: true }).click();
  await expect.poll(() => filed.length).toBe(1);
  expect(filed[0].path).toMatch(/\/places\/codeaf\/members$/);
  expect(filed[0].body.chats).toEqual(['lexer']);

  await archived.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Unarchive' }).click();
  await expect.poll(() => engine.history.archived()).toEqual([{ id: 'lexer', archived: false }]);
  await archived.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Read', exact: true }).click();
  await expect(page.locator('.history-message')).toHaveCount(9);
});

for (const gesture of ['command', 'middle'] as const) {
  test(`TH-13: a ${gesture} click opens the row in a new tab`, async ({ page }) => {
    await openHistory(page, designConversations());
    const row = option(page, 'Does JSON5 handle this');
    const press = gesture === 'command' ? { modifiers: ['ControlOrMeta' as const] } : { button: 'middle' as const };
    await row.click(press);
    const history = page.getByRole('tab', { name: /History/ });
    const opened = page.getByRole('tab', { name: /Does JSON5 handle this/ });
    await expect(opened).toHaveCount(1);
    await expect(history).toHaveCount(1);
    // A command-click moves to the new tab. A middle click leaves it behind, the way a browser does.
    await expect(history).toHaveAttribute('aria-selected', gesture === 'middle' ? 'true' : 'false');
    await expect(opened).toHaveAttribute('aria-selected', gesture === 'command' ? 'true' : 'false');
    await page.getByRole('tab', { name: /History/ }).click();
    await row.click(press);
    await expect(opened).toHaveCount(1);
  });
}

/** Fourteen archived chats, one minute apart, so a shift-click can cover the whole day. */
function archivedChats(count: number): MockConversation[] {
  return Array.from({ length: count }, (_, index) => {
    const n = String(index + 1).padStart(2, '0');
    return {
      id: `chat-${n}`, title: `Chat ${n}`, at: new Date(NOW.getTime() - index * 60_000).toISOString(), archived: true,
      messages: [{ role: 'user', text: `Note ${n}` }],
      recap: { line: `Discussed chat ${n}`, discussed: '', decided: [], outcome: '', files: [] },
    };
  });
}

test('TH-14: fourteen archived chats confirm together and Undo puts them back', async ({ page }) => {
  test.setTimeout(45_000);
  const engine = await openHistory(page, archivedChats(14));
  const list = page.getByRole('listbox', { name: 'Conversations' });
  await option(page, 'Chat 01').click();
  await list.evaluate(node => { node.scrollTop = node.scrollHeight; });
  const last = option(page, 'Chat 14');
  await expect(last).toBeVisible();
  await last.click({ modifiers: ['Shift'] });
  await last.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete…' }).click();
  const many = page.getByRole('group', { name: 'Delete 14 chats?' });
  await expect(many).toBeVisible();
  await expect(option(page, 'Chat 01')).toHaveCount(0);
  await expect(page.locator('dialog[open]')).toHaveCount(0);
  await many.getByRole('button', { name: 'Delete', exact: true }).click();
  const toast = page.locator('.toast').filter({ hasText: '14 chats deleted' });
  await page.clock.fastForward(300);
  await expect(toast.getByRole('button', { name: 'Undo' })).toBeVisible();
  await page.clock.fastForward(9_000);
  await expect(toast).toBeVisible();
  await toast.getByRole('button', { name: 'Undo' }).click();
  await expect(toast).toHaveCount(0);
  await expect(option(page, 'Chat 01')).toBeVisible();
  await expect(option(page, 'Chat 14')).toBeVisible();
  expect(engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/history/restore'))).toHaveLength(1);

  // The fourteen-row selection is still held. A plain click narrows it, so this Delete… asks about one chat.
  await option(page, 'Chat 01').click();
  await option(page, 'Chat 01').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete…' }).click();
  await page.getByRole('group', { name: 'Delete 1 chat?' }).getByRole('button', { name: 'Delete', exact: true }).click();
  const single = page.locator('.toast').filter({ hasText: '1 chat deleted' });
  await page.clock.fastForward(300);
  await expect(single).toBeVisible();
  await page.clock.fastForward(10_000);
  await expect(single).toHaveCount(0);
  await expect(option(page, 'Chat 01')).toHaveCount(0);
  expect(engine.calls.filter(call => call.path.endsWith('/history/restore'))).toHaveLength(1);
});

test('an idle row has Archive and no Delete until it is archived', async ({ page }) => {
  const conversations = designConversations();
  const engine = await openHistory(page, conversations);
  const row = option(page, 'Does JSON5 handle this');
  await row.click({ button: 'right' });
  const menu = page.getByRole('menu', { name: 'Does JSON5 handle this? actions' });
  await expect(menu.getByRole('menuitem', { name: 'Archive', exact: true })).toBeEnabled();
  await expect(menu.getByRole('menuitem', { name: 'Delete…' })).toHaveCount(0);
  await menu.getByRole('menuitem', { name: 'Archive', exact: true }).click();
  await expect.poll(() => engine.history.archived()).toEqual([{ id: 'json5', archived: true }]);
  await row.click({ button: 'right' });
  await expect(page.getByRole('menuitem', { name: 'Delete…' })).toBeVisible();
  await expect(page.getByRole('menuitem', { name: 'Unarchive' })).toBeVisible();
});

function zephyrChats(): MockConversation[] {
  return Array.from({ length: 14 }, (_, index) => {
    const n = String(index + 1).padStart(2, '0');
    return {
      id: `z-${n}`, title: `Zephyr ${n}`, at: new Date(NOW.getTime() - index * 60_000).toISOString(), archived: index === 0,
      messages: [{ role: 'user', text: `zephyr note ${n}` }],
    };
  });
}

test('TH-21 TN-07: ⌘T lists From history, marks archived, and See all 14 opens History', async ({ page }) => {
  await page.clock.setFixedTime(NOW);
  const engine = await installMockEngine(page, withHistory(zephyrChats()));
  await page.goto('/');
  await expect(page.getByRole('tab').first()).toBeVisible();
  await page.keyboard.press('Control+t');
  const field = page.getByRole('combobox', { name: 'Search or start' });
  await expect(field).toBeFocused();
  await field.fill('zephyr');
  const section = page.getByRole('group', { name: 'From history' });
  await expect(section).toBeVisible();
  await expect(section.getByRole('option', { name: /Zephyr 01/ })).toContainText(/· archived/);
  const seeAll = section.getByRole('option', { name: /^See all 14 in History/ });
  await expect(seeAll).toBeVisible();
  await expect(seeAll).toContainText(/⌘↵|Ctrl ↵/);
  expect(engine.calls.filter(call => call.path.endsWith('/history/search')).length).toBeGreaterThan(0);
  await field.press('Control+Enter');
  await expect(page.getByRole('tab', { name: /^History/ })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('textbox', { name: 'Search history' })).toHaveValue('zephyr');
});

for (const key of ['Shift+F10', 'ContextMenu']) {
  test(`${key} opens the selected row menu and Escape returns focus to the list`, async ({ page }) => {
    await openHistory(page, designConversations());
    const list = page.getByRole('listbox', { name: 'Conversations' });
    await list.focus();
    await page.keyboard.press('ArrowDown');
    await page.keyboard.press(key);
    const menu = page.getByRole('menu', { name: 'Release v2.4 actions' });
    await expect(menu).toBeVisible();
    await expect(menu.getByRole('menuitem', { name: 'Continue' })).toBeFocused();
    await expect(menu.getByRole('menuitem', { name: 'Archive', exact: true })).toBeDisabled();
    await page.keyboard.press('Escape');
    await expect(menu).toHaveCount(0);
    await expect(list).toBeFocused();
  });
}

for (const scheme of ['light', 'dark'] as const) {
  test(`axe ${scheme}: the row menu and the delete confirm are open`, async ({ page }) => {
    test.setTimeout(60_000);
    await schemeOf(page, scheme);
    await openHistory(page, archivedChats(2), 'typical-day');
    await expect(page.locator('html')).toHaveAttribute('data-theme', scheme);
    await expect(page.getByRole('navigation', { name: 'Places' }).getByText('codeaf', { exact: true })).toBeVisible();
    await option(page, 'Chat 01').click({ button: 'right' });
    const menu = page.getByRole('menu', { name: 'Chat 01 actions' });
    await expect(menu.getByRole('menuitem', { name: 'Add to place' })).toBeVisible();
    await expectAccessible(page);
    await menu.getByRole('menuitem', { name: 'Delete…' }).click();
    const confirm = page.getByRole('group', { name: 'Delete 1 chat?' });
    await expect(confirm).toBeVisible();
    await expect(page.getByRole('menu')).toHaveCount(0);
    await expect(page.locator('.history-list .history-delete-confirm')).toHaveCount(0);
    await expectConfirmAccessible(page);
  });
}
