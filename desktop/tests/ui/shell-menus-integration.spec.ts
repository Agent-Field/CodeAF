import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, tokenColor } from './contracts';
import { installMockEngine } from './support/mock-engine';
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
  test('in a browser there is no Copy link and no Move to new window, and nothing is disabled in their place', async ({ page }) => {
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
