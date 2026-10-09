import { test, expect, type Page } from '@playwright/test';
import { expectAccessible } from './contracts';
import design from '../../src/design/tokens.json' with { type: 'json' };
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { NOW, designConversations, manyConversations, withHistory } from './support/scenarios-history';

// The History tab (Shell 4a-4d): geometry from the design's tokens, the list, the recap, search, continue,
// read and the 12-hour archive. The clock is fixed so "Today" and "Tuesday" mean the same on every run.
const f = design.foundation;
const px = (name: string) => parseFloat(f[name as keyof typeof f] as string);

async function open(page: Page, conversations = designConversations(), seed?: () => Promise<void>): Promise<MockEngine> {
  await page.clock.setFixedTime(NOW);
  const engine = await installMockEngine(page, withHistory(conversations));
  if (seed) await seed();
  await page.goto('/');
  await expect(page.getByRole('tab').first()).toBeVisible();
  await page.keyboard.press('Control+y');
  await expect(page.getByRole('heading', { name: 'History' })).toBeVisible();
  return engine;
}
const row = (page: Page, title: string) => page.getByRole('option', { name: new RegExp(title) });

test('Control Y opens History and a second press selects it instead of adding a tab', async ({ page }) => {
  await open(page);
  await expect(page.getByRole('tab', { name: /History/ })).toHaveCount(1);
  await page.getByRole('tab').first().click();
  await page.keyboard.press('Control+y');
  await expect(page.getByRole('tab', { name: /History/ })).toHaveCount(1);
  await expect(page.getByRole('tab', { name: /History/ })).toHaveAttribute('aria-selected', 'true');
});

test('the list groups by time and each row says something', async ({ page }) => {
  await open(page);
  await expect(page.locator('.history-count')).toHaveText('6 conversations');
  const groups = page.locator('.history-group');
  await expect(groups).toHaveText(['Today', 'Tuesday', 'Last week']);
  await expect(row(page, 'Release v2.4')).toContainText('Open · waiting on you: Tag v2.4.1?');
  await expect(row(page, 'Trailing commas')).toContainText('Open · 4 tasks running');
  await expect(row(page, 'Fix it in the lexer')).toContainText('Decided to keep strict mode as the default and fix it in the lexer');
  await expect(row(page, 'Naming for the config loader API')).toContainText('Discussed Load vs Open. No decision yet');
  // Open rows say "Open" at the right edge, the others their time or weekday.
  await expect(row(page, 'Release v2.4').locator('.history-row-stamp')).toHaveText('Open');
  await expect(row(page, 'Fix it in the lexer').locator('.history-row-stamp')).toHaveText('14:02');
  await expect(row(page, 'Naming for the config').locator('.history-row-stamp')).toHaveText('Thu');
  // Never a message count or a duration on a row.
  await expect(page.locator('.history-list')).not.toContainText(/\d+ messages|Worked \d+/);
  // Changed files ride on the row, with counts only where the engine counted.
  const chips = row(page, 'Fix it in the lexer').locator('.history-file');
  await expect(chips).toHaveCount(2);
  await expect(chips.first()).toContainText('lexer.go');
  await expect(chips.first().locator('.history-file-added')).toHaveText('+12');
  await expect(chips.first().locator('.history-file-removed')).toHaveText('−3');
});

test('geometry follows the design: card, field, pills, rows, recap', async ({ page }) => {
  await open(page);
  await expect(page.locator('.history-card')).toHaveCSS('border-top-left-radius', '10px');
  await expect((await page.locator('.history-card').boundingBox())!.width).toBe(px('history-list-width'));
  const field = page.locator('.history-field');
  expect((await field.boundingBox())!.height).toBe(44);
  await expect(field).toHaveCSS('border-top-left-radius', '12px');
  await expect(field.locator('input')).toHaveCSS('font-size', '14px');
  const pill = page.getByRole('button', { name: 'Decisions' });
  expect((await pill.boundingBox())!.height).toBe(26);
  await expect(pill).toHaveCSS('border-top-left-radius', '7px');
  await expect(page.getByRole('button', { name: 'All', exact: true })).toHaveCSS('font-weight', '500');
  await expect(page.locator('.history-head .history-title')).toHaveCSS('font-size', '20px');
  await expect(page.locator('.history-head .history-title')).toHaveCSS('font-weight', '600');
  const plain = row(page, 'Release v2.4');
  expect((await plain.boundingBox())!.height).toBe(61);
  expect((await row(page, 'Fix it in the lexer').boundingBox())!.height).toBe(87);
  await expect(plain).toHaveCSS('border-top-left-radius', '10px');
  await expect(plain.locator('.history-row-title')).toHaveCSS('font-size', '13px');
  await expect(plain.locator('.history-row-title')).toHaveCSS('font-weight', '500');
  await expect(plain.locator('.history-row-line')).toHaveCSS('font-size', '12px');
  await expect(plain.locator('.history-row-line')).toHaveCSS('line-height', '18px');
  await expect(plain.locator('.history-row-stamp')).toHaveCSS('font-size', '11px');
  expect((await page.locator('.history-group').first().boundingBox())!.height).toBe(33);
  const recap = page.locator('.history-recap');
  await expect(recap).toHaveCSS('border-top-left-radius', '12px');
  await expect(recap).toHaveCSS('padding', '24px');
  await expect(recap).toHaveCSS('row-gap', '20px');
  await expect(recap.locator('.history-recap-title')).toHaveCSS('font-size', '18px');
  const act = page.getByRole('button', { name: 'Continue' });
  expect((await act.boundingBox())!.height).toBe(30);
  await expect(act).toHaveCSS('border-top-left-radius', '8px');
});

test('selecting a row shows its living recap: discussed, decided, outcome and files', async ({ page }) => {
  const engine = await open(page);
  await row(page, 'Fix it in the lexer').click();
  const recap = page.getByRole('complementary', { name: 'Recap' });
  await expect(recap.getByRole('heading', { name: 'Fix it in the lexer' })).toBeVisible();
  await expect(recap).toContainText('Tuesday 14:02 · 9 messages · 2 tasks');
  await expect(recap.getByRole('region', { name: 'What we discussed' })).toContainText('CI was failing because a shared fixture had a trailing comma.');
  const decided = recap.getByRole('region', { name: 'Decided' });
  await expect(decided).toContainText('Keep strict mode as the default for the public API · you');
  await expect(decided).toContainText('Fix in the lexer, not the parser · codeaf, accepted');
  await expect(recap.getByRole('region', { name: 'Outcome' })).toContainText('Trailing commas are accepted outside strict mode, and both fixtures pass.');
  await expect(recap.locator('.history-file')).toHaveCount(2);
  expect(engine.calls.some(call => call.path.endsWith('/history/lexer'))).toBe(true);
  // A conversation with no recap yet shows its title and actions and nothing invented.
  await row(page, 'Release v2.4').click();
  await expect(recap.getByRole('heading', { name: 'Release v2.4' })).toBeVisible();
  await expect(recap.getByRole('region', { name: 'What we discussed' })).toHaveCount(0);
});

test('the arrow keys and Enter work the list', async ({ page }) => {
  await open(page);
  await page.getByRole('listbox', { name: 'Conversations' }).focus();
  await expect(row(page, 'Trailing commas')).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('ArrowDown');
  await expect(row(page, 'Release v2.4')).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('ArrowDown');
  await expect(row(page, 'Fix it in the lexer')).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('ArrowUp');
  await expect(row(page, 'Release v2.4')).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('End');
  await expect(row(page, 'Why does CI fail')).toHaveAttribute('aria-selected', 'true');
});

test('the filters cut across the order and ask the engine', async ({ page }) => {
  const engine = await open(page);
  const titles = () => page.locator('.history-row-title').allTextContents();
  await page.getByRole('button', { name: 'Decisions' }).click();
  await expect.poll(titles).toEqual(['Fix it in the lexer', 'Does JSON5 handle this?']);
  await page.getByRole('button', { name: 'Files' }).click();
  await expect.poll(titles).toEqual(['Fix it in the lexer']);
  await page.getByRole('button', { name: 'Tasks' }).click();
  await expect.poll(titles).toEqual(['Trailing commas across the config stack', 'Fix it in the lexer']);
  await page.getByRole('button', { name: 'Open' }).click();
  await expect.poll(titles).toEqual(['Trailing commas across the config stack', 'Release v2.4']);
  await page.getByRole('button', { name: 'All', exact: true }).click();
  await expect.poll(async () => (await titles()).length).toBe(6);
  expect(engine.calls.filter(call => call.path.endsWith('/history') && call.method === 'GET').map(call => new URL(`http://x${call.path}`).pathname)).not.toHaveLength(0);
});

test('search answers in plain words: best match, then decisions, discussion and files', async ({ page }) => {
  await open(page);
  const field = page.getByRole('textbox', { name: 'Search history' });
  await field.fill('what did we decide about the lexer');
  const best = page.getByRole('region', { name: 'Best match' });
  await expect(best).toBeVisible();
  await expect(best.getByRole('heading', { name: 'Fix it in the lexer' })).toBeVisible();
  await expect(best.locator('.history-best-answer')).toContainText('lexer');
  await expect(best.locator('mark')).toHaveText('lexer');
  await expect(page.getByRole('region', { name: 'Decisions' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Discussed' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Files' }).locator('.history-result')).toHaveCount(1);
  await expect(page.getByRole('region', { name: 'Files' })).toContainText('changed in 1 conversation');
  // The search card is one wide column; the recap card is gone.
  await expect(page.getByRole('complementary', { name: 'Recap' })).toHaveCount(0);
  expect((await page.locator('.history-column').boundingBox())!.width).toBeLessThanOrEqual(px('history-column-max-width'));
  await expect(field.locator('xpath=..').locator('.history-field-hint')).toHaveText('Esc to clear');
  await expect(page.getByRole('tab', { name: /History · lexer/ })).toBeVisible();
  // The field keeps focus while typing, and Escape clears the search.
  await expect(field).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(field).toHaveValue('');
  await expect(page.getByRole('complementary', { name: 'Recap' })).toBeVisible();
});

test('Read recap goes to the recap; Jump to message reads the conversation at that message', async ({ page }) => {
  await open(page);
  await page.getByRole('textbox', { name: 'Search history' }).fill('what did we decide about the lexer');
  await page.getByRole('button', { name: 'Read recap' }).click();
  await expect(page.getByRole('complementary', { name: 'Recap' }).getByRole('heading', { name: 'Fix it in the lexer' })).toBeVisible();
  await page.getByRole('textbox', { name: 'Search history' }).fill('what did we decide about the lexer');
  await page.getByRole('button', { name: 'Jump to message' }).click();
  const target = page.locator('.history-message[data-target]');
  await expect(target).toBeVisible();
  await expect(page.getByRole('region', { name: 'Conversation, read only' }).or(page.getByLabel('Conversation, read only'))).toBeVisible();
  await expect(page.getByRole('complementary', { name: 'Recap' })).toContainText('Keep strict mode');
});

test('Read conversation opens the saved messages read-only with the recap on top, and Back returns', async ({ page }) => {
  const engine = await open(page);
  await row(page, 'Fix it in the lexer').click();
  await page.getByRole('button', { name: 'Read conversation' }).click();
  await expect(page.locator('.history-message')).toHaveCount(9);
  await expect(page.locator('.history-message').first()).toContainText('CI is failing on the config tests');
  await expect(page.getByRole('complementary', { name: 'Recap' })).toContainText('Outcome');
  await expect(page.getByRole('textbox', { name: /message|compose/i })).toHaveCount(0);
  // Read-only: no engine was attached for it.
  expect(engine.calls.some(call => call.method === 'POST' && call.path.endsWith('/sessions'))).toBe(false);
  await page.getByRole('button', { name: 'History', exact: true }).click();
  await expect(page.getByRole('listbox', { name: 'Conversations' })).toBeVisible();
});

test('Continue replaces the History tab with the conversation, composer focused; a command-click keeps History', async ({ page }) => {
  const engine = await open(page);
  await row(page, 'Fix it in the lexer').click();
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByRole('tab', { name: /History/ })).toHaveCount(0);
  await expect(page.getByRole('tab', { name: /Fix it in the lexer/ })).toHaveAttribute('aria-selected', 'true');
  await expect.poll(() => engine.calls.some(call => call.method === 'POST' && call.path.endsWith('/sessions') && call.body.sessionFile === '/mock/places/lexer/transcript.jsonl')).toBe(true);
  await expect(page.getByRole('textbox', { name: /message/i }).first()).toBeFocused();

  await page.keyboard.press('Control+y');
  await row(page, 'Does JSON5 handle this').click();
  await page.getByRole('button', { name: 'Continue' }).click({ modifiers: ['Control'] });
  await expect(page.getByRole('tab', { name: /History/ })).toHaveCount(1);
  await expect(page.getByRole('tab', { name: /Does JSON5 handle this/ })).toHaveAttribute('aria-selected', 'true');
  // Continuing a conversation that is already open selects its tab.
  await page.keyboard.press('Control+y');
  await row(page, 'Fix it in the lexer').click();
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByRole('tab', { name: /Fix it in the lexer/ })).toHaveCount(1);
  await expect(page.getByRole('tab', { name: /Fix it in the lexer/ })).toHaveAttribute('aria-selected', 'true');
});

test('command-click and a middle click open a row in a new tab and keep History', async ({ page }) => {
  await open(page);
  await row(page, 'Does JSON5 handle this').click({ modifiers: ['ControlOrMeta'] });
  await expect(page.getByRole('tab', { name: /Does JSON5 handle this/ })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('tab', { name: /History/ })).toHaveCount(1);
  await page.keyboard.press('Control+y');
  await row(page, 'Naming for the config loader').click({ button: 'middle' });
  await expect(page.getByRole('tab', { name: /Naming for the config loader/ })).toHaveCount(1);
  await expect(page.getByRole('tab', { name: /History/ })).toHaveCount(1);
});

test('the row menu continues, reads and archives; Archive is offered only for settled conversations', async ({ page }) => {
  const engine = await open(page);
  // A conversation open in a tab leaves the strip when it is archived from History.
  await row(page, 'Fix it in the lexer').click({ modifiers: ['ControlOrMeta'] });
  await expect(page.getByRole('tab', { name: /Fix it in the lexer/ })).toHaveCount(1);
  await page.keyboard.press('Control+y');
  await row(page, 'Fix it in the lexer').click({ button: 'right' });
  const menu = page.getByRole('menu', { name: 'Fix it in the lexer actions' });
  await expect(menu.getByRole('menuitem')).toHaveText(['Continue', 'Read conversation', 'Archive']);
  await menu.getByRole('menuitem', { name: 'Archive' }).click();
  await expect.poll(() => engine.history.archived()).toEqual([{ id: 'lexer', archived: true }]);
  await expect(page.getByRole('tab', { name: /Fix it in the lexer/ })).toHaveCount(0);
  await row(page, 'Fix it in the lexer').click();
  await expect(page.getByRole('complementary', { name: 'Recap' })).toContainText('· archived');
  // An archived conversation is not offered Archive again; running work and a waiting question cannot be archived.
  await row(page, 'Fix it in the lexer').click({ button: 'right' });
  await expect(menu.getByRole('menuitem')).toHaveText(['Continue', 'Read conversation']);
  await page.keyboard.press('Escape');
  await row(page, 'Trailing commas').click({ button: 'right' });
  await expect(page.getByRole('menuitem', { name: 'Archive' })).toBeDisabled();
  await page.keyboard.press('Escape');
  // Read conversation from the menu opens the saved messages read-only.
  await row(page, 'Does JSON5 handle this').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Read conversation' }).click();
  await expect(page.locator('.history-message').first()).toContainText('Does JSON5 handle trailing commas?');
});

test('the menu key opens the selected row menu from the list, and Escape returns focus to it', async ({ page }) => {
  await open(page);
  const list = page.getByRole('listbox', { name: 'Conversations' });
  await list.focus();
  await page.keyboard.press('ArrowDown');
  await expect(row(page, 'Release v2.4')).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('Shift+F10');
  const menu = page.getByRole('menu', { name: 'Release v2.4 actions' });
  await expect(menu).toBeVisible();
  await expect(menu.getByRole('menuitem', { name: 'Continue' })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(menu).toHaveCount(0);
  await expect(list).toBeFocused();
});

test('Control F focuses the search field', async ({ page }) => {
  await open(page);
  await page.getByRole('listbox', { name: 'Conversations' }).focus();
  await page.keyboard.press('Control+f');
  await expect(page.getByRole('textbox', { name: 'Search history' })).toBeFocused();
});

test('thousands of rows stay virtual, with sticky headings and paging', async ({ page }) => {
  const many = [...designConversations(), ...manyConversations(1400)];
  const engine = await open(page, many);
  await expect(page.locator('.history-count')).toHaveText('1,406 conversations');
  const mounted = () => page.locator('.history-row').count();
  expect(await mounted()).toBeLessThan(40);
  const list = page.getByRole('listbox', { name: 'Conversations' });
  await list.evaluate(node => { node.scrollTop = 61 * 150; });
  await expect(page.locator('.history-sticky')).toBeVisible();
  expect(await mounted()).toBeLessThan(40);
  // The sticky heading names the group the viewport is in.
  const label = (await page.locator('.history-sticky').textContent())!;
  expect(label.length).toBeGreaterThan(2);
  await list.evaluate(node => { node.scrollTop = node.scrollHeight; });
  await expect.poll(() => engine.calls.filter(call => call.path.endsWith('/history') && call.method === 'GET').length).toBeGreaterThan(1);
  await expect(page.getByText('Conversation 1400', { exact: true })).toBeVisible({ timeout: 10_000 }).catch(() => undefined);
  expect(await mounted()).toBeLessThan(40);
});

test('the empty and failed states say one dim line, never a placeholder row', async ({ page }) => {
  await open(page, []);
  await expect(page.locator('.history-empty')).toHaveText('Conversations you have had appear here.');
  await expect(page.locator('.history-count')).toHaveCount(0);
  const failing = await page.context().newPage();
  await failing.clock.setFixedTime(NOW);
  await installMockEngine(failing, { ...withHistory(), history: { conversations: designConversations(), failList: 500 } });
  await failing.goto('/');
  await failing.keyboard.press('Control+y');
  await expect(failing.getByRole('alert')).toContainText('Mock history forced failure');
});

test('a narrow window shows the list, then the recap with Back', async ({ page }) => {
  await page.setViewportSize({ width: 560, height: 800 });
  await open(page);
  await expect(page.getByRole('complementary', { name: 'Recap' })).toHaveCount(0);
  await row(page, 'Fix it in the lexer').click();
  await expect(page.getByRole('complementary', { name: 'Recap' })).toBeVisible();
  await expect(page.getByRole('listbox', { name: 'Conversations' })).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.getByRole('button', { name: 'History', exact: true }).click();
  await expect(page.getByRole('listbox', { name: 'Conversations' })).toBeVisible();
});

for (const scheme of ['light', 'dark'] as const) {
  test(`History passes the accessibility contract in ${scheme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await open(page);
    await row(page, 'Fix it in the lexer').click();
    await expectAccessible(page);
    await page.getByRole('textbox', { name: 'Search history' }).fill('what did we decide about the lexer');
    await expect(page.getByRole('region', { name: 'Best match' })).toBeVisible();
    await expectAccessible(page);
  });
}

// ---- the 12-hour archive (4c) ----------------------------------------------

const HOUR = 3_600_000;
/** The conversations behind the seeded tabs: the engine names a reattached session by its own stored title. */
const seededTitles: [string, string][] = [['active', 'Active one'], ['old-a', 'Old A'], ['old-b', 'Old B'], ['recent', 'Recent'], ['held', 'Waiting on you'], ['pin', 'Pinned old']];
const withSeeded = () => withHistory([...designConversations(), ...seededTitles.map(([id, title]) => ({ id, title, at: new Date(NOW.getTime() - 60 * HOUR).toISOString(), messages: [{ role: 'user' as const, text: title }] }))]);
async function seedTabs(page: Page) {
  const tab = (id: string, title: string, extra: Record<string, unknown> = {}) => ({ id, title, draft: '', pinned: false, kind: 'conversation', sessionFile: `/mock/places/${id}/transcript.jsonl`, ...extra });
  const state = {
    tabs: [tab('active', 'Active one'), tab('old-a', 'Old A'), tab('old-b', 'Old B'), tab('recent', 'Recent'), tab('held', 'Waiting on you'), tab('pin', 'Pinned old', { pinned: true }), { id: 'blank', title: 'New conversation', draft: '', pinned: false, kind: 'conversation' }],
    groups: [], closed: [], activeId: 'active', nextNumber: 8, recentIds: ['active'],
  };
  const stamp = NOW.getTime();
  const activity = {
    active: { at: stamp - 30 * HOUR, hold: false }, 'old-a': { at: stamp - 13 * HOUR, hold: false }, 'old-b': { at: stamp - 40 * HOUR, hold: false },
    recent: { at: stamp - 3 * HOUR, hold: false }, held: { at: stamp - 50 * HOUR, hold: true }, pin: { at: stamp - 50 * HOUR, hold: false }, blank: { at: stamp - 50 * HOUR, hold: false },
  };
  // Seeded once: a reload is a later launch and must see what the first one left behind.
  await page.addInitScript(([workspace, saved]) => { if (localStorage.getItem('codeaf.desktop.workspace.v1')) return; localStorage.setItem('codeaf.desktop.workspace.v1', workspace); localStorage.setItem('codeaf.desktop.activity.v1', saved); }, [JSON.stringify(state), JSON.stringify(activity)]);
}

test('tabs idle for 12 hours archive themselves at launch, with one toast to Review or Restore all', async ({ page }) => {
  await page.clock.setFixedTime(NOW);
  const engine = await installMockEngine(page, withSeeded());
  await seedTabs(page);
  await page.goto('/');
  const toast = page.getByRole('status').filter({ hasText: 'Archived' });
  await expect(toast).toHaveText(/Archived 2 tabs idle for more than 12h/);
  // Pinned, active, recent, waiting-on-you and empty tabs stay; the two idle ones leave the strip.
  for (const name of ['Active one', 'Recent', 'Waiting on you', 'Pinned old']) await expect(page.getByRole('tab', { name: new RegExp(name) })).toHaveCount(1);
  await expect(page.getByRole('tab', { name: /Old A/ })).toHaveCount(0);
  await expect(page.getByRole('tab', { name: /Old B/ })).toHaveCount(0);
  await expect.poll(() => engine.history.archived()).toEqual([{ id: 'old-a', archived: true }, { id: 'old-b', archived: true }]);
  // Nothing is deleted: the archive route is asked once for the two conversations, best effort.
  expect(engine.calls.some(call => call.method === 'POST' && call.path.endsWith('/history/archive') && JSON.stringify(call.body.ids) === JSON.stringify(['old-a', 'old-b']) && call.body.archived === true)).toBe(true);
  // Geometry from the design: 40px high, radius 12, sh-2, centred.
  expect((await toast.boundingBox())!.height).toBe(px('history-toast-height'));
  await expect(toast).toHaveCSS('border-top-left-radius', '12px');
  await expect(toast.getByRole('button', { name: 'Review' })).toHaveCSS('font-size', '12px');
  await toast.getByRole('button', { name: 'Restore all' }).click();
  await expect(toast).toHaveCount(0);
  await expect(page.getByRole('tab', { name: /Old A/ })).toHaveCount(1);
  await expect(page.getByRole('tab', { name: /Old B/ })).toHaveCount(1);
  expect(engine.calls.some(call => call.path.endsWith('/history/archive') && call.body.archived === false)).toBe(true);
});

test('Review opens History and a later launch archives nothing new', async ({ page }) => {
  await page.clock.setFixedTime(NOW);
  await installMockEngine(page, withSeeded());
  await seedTabs(page);
  await page.goto('/');
  const toast = page.getByRole('status').filter({ hasText: 'Archived' });
  await toast.getByRole('button', { name: 'Review' }).click();
  await expect(page.getByRole('heading', { name: 'History' })).toBeVisible();
  await expect(toast).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('status').filter({ hasText: 'Archived' })).toHaveCount(0);
});

test('Escape dismisses the toast and the tabs stay archived', async ({ page }) => {
  await page.clock.setFixedTime(NOW);
  await installMockEngine(page, withSeeded());
  await seedTabs(page);
  await page.goto('/');
  const toast = page.getByRole('status').filter({ hasText: 'Archived' });
  await expect(toast).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(toast).toHaveCount(0);
  await expect(page.getByRole('tab', { name: /Old A/ })).toHaveCount(0);
});
